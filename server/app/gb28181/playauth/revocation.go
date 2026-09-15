package playauth

import (
	"sync/atomic"
	"time"
)

// revokedBefore 播放授权全局失效分界(Unix 秒):IssuedAt 早于该时刻的 token 一律拒绝。
// 设备归属调整 / 取消共享时 BumpRevocation,替代逐 token 撤销。
var revokedBefore atomic.Int64

// BumpRevocation 使所有签发时刻早于 now 的播放授权立即失效。
func BumpRevocation(now time.Time) {
	if now.IsZero() {
		now = time.Now()
	}
	revokedBefore.Store(now.UTC().Unix())
}

// RevokedBefore 返回当前失效分界(0 = 从未失效过),供测试断言。
func RevokedBefore() int64 {
	return revokedBefore.Load()
}
