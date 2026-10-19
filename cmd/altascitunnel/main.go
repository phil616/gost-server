package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/altasci/altascitunnel/internal/agent"
	"github.com/kardianos/service"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
)

var defaultServer = "wss://tunnel.example.com"

type program struct {
	daemon *agent.Daemon
	cancel context.CancelFunc
	done   chan error
}

func (p *program) Start(service.Service) error {
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.done = make(chan error, 1)
	go func() {
		e := p.daemon.Run(ctx)
		p.done <- e
		if e != nil {
			slog.Error("agent service failed", "error", e)
			os.Exit(1)
		}
	}()
	return nil
}
func (p *program) Stop(service.Service) error { p.cancel(); return <-p.done }
func main() {
	if e := run(); e != nil {
		slog.Error("agent stopped", "error", e)
		os.Exit(1)
	}
}
func run() error {
	cmd := "run"
	args := os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd = args[0]
		args = args[1:]
	}
	sub := ""
	if cmd == "service" && len(args) > 0 {
		sub = args[0]
		args = args[1:]
	}
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	dir := fs.String("data-dir", agent.DefaultDir(), "private service state directory")
	server := fs.String("server", defaultServer, "management WSS origin")
	ca := fs.String("ca-file", "", "additional trusted CA file")
	token := fs.String("token", "", "one-time enrollment token")
	localTest := fs.Bool("allow-loopback-ws", false, "allow unencrypted loopback WS in local tests")
	if e := fs.Parse(args); e != nil {
		return e
	}
	serverExplicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "server" {
			serverExplicit = true
		}
	})
	if cmd == "purge-state" {
		return removeIdentity(*dir)
	}
	if cmd == "service-prepare" {
		return agent.PrepareService(*dir)
	}
	if cmd == "status" || cmd == "register" {
		client := agent.LocalClient(*dir)
		method := http.MethodGet
		path := "status"
		var body io.Reader
		if cmd == "register" {
			if *token == "" {
				return errors.New("--token required")
			}
			method = http.MethodPost
			path = "register"
			serverOverride := ""
			if serverExplicit {
				serverOverride = *server
			}
			b, _ := json.Marshal(map[string]string{"token": *token, "server": serverOverride})
			body = bytes.NewReader(b)
		}
		res, e := client.Do(mustRequest(method, "http://local/"+path, body))
		if e != nil {
			return fmt.Errorf("local service unavailable; install/start altascitunnel first: %w", e)
		}
		defer res.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		if res.StatusCode >= 300 {
			return fmt.Errorf("local service: %s", strings.TrimSpace(string(data)))
		}
		if cmd == "register" {
			fmt.Println("Enrollment accepted; inspect altascitunnel status for the assigned ClientID.")
		} else {
			fmt.Println(string(data))
		}
		return nil
	}
	configuredServer := ""
	if serverExplicit {
		configuredServer = *server
	}
	d := &agent.Daemon{Directory: *dir, Server: configuredServer, CAFile: *ca, AllowInsecureLocal: *localTest}
	p := &program{daemon: d}
	config := &service.Config{Name: "AltasCITunnel", DisplayName: "AltasCI Tunnel", Description: "Managed GOST reverse tunnel connector", Arguments: []string{"run", "--data-dir", *dir}}
	if runtime.GOOS == "windows" {
		config.UserName = `NT AUTHORITY\LocalService`
	}
	if serverExplicit {
		config.Arguments = append(config.Arguments, "--server", *server)
	}
	if *ca != "" {
		config.Arguments = append(config.Arguments, "--ca-file", *ca)
	}
	svc, e := service.New(p, config)
	if e != nil {
		return e
	}
	if cmd == "service" {
		switch sub {
		case "install":
			if e := svc.Install(); e != nil {
				return e
			}
			return agent.PrepareService(*dir)
		case "start":
			return svc.Start()
		case "stop":
			return svc.Stop()
		case "uninstall":
			_ = svc.Stop()
			if e := svc.Uninstall(); e != nil {
				return e
			}
			return removeIdentity(*dir)
		default:
			return errors.New("service install|start|stop|uninstall required")
		}
	}
	if cmd != "run" {
		return errors.New("expected run|register|status|service")
	}
	if !service.Interactive() {
		return svc.Run()
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return d.Run(ctx)
}
func mustRequest(method, url string, body io.Reader) *http.Request {
	r, e := http.NewRequest(method, url, body)
	if e != nil {
		panic(e)
	}
	r.Header.Set("Content-Type", "application/json")
	return r
}
func removeIdentity(dir string) error {
	for _, name := range []string{"identity.json", "config.json", "retries.json", "management-retry.json"} {
		e := os.Remove(dir + string(os.PathSeparator) + name)
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return e
		}
	}
	return nil
}
