package sip

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSIPTransportIsUDP 锁住 transport 归类。
//
// 这里判错不是"多走一条分支"这么轻：UDP 被判成非 UDP 就会掉进
// srv.ListenAndServe(TCP) 分支，监听直接失败，SIP 整个起不来。
// 配置里 UDP 的大小写与后缀历来不统一（udp / UDP / udp4 / udp6），
// 所以必须归一化后判断。
func TestSIPTransportIsUDP(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"udp", true},
		{"UDP", true},
		{"Udp", true},
		{"udp4", true},
		{"udp6", true},
		{" udp ", true},
		{"tcp", false},
		{"TCP", false},
		{"tcp4", false},
		{"ws", false},
		{"", false},
		{"udp-tcp", false},
	}
	for _, c := range cases {
		require.Equal(t, c.want, sipTransportIsUDP(c.in), "transport=%q", c.in)
	}
}

// TestRecvBufferLimitWarning 锁住"申请值超过 rmem_max 要告警"这条判据。
//
// 静默截断是这件事最大的坑：内核不报错、日志没异常，丢包照旧，
// 看起来就像"缓冲改了但没用"。所以边界必须钉死。
func TestRecvBufferLimitWarning(t *testing.T) {
	const target = 4 << 20 // 4MB

	t.Run("系统上限够大不告警", func(t *testing.T) {
		require.Empty(t, recvBufferLimitWarning(target, 8<<20))
	})
	t.Run("申请值等于上限不告警", func(t *testing.T) {
		require.Empty(t, recvBufferLimitWarning(target, target))
	})
	t.Run("申请值超过上限要告警且带修复命令", func(t *testing.T) {
		msg := recvBufferLimitWarning(target, 212992)
		require.NotEmpty(t, msg)
		require.Contains(t, msg, "rmem_max")
		require.Contains(t, msg, "4194304") // 修复命令里要出现目标值，运维可直接复制
	})
	t.Run("上限未知时不误报", func(t *testing.T) {
		require.Empty(t, recvBufferLimitWarning(target, 0))
		require.Empty(t, recvBufferLimitWarning(target, -1))
	})
}
