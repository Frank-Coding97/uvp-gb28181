package management

import (
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/zlm"
)

func TestIsPlatformControlSession(t *testing.T) {
	cases := []struct {
		name    string
		session zlm.Session
		want    bool
	}{
		{
			name:    "回环地址上的 HTTP 是平台自己调 API",
			session: zlm.Session{PeerIP: "127.0.0.1", Type: "tcp", TypeID: "mediakit::HttpSession"},
			want:    true,
		},
		{
			name:    "IPv6 回环同样排除",
			session: zlm.Session{PeerIP: "::1", TypeID: "mediakit::HttpSession"},
			want:    true,
		},
		{
			name:    "IPv6 回环带 zone 也要识别",
			session: zlm.Session{PeerIP: "::1%lo0", TypeID: "mediakit::HttpSession"},
			want:    true,
		},
		{
			name:    "localhost 字面量",
			session: zlm.Session{PeerIP: "localhost", TypeID: "mediakit::HttpSession"},
			want:    true,
		},
		{
			name:    "回环 RTSP 是真实媒体会话，保留",
			session: zlm.Session{PeerIP: "127.0.0.1", TypeID: "mediakit::RtspSession"},
			want:    false,
		},
		{
			name:    "回环 RTP 是真实媒体会话，保留",
			session: zlm.Session{PeerIP: "127.0.0.1", TypeID: "mediakit::RtpSession"},
			want:    false,
		},
		{
			name:    "回环 ICE 是节点间传输，保留",
			session: zlm.Session{PeerIP: "127.0.0.1", TypeID: "mediakit::iceSession"},
			want:    false,
		},
		{
			name:    "外部 IP 的 HTTP（HTTP-FLV 观众）不能误伤",
			session: zlm.Session{PeerIP: "10.1.2.3", TypeID: "mediakit::HttpSession"},
			want:    false,
		},
		{
			name:    "没有对端地址的会话不当成自身连接",
			session: zlm.Session{PeerIP: "", TypeID: "mediakit::HttpSession"},
			want:    false,
		},
	}
	for _, current := range cases {
		t.Run(current.name, func(t *testing.T) {
			require.Equal(t, current.want, isPlatformControlSession(current.session))
		})
	}
}

func TestBusinessSessionCountDropsPlatformControlSessions(t *testing.T) {
	sessions := []zlm.Session{
		{PeerIP: "127.0.0.1", Type: "tcp", TypeID: "mediakit::HttpSession"},
		{PeerIP: "127.0.0.1", TypeID: "mediakit::HttpSession"},
		{PeerIP: "113.132.90.21", TypeID: "mediakit::RtpSession"},
		{PeerIP: "223.160.211.246", TypeID: "mediakit::iceSession"},
	}
	require.Equal(t, 2, businessSessionCount(sessions))
	require.Equal(t, 0, businessSessionCount(nil))
	require.Equal(t, 0, businessSessionCount([]zlm.Session{
		{PeerIP: "127.0.0.1", TypeID: "mediakit::HttpSession"},
	}))
}

func TestVisibleSessionsKeepsBusinessOrder(t *testing.T) {
	sessions := []zlm.Session{
		{ID: "a", PeerIP: "127.0.0.1", TypeID: "mediakit::HttpSession"},
		{ID: "b", PeerIP: "113.132.90.21", TypeID: "mediakit::RtpSession"},
		{ID: "c", PeerIP: "127.0.0.1", TypeID: "mediakit::HttpSession"},
		{ID: "d", PeerIP: "223.160.211.246", TypeID: "mediakit::iceSession"},
	}
	filtered := visibleSessions(sessions)
	require.Len(t, filtered, 2)
	require.Equal(t, "b", filtered[0].ID)
	require.Equal(t, "d", filtered[1].ID)
	// 原切片不能被就地改写，调用方可能还要用。
	require.Len(t, sessions, 4)
	require.Empty(t, visibleSessions(nil))
}
