package uac

import (
	"testing"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
)

// SUBSCRIBE 必须带 Contact（RFC 3265 §7.1），否则对端有权直接 400。
//
// 现场形态（2026-10 级联实测）：上一级用本 UAC 向下级订阅移动位置 / 报警 / 云台
// 位置，下级全部回 `400 Invalid subscription dialog`，三类订阅永远起不来；本平台
// 自己作为下级时的接收侧也把「没有 Contact」判成对话框不合法。根因就是这里原来
// 传了 includeContact=false。
func TestBuildSubscribeRequestCarriesContact(t *testing.T) {
	u := &UAC{serverID: "37010000002000000001", domain: "3701000000", sipPort: 5060, advertiseIP: "10.0.0.5"}
	req, err := u.buildSubscribeRequest(SubscriptionRequest{
		DeviceID:    "34020000002000000001",
		Destination: "124.236.76.233:15060",
		Transport:   "UDP",
		Event:       "Catalog;id=7",
		Body:        []byte(`<Query><CmdType>Catalog</CmdType></Query>`),
		Expires:     3600,
	})
	require.NoError(t, err)

	contact := req.Contact()
	require.NotNil(t, contact, "SUBSCRIBE 缺 Contact 会被对端判成非法对话框")
	require.Equal(t, "sip:"+u.serverID+"@10.0.0.5:5060", contact.Address.String())
	require.Equal(t, u.serverID, contact.Address.User)
	require.Equal(t, "10.0.0.5", contact.Address.Host)
	require.EqualValues(t, u.sipPort, contact.Address.Port)

	// Event 头原样透传：目录订阅的 `;id=` 由上层决定，不能在传输层被吃掉。
	require.Equal(t, "Catalog;id=7", req.GetHeader("Event").Value())
	require.Equal(t, uint32(1), req.CSeq().SeqNo)
	require.Equal(t, sip.SUBSCRIBE, req.CSeq().MethodName)
}
