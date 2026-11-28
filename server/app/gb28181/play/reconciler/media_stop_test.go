package reconciler

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// mediaStoppedCleanupError 模拟 play.stopCompletionError 的语义：
// 媒体已经确定停止，只剩 BYE 收尾失败（对端回 481，平台重启后很常见）。
type mediaStoppedCleanupError struct{}

func (mediaStoppedCleanupError) Error() string {
	return `BYE 失败: Invite failed with response: SIP/2.0 481 Call/Transaction Does Not Exist`
}

func (mediaStoppedCleanupError) MediaStopCompleted() bool { return true }

// TestReconcileTreatsMediaStoppedCleanupAsCleaned 锁住「媒体没了就算清掉」：
//
// 如果把它当失败，这条会话会永远留在待清理里，而每次新点播又会复用它，
// 于是该通道从此再也点不开（播放器直接 websocketError，点播接口却一路 200）。
func TestReconcileTreatsMediaStoppedCleanupAsCleaned(t *testing.T) {
	_, _ = newBackgroundRuntime(t)
	stopper := &fakeStopper{stopErr: mediaStoppedCleanupError{}}
	r := New(0, stopper,
		WithMediaProbe(&fakeProbe{}),
		WithChannelLister(&fakeLister{channels: gbmodels.GbChannelList{makeChannel("stream-1")}}))

	stats := r.runOnce(context.Background())
	require.Equal(t, 1, stats.Scanned)
	require.Equal(t, 1, stats.Cleaned)
	require.Equal(t, 0, stats.Failed)
	require.Len(t, stopper.calls, 1)
}

// TestReconcileStillFailsOnRealCleanupError 保证「媒体没停」的失败仍然算失败，
// 不能因为上面的放宽把真正的清理故障一起吞掉。
func TestReconcileStillFailsOnRealCleanupError(t *testing.T) {
	_, _ = newBackgroundRuntime(t)
	stopper := &fakeStopper{stopErr: errors.New("zlm unreachable")}
	r := New(0, stopper,
		WithMediaProbe(&fakeProbe{}),
		WithChannelLister(&fakeLister{channels: gbmodels.GbChannelList{makeChannel("stream-1")}}))

	stats := r.runOnce(context.Background())
	require.Equal(t, 1, stats.Failed)
	require.Equal(t, 0, stats.Cleaned)
}

func TestMediaStopReached(t *testing.T) {
	require.True(t, mediaStopReached(nil))
	require.True(t, mediaStopReached(mediaStoppedCleanupError{}))
	require.False(t, mediaStopReached(errors.New("plain failure")))
	require.True(t, mediaStopReached(wrappedCleanup{mediaStoppedCleanupError{}}))
}

// wrappedCleanup 确认判定走 errors.As，被包装过也要能识别。
type wrappedCleanup struct{ err error }

func (w wrappedCleanup) Error() string { return "wrapped: " + w.err.Error() }
func (w wrappedCleanup) Unwrap() error { return w.err }
