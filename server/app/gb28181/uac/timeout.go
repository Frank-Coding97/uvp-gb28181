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
