package sipclient

import (
	"testing"

	"github.com/emiago/sipgo/sip"

	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

func TestRequestFactoryKeepsPlatformIdentitiesIsolated(t *testing.T) {
	a := mustFactory(t, Identity{
		UpstreamServerID: "34020000002000000001", UpstreamDomain: "3402000000", Host: "192.0.2.10", Port: 5060,
		LocalDeviceID: "34020000001320000001", LocalDomain: "3402000000", LocalIP: "192.0.2.20", LocalPort: 5061,
		Transport: "udp", Profile: protocol.Version2016,
	})
	b := mustFactory(t, Identity{
		UpstreamServerID: "44010000002000000001", UpstreamDomain: "4401000000", Host: "198.51.100.10", Port: 15060,
		LocalDeviceID: "44010000001320000001", LocalDomain: "4401000000", LocalIP: "198.51.100.20", LocalPort: 15061,
		Transport: "tcp", Profile: protocol.Version2022,
	})

	requestA, err := a.BuildRegister(3600, "call-a")
	if err != nil {
		t.Fatal(err)
	}
	requestB, err := b.BuildRegister(3600, "call-b")
	if err != nil {
		t.Fatal(err)
	}
	assertIdentity(t, requestA, "34020000002000000001", "3402000000", "34020000001320000001", "3402000000", "192.0.2.20", 5061, "UDP", "192.0.2.10:5060")
	assertIdentity(t, requestB, "44010000002000000001", "4401000000", "44010000001320000001", "4401000000", "198.51.100.20", 15061, "TCP", "198.51.100.10:15060")
	if requestA.GetHeader("X-GB-Ver") != nil {
		t.Fatal("2016 request must not carry X-GB-Ver")
	}
	if header := requestB.GetHeader("X-GB-Ver"); header == nil || header.Value() != "3.0" {
		t.Fatalf("2022 X-GB-Ver=%v", header)
	}
	if requestA.CSeq().SeqNo != 1 || requestB.CSeq().SeqNo != 1 {
		t.Fatalf("independent factories must start at CSeq 1: A=%d B=%d", requestA.CSeq().SeqNo, requestB.CSeq().SeqNo)
	}
	nextA, err := a.BuildRegister(0, "call-a-logout")
	if err != nil {
		t.Fatal(err)
	}
	if nextA.CSeq().SeqNo != 2 || nextA.GetHeader("Expires").Value() != "0" {
		t.Fatalf("logout CSeq=%d Expires=%s", nextA.CSeq().SeqNo, nextA.GetHeader("Expires").Value())
	}
}

func TestRequestFactoryRejectsInvalidIdentity(t *testing.T) {
	valid := Identity{
		UpstreamServerID: "34020000002000000001", UpstreamDomain: "3402000000", Host: "192.0.2.10", Port: 5060,
		LocalDeviceID: "34020000001320000001", LocalDomain: "3402000000", LocalIP: "192.0.2.20", LocalPort: 5061,
		Transport: "udp", Profile: protocol.Version2016,
	}
	cases := []Identity{valid, valid, valid, valid, valid}
	cases[0].LocalDeviceID = "bad"
	cases[1].Host = ""
	cases[2].LocalIP = "0.0.0.0"
	cases[3].Transport = "ws"
	cases[4].Host = "upstream.example\r\nVia: injected"
	for i, identity := range cases {
		if _, err := NewRequestFactory(identity); err == nil {
			t.Fatalf("case %d must fail", i)
		}
	}
}

func mustFactory(t *testing.T, identity Identity) *RequestFactory {
	t.Helper()
	factory, err := NewRequestFactory(identity)
	if err != nil {
		t.Fatal(err)
	}
	return factory
}

func assertIdentity(t *testing.T, request *sip.Request, upstreamID, upstreamDomain, localID, localDomain, localIP string, localPort int, transport, destination string) {
	t.Helper()
	if request.Recipient.User != upstreamID || request.Recipient.Host != upstreamDomain {
		t.Fatalf("recipient=%s", request.Recipient.String())
	}
	if request.From() == nil || request.From().Address.User != localID || request.From().Address.Host != localDomain {
		t.Fatalf("From=%v", request.From())
	}
	if request.Contact() == nil || request.Contact().Address.User != localID || request.Contact().Address.Host != localIP || request.Contact().Address.Port != localPort {
		t.Fatalf("Contact=%v", request.Contact())
	}
	if request.Via() == nil || request.Via().Host != localIP || request.Via().Port != localPort || request.Via().Transport != transport {
		t.Fatalf("Via=%v", request.Via())
	}
	if request.Transport() != transport || request.Destination() != destination {
		t.Fatalf("transport=%s destination=%s", request.Transport(), request.Destination())
	}
}
