package sipgo

import (
	"testing"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testInviteForAck(t *testing.T) *sip.Request {
	t.Helper()
	req := sip.NewRequest(sip.INVITE, sip.Uri{User: "37292800001320003005", Host: "3402000001"})
	req.SetDestination("172.21.0.3:8160")
	req.AppendHeader(&sip.ViaHeader{ProtocolName: "SIP", ProtocolVersion: "2.0", Transport: "UDP",
		Host: "172.21.0.1", Port: 52064, Params: sip.NewParams()})
	contact := sip.ContactHeader{Address: sip.Uri{User: "34020000012000000001", Host: "172.21.0.1", Port: 52064}}
	req.AppendHeader(&contact)
	callID := sip.CallIDHeader("ack-fallback-call")
	req.AppendHeader(&callID)
	req.AppendHeader(&sip.CSeqHeader{SeqNo: 90, MethodName: sip.INVITE})
	from := sip.NewParams()
	from.Add("tag", "local-tag")
	req.AppendHeader(&sip.FromHeader{Address: sip.Uri{User: "34020000012000000001", Host: "3402000001"}, Params: from})
	req.AppendHeader(&sip.ToHeader{Address: sip.Uri{User: "37292800001320003005", Host: "3402000001"}})
	return req
}

func testOKForAck(t *testing.T, req *sip.Request, contactUser, contactHost string, port int) *sip.Response {
	t.Helper()
	res := sip.NewResponseFromRequest(req, 200, "OK", nil)
	res.RemoveHeader("Contact")
	contact := sip.ContactHeader{Address: sip.Uri{User: contactUser, Host: contactHost, Port: port}}
	res.AppendHeader(&contact)
	return res
}

// 对端把 200 的 Contact 回显成我方 Contact(WVP 2.7.4 行为)时,ACK 必须回到
// INVITE 的真实发送目标,否则 ACK 打回自己、对端永远收不到,也就不会推流。
func TestNewAckRequestUACFallsBackToInviteTargetWhenResponseEchoesLocalContact(t *testing.T) {
	req := testInviteForAck(t)
	res := testOKForAck(t, req, "34020000012000000001", "192.168.10.220", 52064)

	ack := newAckRequestUAC(req, res, nil)
	require.NotNil(t, ack)
	assert.Equal(t, "37292800001320003005", ack.Recipient.User)
	assert.Equal(t, "172.21.0.3", ack.Recipient.Host)
	assert.Equal(t, 8160, ack.Recipient.Port)
}

// 对端 Contact 规范(指向它自己)时,必须原样遵循 Contact,不能被兜底逻辑改写。
func TestNewAckRequestUACKeepsPeerContactWhenNotEchoed(t *testing.T) {
	req := testInviteForAck(t)
	res := testOKForAck(t, req, "35020000002000000001", "192.168.10.220", 8160)

	ack := newAckRequestUAC(req, res, nil)
	require.NotNil(t, ack)
	assert.Equal(t, "35020000002000000001", ack.Recipient.User)
	assert.Equal(t, "192.168.10.220", ack.Recipient.Host)
	assert.Equal(t, 8160, ack.Recipient.Port)
}

// 200 未携带 Contact 时退回 INVITE 的原始 Request-URI。
func TestNewAckRequestUACFallsBackWhenResponseHasNoContact(t *testing.T) {
	req := testInviteForAck(t)
	res := sip.NewResponseFromRequest(req, 200, "OK", nil)
	res.RemoveHeader("Contact")

	ack := newAckRequestUAC(req, res, nil)
	require.NotNil(t, ack)
	assert.Equal(t, "37292800001320003005", ack.Recipient.User)
	assert.Equal(t, "3402000001", ack.Recipient.Host)
}
