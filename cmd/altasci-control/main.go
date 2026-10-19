package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/altasci/altascitunnel/internal/control"
	"github.com/altasci/altascitunnel/internal/edge"
	"github.com/altasci/altascitunnel/internal/host"
	"github.com/altasci/altascitunnel/internal/secure"
	"github.com/altasci/altascitunnel/internal/store"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

func main() {
	if e := run(); e != nil {
		slog.Error("control stopped", "error", e)
		os.Exit(1)
	}
}
func run() error {
	command := "serve"
	args := os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command = args[0]
		args = args[1:]
	}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	dsn := fs.String("database-url", os.Getenv("DATABASE_URL"), "PostgreSQL URL (or DATABASE_URL)")
	key := fs.String("key", "/etc/altasci/secrets.key", "AES-256 key file")
	listen := fs.String("listen", "127.0.0.1:18081", "HTTP listener behind OpenResty")
	endpoint := fs.String("edge", "unix:///run/altasci/edge.sock", "Edge internal endpoint")
	public := fs.String("public-url", "https://tunnel.example.com", "public HTTPS origin")
	web := fs.String("web", "web/dist", "compiled React assets")
	user := fs.String("username", "admin", "administrator username")
	passwordStdin := fs.Bool("password-stdin", false, "read administrator password from stdin")
	dev := fs.Bool("dev-http", false, "allow non-Secure admin cookie for loopback development only")
	recoveryFloor := fs.Int64("minimum-revision", 0, "recovery revision floor above every previously issued policy/client revision")
	if e := fs.Parse(args); e != nil {
		return e
	}
	if command == "keygen" {
		return secure.GenerateKey(*key)
	}
	if *dsn == "" {
		return errors.New("DATABASE_URL or --database-url required")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	repo, e := store.Open(ctx, *dsn)
	if e != nil {
		return e
	}
	defer repo.Close()
	if command == "migrate" {
		return repo.Migrate(ctx)
	}
	vault, e := secure.Load(*key)
	if e != nil {
		return e
	}
	server := control.New(repo, vault, edge.NewClient(*endpoint), *public)
	if command == "recover-revisions" {
		return server.RecoverRevisions(ctx, *recoveryFloor)
	}
	if command == "admin-create" {
		if !*passwordStdin {
			return errors.New("--password-stdin required")
		}
		password, e := bufio.NewReader(os.Stdin).ReadString('\n')
		if e != nil && len(password) == 0 {
			return e
		}
		return server.InitAdmin(ctx, *user, strings.TrimRight(password, "\r\n"))
	}
	if command != "serve" {
		return fmt.Errorf("unknown command %q", command)
	}
	if e = host.Loopback(*listen); e != nil {
		return e
	}
	server.WebDir = *web
	server.CookieSecure = !*dev
	server.Start(ctx)
	defer server.Close()
	ln, e := host.Listen(*listen)
	if e != nil {
		return e
	}
	slog.Info("control started", "listen", *listen)
	return host.Serve(ctx, ln, server.Handler())
}
