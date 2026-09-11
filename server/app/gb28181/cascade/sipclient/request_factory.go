package sipclient

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/emiago/sipgo/sip"

	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

type Identity struct {
	UpstreamServerID string
	UpstreamDomain   string
	Host             string
	Port             int
	LocalDeviceID    string
	LocalDomain      string
	LocalIP          string
	LocalPort        int
	Transport        string
	Profile          protocol.Version
}

type RequestFactory struct {
	identity  Identity
	transport string
	cseq      uint32
}

func NewRequestFactory(identity Identity) (*RequestFactory, error) {
	transport := strings.ToUpper(strings.TrimSpace(identity.Transport))
	if !validGBID(identity.UpstreamServerID) || !validGBID(identity.LocalDeviceID) {
		return nil, fmt.Errorf("cascade SIP identity requires 20-digit IDs")
	}
	if !validHostToken(identity.UpstreamDomain) || !validHostToken(identity.LocalDomain) || !validHostToken(identity.Host) {
		return nil, fmt.Errorf("cascade SIP identity requires domains and upstream host")
	}
	localIP := net.ParseIP(strings.TrimSpace(identity.LocalIP))
	if localIP == nil || localIP.To4() == nil || localIP.IsUnspecified() {
		return nil, fmt.Errorf("cascade SIP identity requires a usable local IPv4 address")
	}
	if identity.Port < 1 || identity.Port > 65535 || identity.LocalPort < 1 || identity.LocalPort > 65535 {
		return nil, fmt.Errorf("cascade SIP identity has an invalid port")
	}
	if transport != "UDP" && transport != "TCP" {
		return nil, fmt.Errorf("cascade SIP identity transport must be UDP or TCP")
	}
	if identity.Profile != protocol.Version2016 && identity.Profile != protocol.Version2022 {
		return nil, fmt.Errorf("cascade SIP identity requires an effective profile")
	}
	identity.LocalIP = localIP.To4().String()
	identity.Host = strings.TrimSpace(identity.Host)
	return &RequestFactory{identity: identity, transport: transport}, nil
}

func (f *RequestFactory) BuildRegister(expires int, callID string) (*sip.Request, error) {
	if f == nil || expires < 0 || strings.TrimSpace(callID) == "" {
		return nil, fmt.Errorf("invalid cascade REGISTER request")
	}
	recipient := sip.Uri{Scheme: "sip", User: f.identity.UpstreamServerID, Host: f.identity.UpstreamDomain}
	request := sip.NewRequest(sip.REGISTER, recipient)
	request.SetDestination(net.JoinHostPort(f.identity.Host, strconv.Itoa(f.identity.Port)))
	request.SetTransport(f.transport)

	fromParams := sip.NewParams()
	fromParams.Add("tag", sip.GenerateTagN(16))
	request.AppendHeader(&sip.FromHeader{
		Address: sip.Uri{Scheme: "sip", User: f.identity.LocalDeviceID, Host: f.identity.LocalDomain}, Params: fromParams,
	})
	request.AppendHeader(&sip.ToHeader{Address: sip.Uri{Scheme: "sip", User: f.identity.LocalDeviceID, Host: f.identity.LocalDomain}, Params: sip.NewParams()})
	request.AppendHeader(&sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: f.identity.LocalDeviceID, Host: f.identity.LocalIP, Port: f.identity.LocalPort}})
	viaParams := sip.NewParams()
	viaParams.Add("branch", sip.GenerateBranchN(16))
	request.AppendHeader(&sip.ViaHeader{
		ProtocolName: "SIP", ProtocolVersion: "2.0", Transport: f.transport,
		Host: f.identity.LocalIP, Port: f.identity.LocalPort, Params: viaParams,
	})
	callIDHeader := sip.CallIDHeader(strings.TrimSpace(callID))
	request.AppendHeader(&callIDHeader)
	request.AppendHeader(&sip.CSeqHeader{SeqNo: atomic.AddUint32(&f.cseq, 1), MethodName: sip.REGISTER})
	request.AppendHeader(sip.NewHeader("Max-Forwards", "70"))
	request.AppendHeader(sip.NewHeader("Expires", strconv.Itoa(expires)))
	if f.identity.Profile == protocol.Version2022 {
		request.AppendHeader(sip.NewHeader("X-GB-Ver", "3.0"))
	}
	request.SetBody(nil)
	return request, nil
}

// BuildMessage creates only the SIP envelope for a profile-owned MANSCDP body.
// Message XML is intentionally encoded outside this factory.
func (f *RequestFactory) BuildMessage(body []byte, callID string) (*sip.Request, error) {
	if f == nil || len(body) == 0 || strings.TrimSpace(callID) == "" {
		return nil, fmt.Errorf("invalid cascade MESSAGE request")
	}
	recipient := sip.Uri{Scheme: "sip", User: f.identity.UpstreamServerID, Host: f.identity.UpstreamDomain}
	request := sip.NewRequest(sip.MESSAGE, recipient)
	request.SetDestination(net.JoinHostPort(f.identity.Host, strconv.Itoa(f.identity.Port)))
	request.SetTransport(f.transport)

	fromParams := sip.NewParams()
	fromParams.Add("tag", sip.GenerateTagN(16))
	request.AppendHeader(&sip.FromHeader{
		Address: sip.Uri{Scheme: "sip", User: f.identity.LocalDeviceID, Host: f.identity.LocalDomain}, Params: fromParams,
	})
	request.AppendHeader(&sip.ToHeader{Address: sip.Uri{Scheme: "sip", User: f.identity.UpstreamServerID, Host: f.identity.UpstreamDomain}, Params: sip.NewParams()})
	request.AppendHeader(&sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: f.identity.LocalDeviceID, Host: f.identity.LocalIP, Port: f.identity.LocalPort}})
	viaParams := sip.NewParams()
	viaParams.Add("branch", sip.GenerateBranchN(16))
	request.AppendHeader(&sip.ViaHeader{
		ProtocolName: "SIP", ProtocolVersion: "2.0", Transport: f.transport,
		Host: f.identity.LocalIP, Port: f.identity.LocalPort, Params: viaParams,
	})
	callIDHeader := sip.CallIDHeader(strings.TrimSpace(callID))
	request.AppendHeader(&callIDHeader)
	request.AppendHeader(&sip.CSeqHeader{SeqNo: atomic.AddUint32(&f.cseq, 1), MethodName: sip.MESSAGE})
	request.AppendHeader(sip.NewHeader("Max-Forwards", "70"))
	request.AppendHeader(sip.NewHeader("Content-Type", "Application/MANSCDP+xml"))
	if f.identity.Profile == protocol.Version2022 {
		request.AppendHeader(sip.NewHeader("X-GB-Ver", "3.0"))
	}
	request.SetBody(body)
	return request, nil
}

func (f *RequestFactory) Profile() protocol.Version {
	if f == nil {
		return ""
	}
	return f.identity.Profile
}

func validGBID(value string) bool {
	if len(value) != 20 {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func validHostToken(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 253 || strings.ContainsAny(value, " \t\r\n/@") {
		return false
	}
	if net.ParseIP(value) != nil {
		return true
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' {
				return false
			}
		}
	}
	return true
}
