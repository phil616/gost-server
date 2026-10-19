package main

import (
	"context"
	"flag"
	"github.com/altasci/altascitunnel/internal/edge"
	"github.com/altasci/altascitunnel/internal/host"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	if e := run(); e != nil {
		slog.Error("edge stopped", "error", e)
		os.Exit(1)
	}
}
func run() error {
	data := flag.String("listen", "127.0.0.1:18080", "GOST WebSocket listener")
	admin := flag.String("admin", "unix:///run/altasci/edge.sock", "protected internal listener (loopback TCP allowed for testing)")
	policy := flag.String("policy", "/var/lib/altasci-edge/policy.json", "durable policy snapshot")
	flag.Parse()
	if e := host.Loopback(*data); e != nil {
		return e
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	engine, e := edge.New(*policy)
	if e != nil {
		return e
	}
	ln, e := host.Listen(*admin)
	if e != nil {
		return e
	}
	defer ln.Close()
	if e = engine.Start(ctx, *data); e != nil {
		return e
	}
	slog.Info("edge started", "listen", *data)
	return host.Serve(ctx, ln, engine.Handler())
}
