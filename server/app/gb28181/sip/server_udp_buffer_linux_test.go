//go:build linux

package sip

import (
	"net"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// TestUDPRecvBufferReachesKernel 验证 SetReadBuffer 真的写进了内核，
// 而不是只在 Go 侧"调用过"。
//
// 背景：DSN/参数类配置最容易出的问题就是"代码写了、日志正常、实际没生效"。
// 今天已经踩过一次同类坑（SQLite 的 _pragma 拼错会被驱动静默忽略），
// 所以这里直接 getsockopt 读回来对账。
//
// 内核语义：setsockopt(SO_RCVBUF, n) 先被裁到 net.core.rmem_max，再翻倍成
// 实际 sk_rcvbuf。所以先按 rmem_max 收敛目标值，否则这条测试在默认 208KB 的
// 机器上必然失败（那不是代码错，是系统参数没调）。
func TestUDPRecvBufferReachesKernel(t *testing.T) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	target := 4 << 20
	if limit := readRmemMax(); limit > 0 && target > limit {
		target = limit
		t.Logf("net.core.rmem_max=%d，目标值收敛到 %d", limit, target)
	}
	require.NoError(t, conn.SetReadBuffer(target))

	raw, err := conn.SyscallConn()
	require.NoError(t, err)

	var got int
	var sockErr error
	require.NoError(t, raw.Control(func(fd uintptr) {
		got, sockErr = unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_RCVBUF)
	}))
	require.NoError(t, sockErr)
	require.GreaterOrEqual(t, got, target, "内核实际接收缓冲小于申请值")
}

// readRmemMax 返回 net.core.rmem_max；读不到返回 0（视为未知）。
func readRmemMax() int {
	raw, err := os.ReadFile("/proc/sys/net/core/rmem_max")
	if err != nil {
		return 0
	}
	limit, convErr := strconv.Atoi(strings.TrimSpace(string(raw)))
	if convErr != nil {
		return 0
	}
	return limit
}
