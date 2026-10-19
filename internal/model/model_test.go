package model

import "testing"

func TestTargetIPv4AndHash(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "192.168.1.20", "8.8.8.8"} {
		if e := (Tunnel{Protocol: "tcp", TargetIP: ip, TargetPort: 22}).Validate(); e != nil {
			t.Fatal(ip, e)
		}
	}
	for _, ip := range []string{"0.0.0.0", "::1", "224.0.0.1", "255.255.255.255", "example.com"} {
		if (Tunnel{Protocol: "udp", TargetIP: ip, TargetPort: 53}).Validate() == nil {
			t.Fatal(ip)
		}
	}
	a := ClientConfig{SchemaVersion: 1, ClientID: "a", Revision: 1, Tunnels: []TunnelSpec{{Tunnel: Tunnel{ID: "b"}}, {Tunnel: Tunnel{ID: "a"}}}}
	b := a
	b.Tunnels = []TunnelSpec{a.Tunnels[1], a.Tunnels[0]}
	if a.HashValue() != b.HashValue() {
		t.Fatal("noncanonical tunnel ordering")
	}
}
