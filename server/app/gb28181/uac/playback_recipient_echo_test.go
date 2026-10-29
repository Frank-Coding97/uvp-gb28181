package uac

import (
	"testing"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
)

// 有一类下级平台会把**我方**的 Contact 原样回显在 200 OK 里(实测 WVP-Pro
// v2.7.4 级联)。回放 dialog 的远端目标若照抄这个 Contact,INFO(TEARDOWN) 就会
// 打回本机监听端口、被自己的 SIP 栈回 481 Dialog Does Not Exist,对端会话永远
// 拆不掉 —— 现场表现为"每次停止回放都报回放控制失败(502),对端还留着死会话"。
//
// 这里钉住 sipgo 侧那套判据(ACK/BYE 已在用)的行为,playbackControlRecipient
// 拿到的 RemoteTarget 就是它的输出。
func TestDialogRequestRecipientRejectsEchoedLocalContact(t *testing.T) {
	inviteTarget := sip.Uri{}
	require.NoError(t, sip.ParseUri("sip:37010301021320000002@172.21.0.3:8160", &inviteTarget))
	request := sip.NewRequest(sip.INVITE, inviteTarget)
	request.SetDestination("172.21.0.3:8160")
	request.AppendHeader(&sip.ContactHeader{Address: sip.Uri{User: "34020000012000000001", Host: "172.21.0.1", Port: 52064}})

	response := sip.NewResponseFromRequest(request, 200, "OK", nil)
	response.AppendHeader(&sip.ContactHeader{Address: sip.Uri{User: "34020000012000000001", Host: "192.168.10.220", Port: 52064}})

	recipient := sipgo.DialogRequestRecipient(request, response)
	require.NotNil(t, recipient)
	require.Equal(t, "37010301021320000002", recipient.User)
	require.Equal(t, "172.21.0.3", recipient.Host)
	require.Equal(t, 8160, recipient.Port)
}

// 对端正常填 Contact 时必须原样采用：它是 RFC 3261 §12.1.1 的 remote target,
// 与 INVITE 收件人可能不同(经过代理/网关时)。
func TestDialogRequestRecipientKeepsWellFormedContact(t *testing.T) {
	inviteTarget := sip.Uri{}
	require.NoError(t, sip.ParseUri("sip:37010301021320000002@172.21.0.3:8160", &inviteTarget))
	request := sip.NewRequest(sip.INVITE, inviteTarget)
	request.SetDestination("172.21.0.3:8160")
	request.AppendHeader(&sip.ContactHeader{Address: sip.Uri{User: "34020000012000000001", Host: "172.21.0.1", Port: 52064}})

	response := sip.NewResponseFromRequest(request, 200, "OK", nil)
	response.AppendHeader(&sip.ContactHeader{Address: sip.Uri{User: "37010301021320000002", Host: "172.21.0.3", Port: 8160}})

	recipient := sipgo.DialogRequestRecipient(request, response)
	require.NotNil(t, recipient)
	require.Equal(t, "37010301021320000002", recipient.User)
	require.Equal(t, "172.21.0.3", recipient.Host)
	require.Equal(t, 8160, recipient.Port)
}

// 没有 200 OK(或没有 Contact)时退回 INVITE 收件人,与历史行为一致。
func TestDialogRequestRecipientFallsBackToInviteTarget(t *testing.T) {
	inviteTarget := sip.Uri{}
	require.NoError(t, sip.ParseUri("sip:37010301021320000002@172.21.0.3:8160", &inviteTarget))
	request := sip.NewRequest(sip.INVITE, inviteTarget)

	recipient := sipgo.DialogRequestRecipient(request, nil)
	require.NotNil(t, recipient)
	require.Equal(t, "37010301021320000002", recipient.User)
	require.Equal(t, "172.21.0.3", recipient.Host)
	require.Equal(t, 8160, recipient.Port)
}
