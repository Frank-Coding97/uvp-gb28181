package play

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/uac"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm"
)

// answerSDPWithMedia 构造设备 200 OK 的 SDP 应答。ip 传空表示应答里没有 c= 行。
func answerSDPWithMedia(ip string, port int, setup string) []byte {
	body := "v=0\r\no=34020000001320000002 0 0 IN IP4 192.168.10.203\r\ns=Play\r\n"
	if ip != "" {
		body += "c=IN IP4 " + ip + "\r\n"
	}
	body += "t=0 0\r\nm=video " + strconv.Itoa(port) + " TCP/RTP/AVP 96 97 98 99\r\na=recvonly\r\n"
	if setup != "" {
		body += "a=setup:" + setup + "\r\n"
	}
	body += "y=0200000001\r\n"
	return []byte(body)
}

// TCP-Active 通道必须让平台当主动方:offer 写 a=setup:active、ZLM 用 tcp_mode=2,
// 并且真的按应答 SDP 里的视频端口回连设备。三者缺一,现象都是「INVITE 成功、
// 页面一直没有画面」,所以三条断言必须一起锁住。
func TestStartTcpActiveDialsAnswerMediaPort(t *testing.T) {
	z := &mockZLM{port: 41000}
	z.online.Store(true)
	inv := &mockInviter{outcome: uac.InviteOutcome{
		RequestID: "req-tcp-active", RequestSent: true, FinalStatus: 200,
		AckSucceeded: true, AnswerSDP: answerSDPWithMedia("192.168.10.203", 30000, "passive"),
	}}
	ch := aChannel()
	ch.StreamTransport = "TCP-Active"
	s, _, _ := newSvc(t, z, inv, onlineDevice(), ch)

	_, err := s.Start(context.Background(), onlineDevice().DeviceID, ch.ChannelID)
	require.NoError(t, err)

	require.Equal(t, int32(2), z.lastTCPMode.Load(), "TCP-Active 必须以 tcp_mode=2 打开收流端")
	require.Contains(t, inv.lastBody, "TCP/RTP/AVP")
	require.Contains(t, inv.lastBody, "a=setup:active")
	require.NotContains(t, inv.lastBody, "a=setup:passive")

	require.Equal(t, int32(1), z.connectCalls.Load(), "必须回连一次设备媒体端口")
	target, ok := z.lastConnect.Load().(zlm.ConnectRtpServerRequest)
	require.True(t, ok)
	require.Equal(t, "192.168.10.203", target.DestHost)
	require.Equal(t, 30000, target.DestPort)
	require.NotEmpty(t, target.StreamID)
}

// 设备应答里的地址是 0.0.0.0(或干脆没有 c=)时不能拿去回连,必须退回信令来源地址。
func TestStartTcpActiveFallsBackToSignalingAddress(t *testing.T) {
	for name, body := range map[string][]byte{
		"unspecified": answerSDPWithMedia("0.0.0.0", 30000, "passive"),
		"missing":     answerSDPWithMedia("", 30000, "passive"),
	} {
		t.Run(name, func(t *testing.T) {
			z := &mockZLM{port: 41000}
			z.online.Store(true)
			inv := &mockInviter{outcome: uac.InviteOutcome{
				RequestID: "req-tcp-active", RequestSent: true, FinalStatus: 200,
				AckSucceeded: true, AnswerSDP: body,
			}}
			ch := aChannel()
			ch.StreamTransport = "TCP-Active"
			s, _, _ := newSvc(t, z, inv, onlineDevice(), ch)

			_, err := s.Start(context.Background(), onlineDevice().DeviceID, ch.ChannelID)
			require.NoError(t, err)
			target, ok := z.lastConnect.Load().(zlm.ConnectRtpServerRequest)
			require.True(t, ok)
			require.Equal(t, onlineDevice().IP, target.DestHost)
		})
	}
}

// 应答里没有可用视频端口时必须显式失败,并且把设备侧会话收掉(BYE + 关收流端),
// 不能留下一条占着设备的僵尸会话。
func TestStartTcpActiveFailsWhenAnswerHasNoMediaPort(t *testing.T) {
	z := &mockZLM{port: 41000}
	z.online.Store(true)
	inv := &mockInviter{outcome: uac.InviteOutcome{
		RequestID: "req-tcp-active", RequestSent: true, FinalStatus: 200,
		AckSucceeded: true, AnswerSDP: []byte("v=0\r\ns=Play\r\nt=0 0\r\n"),
	}}
	ch := aChannel()
	ch.StreamTransport = "TCP-Active"
	s, _, _ := newSvc(t, z, inv, onlineDevice(), ch)

	_, err := s.Start(context.Background(), onlineDevice().DeviceID, ch.ChannelID)
	require.ErrorContains(t, err, "未给出可回连的视频端口")
	require.Zero(t, z.connectCalls.Load())
	require.GreaterOrEqual(t, inv.byeCalls.Load(), int32(1), "失败后必须给设备发 BYE")
	require.GreaterOrEqual(t, z.closeCalls.Load(), int32(1), "失败后必须关掉收流端")
}

// 设备也声明 a=setup:active(要求由它连平台)时,双方都想当主动方,谁也连不上。
// 这种冲突只能靠改配置解决,错误信息必须点明原因,不能报成超时。
func TestStartTcpActiveRejectsPeerActiveAnswer(t *testing.T) {
	z := &mockZLM{port: 41000}
	z.online.Store(true)
	inv := &mockInviter{outcome: uac.InviteOutcome{
		RequestID: "req-tcp-active", RequestSent: true, FinalStatus: 200,
		AckSucceeded: true, AnswerSDP: answerSDPWithMedia("192.168.10.203", 30000, "active"),
	}}
	ch := aChannel()
	ch.StreamTransport = "TCP-Active"
	s, _, _ := newSvc(t, z, inv, onlineDevice(), ch)

	_, err := s.Start(context.Background(), onlineDevice().DeviceID, ch.ChannelID)
	require.ErrorContains(t, err, "a=setup:active")
	require.Zero(t, z.connectCalls.Load(), "双方都想主动时不该发起回连")
}

// 单端口复用节点只有一个常驻监听 + SSRC 分流,没有出站连接能力。这条组合必须
// 在发 INVITE 之前就拒,否则设备已经按主动模式协商成功、平台却永远连不上。
func TestStartTcpActiveRejectsSinglePortNode(t *testing.T) {
	withFixedAddressPlaybackSettings(t, false, false)
	z := &sharedMockZLM{}
	inv := &mockInviter{}
	ch := aChannel()
	ch.StreamTransport = "TCP-Active"
	s, _, _ := newFixedSvc(t, z, inv, onlineDevice(), ch)
	selected, ok := s.registry.Get(1)
	require.True(t, ok)
	selected.RTPReceiveMode = "single"
	selected.RTPProxyPort = 10000

	_, err := s.Start(context.Background(), onlineDevice().DeviceID, ch.ChannelID)
	require.ErrorContains(t, err, "多端口")
	require.Zero(t, inv.inviteCalls.Load(), "节点模式不匹配时不得发 INVITE")
	require.Zero(t, z.openCalls.Load())
}
