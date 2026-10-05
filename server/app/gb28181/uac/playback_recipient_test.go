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
