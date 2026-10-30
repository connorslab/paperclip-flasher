package main

import "testing"

func TestOrigins(t *testing.T) {
	for _, v := range []struct {
		o  string
		ok bool
	}{{"http://192.168.1.2", true}, {"http://192.168.1.2:8088", true}, {"http://evil.example", false}, {"http://192.168.1.2:9000", false}, {"http://192.168.1.2/path", false}, {"http://user@192.168.1.2", false}} {
		if allowedOrigin(v.o, "192.168.1.2:8088") != v.ok {
			t.Fatal(v)
		}
	}
}
func TestNetworkValidation(t *testing.T) {
	if (NetworkSettings{DHCP: true, Auto: true}).validate() != nil {
		t.Fatal("DHCP rejected")
	}
	for _, v := range []NetworkSettings{{DHCP: false, Auto: true}, {DHCP: true, Auto: false}, {DHCP: false, Auto: true, IP: map[string]string{"IPv4addr": "192.168.1.4", "Subnetmask": "255.0.255.0", "Router": "192.168.1.1"}}} {
		if v.validate() == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
}
