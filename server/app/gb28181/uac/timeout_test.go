package uac

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gbconfig "uvplatform.com/uvp-gb28181/app/gb28181/config"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

func TestWithSIPCommandTimeoutUsesConfiguredDefault(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	app.ConfigYml = nil

	ctx, cancel := withSIPCommandTimeout(context.Background())
	defer cancel()
	deadline, ok := ctx.Deadline()
	require.True(t, ok)
	require.InDelta(t, 10, time.Until(deadline).Seconds(), 0.5)
}

// ⛔⛔ 回归：回放 INVITE 的窗口必须**明显长于**一次 SIP 命令超时。
//
// 级联回放是嵌套的：下级收到上级的回放 INVITE 之后，还要先等它自己设备侧的
// 回放流起来（下级同样卡一个 SIP 命令超时）才回 200 OK。窗口取成一样长，
// 上级就会在下级刚好等满的那一刻先放弃并发 CANCEL，下级跟着把自己的 ctx
// 取消 ⇒ 上级只看到「PLAYBACK INVITE 应答超时」（2026-10-26 62←236 实测：
// 冷启动 14s 建立，第 10s 被 CANCEL；紧接着重试 1s 就建立）。
// 改回 withSIPCommandTimeout 会让这个用例直接失败。
func TestPlaybackInviteAnswerWindowExceedsSingleCommandTimeout(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	app.ConfigYml = nil

	soft := gbconfig.SIPCommandTimeout()
	window := PlaybackInviteAnswerWindow()
	require.GreaterOrEqual(t, window, 2*soft,
		"回放窗口必须给下级留够「它自己的设备窗口 + 媒体就绪」的时间")
	require.Greater(t, window, InviteAnswerWindow(),
		"回放窗口必须比实时点播窗口更宽：回放多一跳下级二次拉流")

	ctx, cancel := withPlaybackInviteAnswerWindow(context.Background())
	defer cancel()
	deadline, ok := ctx.Deadline()
	require.True(t, ok)
	require.InDelta(t, (2*soft + inviteLateAnswerGrace).Seconds(), time.Until(deadline).Seconds(), 0.5)
}

// 父 ctx 更紧时，窗口自动被父预算截断，不能反过来突破调用方的总预算。
func TestPlaybackInviteAnswerWindowCapsAtParentBudget(t *testing.T) {
	parent, parentCancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer parentCancel()

	ctx, cancel := withPlaybackInviteAnswerWindow(parent)
	defer cancel()
	deadline, ok := ctx.Deadline()
	require.True(t, ok)
	require.LessOrEqual(t, time.Until(deadline), 1200*time.Millisecond)
}
