//go:build !windows

package agent

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"syscall"
)

func listenIPC(dir string) (net.Listener, error) {
	path := filepath.Join(dir, "agent.sock")
	if info, e := os.Lstat(path); e == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, errors.New("IPC path is not a socket")
		}
		if c, e := net.Dial("unix", path); e == nil {
			c.Close()
			return nil, errors.New("agent already running")
		}
		if e = os.Remove(path); e != nil {
			return nil, e
		}
	}
	ln, e := net.Listen("unix", path)
	if e == nil {
		e = os.Chmod(path, 0600)
	}
	return ln, e
}
func dialIPC(ctx context.Context, dir string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, "unix", filepath.Join(dir, "agent.sock"))
}
func acquireLock(dir string) (func(), error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(filepath.Join(dir, "agent.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		return nil, errors.New("agent already running")
	}
	return func() { f.Close() }, nil
}

func PrepareService(dir string) error { return os.MkdirAll(dir, 0700) }
