// altasci-server is the container entrypoint. It supervises the existing server
// binaries; it neither proxies requests nor runs a database or a device agent.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/altasci/altascitunnel/internal/host"
	"github.com/altasci/altascitunnel/internal/secure"
)

type config struct {
	data, socket, web, bin, public, control, edge string
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func configuration() config {
	return config{
		data:   env("ALTASCI_DATA_DIR", "/data"),
		socket: "/run/altasci/edge.sock", web: "/app/web/dist", bin: "/usr/local/bin",
		public:  os.Getenv("ALTASCI_PUBLIC_URL"),
		control: env("ALTASCI_CONTROL_LISTEN", "0.0.0.0:18081"),
		edge:    env("ALTASCI_EDGE_LISTEN", "0.0.0.0:18080"),
	}
}

func (c config) key() string { return filepath.Join(c.data, "secrets.key") }

func (c config) command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(filepath.Join(c.bin, name), args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, configuration(), os.Args[1:]); err != nil {
		slog.Error("server command failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, c config, args []string) error {
	command := "serve"
	if len(args) > 0 {
		command, args = args[0], args[1:]
	}
	switch command {
	case "serve":
		if len(args) != 0 {
			return errors.New("serve uses environment configuration, not positional arguments")
		}
		if err := c.validate(); err != nil {
			return err
		}
		if _, err := secure.Load(c.key()); err != nil {
			return fmt.Errorf("load persistent key (run init first, or restore the original key): %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(c.socket), 0700); err != nil {
			return err
		}
		return supervise(ctx, []*exec.Cmd{
			c.command("altasci-edge", "--listen", c.edge, "--allow-non-loopback", "--admin", "unix://"+c.socket, "--policy", filepath.Join(c.data, "policy.json")),
			c.command("altasci-control", "serve", "--listen", c.control, "--allow-non-loopback", "--edge", "unix://"+c.socket, "--key", c.key(), "--public-url", c.public, "--web", c.web),
		}, 15*time.Second)
	case "init":
		if len(args) != 0 {
			return errors.New("init takes no arguments")
		}
		if os.Getenv("DATABASE_URL") == "" {
			return errors.New("DATABASE_URL is required")
		}
		if _, err := os.Lstat(c.key()); errors.Is(err, os.ErrNotExist) {
			if err = secure.GenerateKey(c.key()); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if _, err := secure.Load(c.key()); err != nil {
			return fmt.Errorf("existing key is invalid; refusing to replace it: %w", err)
		}
		return supervise(ctx, []*exec.Cmd{c.command("altasci-control", "migrate")}, 15*time.Second, true)
	case "keygen", "migrate", "admin-create", "recover-revisions":
		argv := append([]string{command, "--key", c.key()}, args...)
		return supervise(ctx, []*exec.Cmd{c.command("altasci-control", argv...)}, 15*time.Second, true)
	case "healthcheck":
		return c.healthcheck(ctx)
	default:
		return errors.New("expected serve|init|keygen|migrate|admin-create|recover-revisions|healthcheck")
	}
}

func (c config) validate() error {
	if os.Getenv("DATABASE_URL") == "" {
		return errors.New("DATABASE_URL is required")
	}
	u, err := url.Parse(c.public)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || c.public != "https://"+u.Host {
		return errors.New("ALTASCI_PUBLIC_URL must be the external HTTPS origin, without a path or trailing slash")
	}
	for _, addr := range []string{c.control, c.edge} {
		if err := host.BusinessListener(addr, true); err != nil {
			return err
		}
	}
	return nil
}

type childExit struct {
	name string
	err  error
}

// oneShot permits successful completion of an initialization command. A server
// exiting with code zero is still unexpected and must restart the container.
func supervise(ctx context.Context, commands []*exec.Cmd, grace time.Duration, oneShot ...bool) error {
	exits := make(chan childExit, len(commands))
	started := make([]*exec.Cmd, 0, len(commands))
	remaining := 0
	var result error
	for _, cmd := range commands {
		if err := cmd.Start(); err != nil {
			result = fmt.Errorf("start %s: %w", filepath.Base(cmd.Path), err)
			break
		}
		started = append(started, cmd)
		remaining++
		go func() { exits <- childExit{filepath.Base(cmd.Path), cmd.Wait()} }()
	}
	if result == nil {
		select {
		case <-ctx.Done():
		case exit := <-exits:
			remaining--
			if len(oneShot) > 0 && oneShot[0] && len(commands) == 1 {
				return exit.err
			}
			result = fmt.Errorf("%s exited unexpectedly: %v", exit.name, exit.err)
		}
	}
	for _, cmd := range started {
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	for remaining > 0 {
		select {
		case <-exits:
			remaining--
		case <-timer.C:
			for _, cmd := range started {
				_ = cmd.Process.Kill()
			}
			for remaining > 0 {
				<-exits
				remaining--
			}
			if result == nil {
				result = errors.New("shutdown grace period exceeded; child processes killed")
			}
		}
	}
	return result
}

func (c config) healthcheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	controlAddr := probeAddress(c.control)
	client := &http.Client{Timeout: 4 * time.Second}
	if err := probe(ctx, client, "http://"+controlAddr+"/health/ready"); err != nil {
		return fmt.Errorf("control readiness: %w", err)
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", c.socket)
	}}
	defer transport.CloseIdleConnections()
	if err := probe(ctx, &http.Client{Transport: transport}, "http://local/health/live"); err != nil {
		return fmt.Errorf("edge liveness: %w", err)
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", probeAddress(c.edge))
	if err != nil {
		return fmt.Errorf("edge data listener: %w", err)
	}
	return conn.Close()
}

func probeAddress(addr string) string {
	host, port, _ := net.SplitHostPort(addr)
	if host == "0.0.0.0" {
		host = "127.0.0.1"
	} else if host == "::" {
		host = "::1"
	}
	return net.JoinHostPort(host, port)
}

func probe(ctx context.Context, client *http.Client, endpoint string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return nil
}
