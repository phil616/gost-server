package host

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func Loopback(addr string) error {
	host, _, e := net.SplitHostPort(addr)
	if e != nil {
		return e
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("internal listener must bind a literal loopback address")
	}
	return nil
}

// BusinessListener explicitly opts into container network interfaces. Internal
// coordination listeners continue to use Listen and its loopback restriction.
func BusinessListener(addr string, allowNonLoopback bool) error {
	if !allowNonLoopback {
		return Loopback(addr)
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if net.ParseIP(host) == nil {
		return errors.New("business listener must bind a literal IP address")
	}
	return nil
}

func ListenBusiness(addr string, allowNonLoopback bool) (net.Listener, error) {
	if err := BusinessListener(addr, allowNonLoopback); err != nil {
		return nil, err
	}
	return net.Listen("tcp", addr)
}
func Listen(endpoint string) (net.Listener, error) {
	if strings.HasPrefix(endpoint, "unix://") {
		path := strings.TrimPrefix(endpoint, "unix://")
		if e := os.MkdirAll(filepath.Dir(path), 0750); e != nil {
			return nil, e
		}
		if info, e := os.Lstat(path); e == nil {
			if info.Mode()&os.ModeSocket == 0 {
				return nil, errors.New("endpoint exists and is not a socket")
			}
			if conn, e := net.DialTimeout("unix", path, time.Second); e == nil {
				conn.Close()
				return nil, errors.New("endpoint already active")
			}
			if e = os.Remove(path); e != nil {
				return nil, e
			}
		}
		ln, e := net.Listen("unix", path)
		if e == nil {
			e = os.Chmod(path, 0660)
		}
		return ln, e
	}
	if e := Loopback(endpoint); e != nil {
		return nil, e
	}
	return net.Listen("tcp", endpoint)
}
func Serve(ctx context.Context, ln net.Listener, h http.Handler) error {
	server := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		server.Shutdown(shutdown)
	}()
	e := server.Serve(ln)
	if errors.Is(e, http.ErrServerClosed) {
		return nil
	}
	return e
}
