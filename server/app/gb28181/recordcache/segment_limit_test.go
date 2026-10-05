package recordcache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gbplayback "uvplatform.cn/uvp-gb28181/app/gb28181/playback"
)

// TestSegmentMediaLimitScalesWithDownloadSpeed 钉住分片长度的唯一口径：
// 墙钟预算 × 倍速 × 安全系数。
//
// ⛔ 这条测试防的是"退回固定分钟数"：曾经用固定的 20 分钟媒体时长，
// 等价于假设有效倍速只有 0.67×，而实测是 4× —— 结果是 30 分钟录像被无谓
// 切成 2 片、用户拿到 2 个文件。谁把折算换回常量，这里立刻变红。
func TestSegmentMediaLimitScalesWithDownloadSpeed(t *testing.T) {
	cases := []struct {
		speed int
		want  time.Duration
	}{
		// 25 分钟墙钟 × 倍速 × 0.9（8× 折算 180 分钟，被 SegmentMediaCap 兜回 120）
		{1, 22*time.Minute + 30*time.Second},
		{2, 45 * time.Minute},
		{4, 90 * time.Minute},
		{8, 120 * time.Minute},
	}
	for _, c := range cases {
		require.Equal(t, c.want, segmentMediaLimit(c.speed), "倍速 %d× 的单片媒体上限", c.speed)
	}
}

// TestSegmentMediaLimitFallsBackAndCaps 覆盖两个边界：
// 倍速未知时按兜底倍速折算；异常大的倍速被绝对上限兜住。
func TestSegmentMediaLimitFallsBackAndCaps(t *testing.T) {
	require.Equal(t, segmentMediaLimit(DefaultDownloadSpeed), segmentMediaLimit(0),
		"倍速取不到时按兜底倍速折算，而不是退化成 0 长度")
	require.Equal(t, segmentMediaLimit(DefaultDownloadSpeed), segmentMediaLimit(-1),
		"负倍速同样走兜底（不会算出负时长）")
	require.Equal(t, SegmentMediaCap, segmentMediaLimit(16),
		"倍速大到折算超过绝对上限时封顶，避免请求区间失控")
}

// TestSegmentWallBudgetStaysWithinSessionCeiling 钉住两条与"会话时长"有关的口径，
// 它们的分量**差很多**，不要混着读。
//
// ⚠️ 本用例原名 `…LeavesHeadroomBeforeSessionHardLimit`，论据是"回放会话有 30 分钟硬限"。
// 那条线**不存在**（2026-10-05 查证）：`playback.DefaultMaxSession` 只是 Registry 在没拿到
// 配置时的兜底，真实值由 bootstrap 从配置键 `gb28181.playback.max_session_sec` 注入，
// 默认 86400 秒 = **24 小时**。25 分钟预算的来由见 `SegmentWallBudget` 的注释。
//
//  1. `SegmentWallGrace > SegmentWallBudget` —— **真正的语义不变量**。
//     墙钟保护必须晚于正常预算，否则正常推进的会话会被当成异常、每片都提前掐断。
//  2. `< playback.DefaultMaxSession` —— **纯防御，不是当前约束**。
//     它防的是"有人把这个兜底常量改小"：真把会话上限压到 25 分钟以下，会话会被硬回收、
//     那一片的尾部不进分片清单，而任务仍显示成功 —— 用户拿到一个缺尾的文件。
//     ⛔ 别再拿它论证"分片长度只能这么小"。
func TestSegmentWallBudgetStaysWithinSessionCeiling(t *testing.T) {
	require.Greater(t, SegmentWallGrace, SegmentWallBudget,
		"墙钟保护要晚于正常预算，否则会把正常推进的会话误判成异常")
	require.Less(t, SegmentWallBudget, gbplayback.DefaultMaxSession,
		"防御：单片墙钟预算若超过会话兜底上限，收尾时机就交给硬回收 = 丢尾部")
	require.Less(t, SegmentWallGrace, gbplayback.DefaultMaxSession,
		"防御：墙钟保护若超过会话兜底上限，这个保护就形同虚设")
}

// TestCommonRecordingFitsInOneSegment 直接表达产品口径：
// 最常见的半小时录像，在 4× 设备上必须一片装下 = 用户拿到一个文件。
func TestCommonRecordingFitsInOneSegment(t *testing.T) {
	require.GreaterOrEqual(t, segmentMediaLimit(DefaultDownloadSpeed), 30*time.Minute,
		"4× 设备上 30 分钟录像必须一片拉完（否则用户会看到多个文件）")
}

// TestOneHourRecordingFitsInOneSegment 是 2026-10-04 加的产品口径：
// 1 小时录像在 4× 设备上也要一片装下。
//
// ⛔ 退回 0.5 时这里立刻红（4× 只给 50 分钟 ⇒ 1 小时被切成 50+10 两片）。
// 之所以敢把系数提到 0.9，是因为墙钟保护（`SegmentWallGrace`）会先兜住：倍速不达标时
// 安全收尾、游标按**实际录到的**内容推进 ⇒ 只是多切一片，不会丢尾部。
// （⛔ 不是靠"28 < 30 分钟会话硬限"——那条线不存在，见上一条用例的说明。）
func TestOneHourRecordingFitsInOneSegment(t *testing.T) {
	require.GreaterOrEqual(t, segmentMediaLimit(DefaultDownloadSpeed), time.Hour,
		"4× 设备上 1 小时录像必须一片拉完（否则用户会看到多个文件）")
	require.Greater(t, segmentMediaLimit(DefaultDownloadSpeed), 50*time.Minute,
		"旧口径 0.5 只给 50 分钟，正是 1 小时被切两片的原因 —— 别退回去")
}
