package sip

import (
	"net"
	"testing"
)

func TestRequestCloneOwnsResolvedAddresses(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "fe80::1"} {
		t.Run(ip, func(t *testing.T) {
			req := NewRequest(BYE, Uri{Scheme: "sip", Host: "example.test"})
			req.Laddr = Addr{IP: net.ParseIP(ip), Port: 5060, Hostname: "local.test", Zone: "en0"}
			req.raddr = Addr{IP: net.ParseIP(ip), Port: 5070, Hostname: "remote.test", Zone: "en1"}
			clone := req.Clone()
			if clone.Laddr.Port != 5060 || clone.Laddr.Zone != "en0" || clone.raddr.Port != 5070 || clone.raddr.Zone != "en1" {
				t.Fatal("clone changed address metadata")
			}
			clone.Laddr.IP[0] ^= 1
			clone.raddr.IP[0] ^= 1
			if !req.Laddr.IP.Equal(net.ParseIP(ip)) || !req.raddr.IP.Equal(net.ParseIP(ip)) {
				t.Fatal("mutating snapshot changed original resolved address")
			}
			originalLocal, originalRemote := clone.Laddr.IP.String(), clone.raddr.IP.String()
			req.Laddr.IP[1] ^= 1
			req.raddr.IP[1] ^= 1
			if clone.Laddr.IP.String() != originalLocal || clone.raddr.IP.String() != originalRemote {
				t.Fatal("mutating original changed snapshot resolved address")
			}
		})
	}
	req := NewRequest(BYE, Uri{Scheme: "sip", Host: "example.test"})
	if clone := req.Clone(); clone.Laddr.IP != nil || clone.raddr.IP != nil {
		t.Fatal("clone resolved an unset address")
	}
}
