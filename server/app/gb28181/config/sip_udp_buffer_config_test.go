package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSIPUDPRecvBufferKBFrom 锁住 SIP UDP 接收缓冲的解析与兜底。
//
// 这个值配小了会直接表现为"设备注册莫名超时"（内核丢包，日志里连请求都没有），
// 所以越界一律回默认，而不是尽量用——宁可给一个够大的安全值。
func TestSIPUDPRecvBufferKBFrom(t *testing.T) {
	key := SIPUDPRecvBufferKBConfigKey

	t.Run("未配置用默认 4MB", func(t *testing.T) {
		src := fakeSource{values: map[string]interface{}{}, ints: map[string]int{}}
		require.Equal(t, DefaultSIPUDPRecvBufferKB, SIPUDPRecvBufferKBFrom(src))
	})

	t.Run("nil 配置源用默认", func(t *testing.T) {
		require.Equal(t, DefaultSIPUDPRecvBufferKB, SIPUDPRecvBufferKBFrom(nil))
	})

	t.Run("合法配置按原值", func(t *testing.T) {
		src := fakeSource{
			values: map[string]interface{}{key: 8192},
			ints:   map[string]int{key: 8192},
		}
		require.Equal(t, 8192, SIPUDPRecvBufferKBFrom(src))
	})

	for _, bad := range []int{0, -1, MaxSIPUDPRecvBufferKB + 1} {
		t.Run("越界回默认", func(t *testing.T) {
			src := fakeSource{
				values: map[string]interface{}{key: bad},
				ints:   map[string]int{key: bad},
			}
			require.Equal(t, DefaultSIPUDPRecvBufferKB, SIPUDPRecvBufferKBFrom(src), "bad=%d", bad)
		})
	}
}

// TestSIPUDPRecvBufferDefaultIsLargeEnough 守住"默认值必须能装下千级突发"这条底线。
//
// 208KB 的系统默认值只够约 300 个 SIP 报文；万级设备同时注册时需要
// 数千个报文的缓冲。这条断言防止有人把默认值调小到失去意义。
func TestSIPUDPRecvBufferDefaultIsLargeEnough(t *testing.T) {
	require.GreaterOrEqual(t, DefaultSIPUDPRecvBufferKB, 2048, "默认缓冲至少 2MB")
	require.GreaterOrEqual(t, MaxSIPUDPRecvBufferKB, DefaultSIPUDPRecvBufferKB*4, "上限要留出调节余地")
	require.LessOrEqual(t, MaxSIPUDPRecvBufferKB, 65536, "上限别离谱到几百 MB")
}
