package host

import "testing"

func TestContainerBusinessListenerDoesNotExposeInternalAdmin(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:18081", "192.0.2.10:18080", "[::]:18081"} {
		if BusinessListener(addr, false) == nil {
			t.Fatalf("default accepted non-loopback %s", addr)
		}
		if err := BusinessListener(addr, true); err != nil {
			t.Fatalf("explicit container binding %s: %v", addr, err)
		}
		if _, err := Listen(addr); err == nil {
			t.Fatalf("internal listener accepted %s", addr)
		}
	}
	for _, addr := range []string{"localhost:18081", ":18081", "unix:///tmp/test.sock", "invalid"} {
		if BusinessListener(addr, true) == nil {
			t.Fatalf("accepted non-literal IP %s", addr)
		}
	}
	ln, err := ListenBusiness("127.0.0.1:0", false)
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
}
