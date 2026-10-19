//go:build windows

package agent

import (
	"context"
	"errors"
	"fmt"
	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
	"net"
	"os"
	"os/exec"
)

const pipeName = `\\.\pipe\AltasCITunnel`

func listenIPC(_ string) (net.Listener, error) {
	principal, e := windows.GetCurrentProcessToken().GetTokenUser()
	if e != nil {
		return nil, e
	}
	sid := principal.User.Sid.String()
	if principal.User.Sid.IsWellKnown(windows.WinLocalServiceSid) {
		serviceSID, _, _, err := windows.LookupSID("", `NT SERVICE\AltasCITunnel`)
		if err != nil {
			return nil, err
		}
		sid = serviceSID.String()
	}
	return winio.ListenPipe(pipeName, &winio.PipeConfig{SecurityDescriptor: "D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GA;;;" + sid + ")"})
}
func dialIPC(ctx context.Context, _ string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, pipeName)
}
func acquireLock(_ string) (func(), error) {
	name, e := windows.UTF16PtrFromString(`Global\AltasCITunnel`)
	if e != nil {
		return nil, e
	}
	h, e := windows.CreateMutex(nil, false, name)
	if e != nil {
		if h != 0 {
			windows.CloseHandle(h)
		}
		if errors.Is(e, windows.ERROR_ALREADY_EXISTS) {
			return nil, errors.New("agent already running")
		}
		return nil, e
	}
	return func() { windows.CloseHandle(h) }, nil
}

func PrepareService(dir string) error {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	for _, args := range [][]string{{"sc.exe", "sidtype", "AltasCITunnel", "unrestricted"}, {"sc.exe", "failure", "AltasCITunnel", "reset=", "86400", "actions=", "restart/5000/restart/5000/restart/5000"}, {"icacls.exe", dir, "/inheritance:r", "/grant:r", "*S-1-5-18:(OI)(CI)F", "*S-1-5-32-544:(OI)(CI)F", "NT SERVICE\\AltasCITunnel:(OI)(CI)F"}} {
		b, e := exec.Command(args[0], args[1:]...).CombinedOutput()
		if e != nil {
			return fmt.Errorf("service directory setup failed: %s: %w", b, e)
		}
	}
	return nil
}
