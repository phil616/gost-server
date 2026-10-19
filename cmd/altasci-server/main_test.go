package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestServerChild(t *testing.T) {
	mode := os.Getenv("ALTASCI_TEST_CHILD")
	if mode == "" {
		return
	}
	if mode == "exit" {
		os.Exit(0)
	}
	term := make(chan os.Signal, 1)
	signal.Notify(term, syscall.SIGTERM)
	if err := os.WriteFile(os.Getenv("ALTASCI_TEST_MARKER")+".ready", nil, 0600); err != nil {
		os.Exit(2)
	}
	<-term
	if mode == "ignore" {
		for {
			time.Sleep(time.Hour)
		}
	}
	if err := os.WriteFile(os.Getenv("ALTASCI_TEST_MARKER"), nil, 0600); err != nil {
		os.Exit(3)
	}
	os.Exit(0)
}

func helper(t *testing.T, mode string) (*exec.Cmd, string) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "stopped")
	cmd := exec.Command(os.Args[0], "-test.run=^TestServerChild$")
	cmd.Env = append(os.Environ(), "ALTASCI_TEST_CHILD="+mode, "ALTASCI_TEST_MARKER="+marker, "GORACE=atexit_sleep_ms=0")
	return cmd, marker
}

func waitReady(t *testing.T, marker string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker + ".ready"); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("child did not become ready")
}

func TestSupervisorShutdownAndFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("container PID 1 uses POSIX SIGTERM; execute on Linux")
	}
	t.Run("signal reaches both children and waits for exit", func(t *testing.T) {
		first, firstMarker := helper(t, "wait")
		second, secondMarker := helper(t, "wait")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- supervise(ctx, []*exec.Cmd{first, second}, time.Second) }()
		waitReady(t, firstMarker)
		waitReady(t, secondMarker)
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		for _, marker := range []string{firstMarker, secondMarker} {
			if _, err := os.Stat(marker); err != nil {
				t.Fatal("child did not receive SIGTERM", err)
			}
		}
	})
	t.Run("a clean server exit still fails the group", func(t *testing.T) {
		first, _ := helper(t, "exit")
		second, _ := helper(t, "wait")
		err := supervise(context.Background(), []*exec.Cmd{first, second}, 100*time.Millisecond)
		if err == nil || !strings.Contains(err.Error(), "exited unexpectedly") {
			t.Fatalf("unexpected result: %v", err)
		}
	})
	t.Run("one-shot initialization may exit successfully", func(t *testing.T) {
		child, _ := helper(t, "exit")
		if err := supervise(context.Background(), []*exec.Cmd{child}, time.Second, true); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("failed second start reaps first child", func(t *testing.T) {
		first, _ := helper(t, "wait")
		err := supervise(context.Background(), []*exec.Cmd{first, exec.Command("/nonexistent-altasci-test")}, 100*time.Millisecond)
		if err == nil || first.ProcessState == nil {
			t.Fatalf("start failure did not reap child: %v", err)
		}
	})
	t.Run("unresponsive child is killed within the grace period", func(t *testing.T) {
		child, marker := helper(t, "ignore")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- supervise(ctx, []*exec.Cmd{child}, 50*time.Millisecond) }()
		waitReady(t, marker)
		cancel()
		select {
		case err := <-done:
			if err == nil || !strings.Contains(err.Error(), "grace period exceeded") {
				t.Fatalf("unexpected result: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("shutdown exceeded deadline")
		}
	})
}

func TestInitPreservesOrRejectsExistingKey(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://unused")
	c := configuration()
	c.data, c.bin = t.TempDir(), t.TempDir() // Missing control binary prevents any DB access.
	for _, contents := range [][]byte{[]byte("invalid"), make([]byte, 32)} {
		if err := os.WriteFile(c.key(), contents, 0600); err != nil {
			t.Fatal(err)
		}
		err := run(context.Background(), c, []string{"init"})
		if err == nil {
			t.Fatal("init should reject invalid key or fail missing child")
		}
		actual, _ := os.ReadFile(c.key())
		if string(actual) != string(contents) {
			t.Fatal("init replaced the existing key")
		}
	}
}

func TestConfigurationAndHealthFailures(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://unused")
	c := configuration()
	c.public = "https://relay.example:8443"
	if err := c.validate(); err != nil {
		t.Fatal(err)
	}
	for _, public := range []string{"", "http://example.test", "https://example.test/", "https://example.test/api", "https://user@example.test", "https://example.test?q=1", "https://example.test?", "https://example.test#", "https://:18443"} {
		c.public = public
		if c.validate() == nil {
			t.Fatalf("accepted invalid origin %s", public)
		}
	}
	if probeAddress("0.0.0.0:18081") != "127.0.0.1:18081" || probeAddress("[::]:18081") != "[::1]:18081" {
		t.Fatal("health check must dial loopback instead of wildcard")
	}
	for _, code := range []int{200, 503} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) }))
		err := probe(context.Background(), server.Client(), server.URL)
		server.Close()
		if (code == 200) != (err == nil) {
			t.Fatalf("HTTP %d: %v", code, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := probe(ctx, http.DefaultClient, "http://127.0.0.1:1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("health check ignored cancellation: %v", err)
	}
}
