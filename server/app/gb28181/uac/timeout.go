package uac

import (
	"context"
	"time"

	gbconfig "uvplatform.com/uvp-gb28181/app/gb28181/config"
)

// inviteLateAnswerGrace 是 INVITE 等待窗口在"SIP 命令超时"之上多留的宽限。
//
// 见 withInviteAnswerWindow 的说明。2s 的依据是 2026-10-25 在 220 的实测:
// LiveGBS 3.6.10 的 400 落在我方 10s 超时之后约 1s。
const inviteLateAnswerGrace = 2 * time.Second

func withSIPCommandTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, gbconfig.SIPCommandTimeout())
}

// InviteAnswerWindow 是实时点播 INVITE 的完整等待窗口(含迟到终态宽限)。
//
// ⛔⛔ 调用方**不要**再用 SIPCommandTimeout() 给父 ctx 加限制:
// context.WithTimeout 只取更早的那个 deadline,父层一旦卡在 SIP 命令超时,
// 这里的宽限会被整段吃掉,迟到的终态照样丢。2026-10-25 就这么踩过:
// play/service.go 在调用 InviteTracked 前套了 SIPCommandTimeout(),窗口实测
// 仍是 10.0s,直到把这层换成本函数才真正生效。
func InviteAnswerWindow() time.Duration {
	return gbconfig.SIPCommandTimeout() + inviteLateAnswerGrace
}

// playbackInviteWindowMultiplier 是回放 INVITE 等待窗口相对 SIP 命令超时的倍数。
//
// ⛔⛔ 回放 INVITE 常常是**嵌套**的：上级把回放 INVITE 发给下级平台，下级收到后
// 还要先把自己设备侧的回放流拉起来（它同样要等一个 SIP 命令超时），媒体就绪之后
// 才回 200 OK。两级用同一个窗口 ⇒ 上级必然在下级刚好等满的那一刻先放弃并发 CANCEL，
// 下级收到 CANCEL 把自己的 ctx 一起取消 ⇒ 上级只看到「PLAYBACK INVITE 应答超时」，
// 而媒体其实 1~2 秒后就绪。
//
// 2026-10-26 实测（62 上级 / 236 下级，通道 34020000001320000025）：
// 冷启动 14s 才建立，第 10s 被上级 CANCEL；紧接着重试因为设备已预热，1s 就建立。
// 所以窗口取 2 倍：给下级留够"它自己的设备窗口 + 媒体就绪"的时间。
const playbackInviteWindowMultiplier = 2

// PlaybackInviteAnswerWindow 是录像回放 INVITE 的完整等待窗口（含迟到终态宽限）。
func PlaybackInviteAnswerWindow() time.Duration {
	return time.Duration(playbackInviteWindowMultiplier)*gbconfig.SIPCommandTimeout() + inviteLateAnswerGrace
}

// withPlaybackInviteAnswerWindow 给回放 INVITE 一个够跨过"下级二次拉流"的等待窗口。
//
// ⛔ 别改回 withSIPCommandTimeout：那等于要求"上级的等待"与"下级的等待"一样长，
// 下级一旦真的用到自己的超时上沿就必然失败（见 playbackInviteWindowMultiplier）。
// 窗口上限仍受父 ctx 约束（httpserver.handler_timeout 默认 30s）。
func withPlaybackInviteAnswerWindow(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, PlaybackInviteAnswerWindow())
}

// withInviteAnswerWindow 给实时点播的 INVITE 一个比 SIP 命令超时略宽的等待窗口。
//
// ⛔ 为什么不能只用 SIP 命令超时(默认 10s):下级平台/设备常常要等**自己的媒体
// 超时**(也差不多 10s)才给终态应答。2026-10-25 在 220 抓包实测 LiveGBS
// 3.6.10: 收到 INVITE 立即回 `100 Trying`,然后在我方 10s 超时之后约 1s 才回
//
//	400 ack sms[34020000002020000001] failed, res[400] none rtp data receive on udp://…:30050
//
// 原实现在 10s 一到就判失败并 Close,这条唯一的失败原因被整条丢掉,页面只剩
// 一句"点播失败"—— 排查方向(网络/注册/对端媒体)全被抹平。
//
// 代价只有一种:对端一声不吭时最坏多等 inviteLateAnswerGrace。拿到终态就
// **立刻**返回,不多等一毫秒;点播总预算(默认 15s)更紧时,WithTimeout 会自动
// 采父 ctx 的 deadline,窗口不会突破总预算。
func withInviteAnswerWindow(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, InviteAnswerWindow())
}
