package uac

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 回放 dialog 的远端目标是设备在 INVITE 应答里给出的 Contact(逐跳可达地址),
// 与平台自身域(deviceURI,常是公网地址)不是同一个东西。
//
// TEARDOWN 的 INFO 曾经漏掉 Contact 覆盖,被发往平台自己的公网地址,设备永远
// 收不到,表现为"拆除时设备零应答"。这里把"必须优先采用 Contact"钉住,并覆盖
// 缺失/不可解析时回落到 deviceURI 的两条分支。
func TestPlaybackControlRecipientPrefersDeviceContact(t *testing.T) {
	u := &UAC{domain: "192.0.2.10"}
	recipient := u.playbackControlRecipient(PlaybackDialogMetadata{
		ChannelID:    "34020000001320000001",
		RemoteTarget: "<sip:34020000001320000001@192.168.10.204:5060>",
	})
	require.Equal(t, "192.168.10.204", recipient.Host)
	require.Equal(t, 5060, recipient.Port)
}

func TestPlaybackControlRecipientFallsBackWhenContactMissing(t *testing.T) {
	u := &UAC{domain: "192.0.2.10"}
	recipient := u.playbackControlRecipient(PlaybackDialogMetadata{ChannelID: "34020000001320000001"})
	require.Equal(t, "192.0.2.10", recipient.Host)
	require.Equal(t, "34020000001320000001", recipient.User)
}

func TestPlaybackControlRecipientIgnoresUnparsableContact(t *testing.T) {
	u := &UAC{domain: "192.0.2.10"}
	recipient := u.playbackControlRecipient(PlaybackDialogMetadata{
		ChannelID:    "34020000001320000001",
		RemoteTarget: "not-a-sip-uri",
	})
	require.Equal(t, "192.0.2.10", recipient.Host)
	require.Equal(t, "34020000001320000001", recipient.User)
}

// 对端把 Contact 填成"我方平台编码 + 我方监听地址"时(实测 WVP-Pro 2.7.4 级联),
// 照抄会让 TEARDOWN 的 INFO 发回自己并当场拿到 481,拆除永远不收敛。
// 此时必须退到 INVITE 的真实收件人 —— 那个地址确实收到过我们的 INVITE 并回了 200 OK。
func TestPlaybackControlRecipientRejectsSelfReferentialContact(t *testing.T) {
	u := &UAC{
		serverID:    "34020000012000000001",
		domain:      "3402000001",
		advertiseIP: "192.168.10.220",
		sipPort:     52064,
	}
	recipient := u.playbackControlRecipient(PlaybackDialogMetadata{
		ChannelID:    "37010301021320000002",
		RemoteTarget: "<sip:34020000012000000001@192.168.10.220:52064>",
		Destination:  "sip:37010301021320000002@172.21.0.3:8160",
	})
	require.Equal(t, "172.21.0.3", recipient.Host)
	require.Equal(t, 8160, recipient.Port)
	require.Equal(t, "37010301021320000002", recipient.User)
}

func TestPlaybackControlRecipientFallsBackToInviteDestination(t *testing.T) {
	u := &UAC{
		serverID:    "34020000012000000001",
		domain:      "3402000001",
		advertiseIP: "192.168.10.220",
		sipPort:     52064,
	}
	recipient := u.playbackControlRecipient(PlaybackDialogMetadata{
		ChannelID:   "37010301021320000002",
		Destination: "sip:37010301021320000002@172.21.0.3:8160",
	})
	require.Equal(t, "172.21.0.3", recipient.Host)
	require.Equal(t, 8160, recipient.Port)
	require.Equal(t, "37010301021320000002", recipient.User)
}

// 同机部署的对端(模拟器/网关)和平台共用出口 IP,属于合法 Contact,不能判成自指。
func TestPlaybackControlRecipientKeepsColocatedDeviceContact(t *testing.T) {
	u := &UAC{
		serverID:    "34020000012000000001",
		domain:      "3402000001",
		advertiseIP: "192.168.10.220",
		sipPort:     52064,
	}
	recipient := u.playbackControlRecipient(PlaybackDialogMetadata{
		ChannelID:    "34020000001320000001",
		RemoteTarget: "<sip:34020000001320000001@192.168.10.220:5060>",
		Destination:  "sip:34020000001320000001@192.168.10.220:5060",
	})
	require.Equal(t, "192.168.10.220", recipient.Host)
	require.Equal(t, 5060, recipient.Port)
	require.Equal(t, "34020000001320000001", recipient.User)
}
