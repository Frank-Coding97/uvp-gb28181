package sip

import (
	"context"
	"net"
	"strings"

	siplib "github.com/emiago/sipgo/sip"

	"uvplatform.cn/uvp-gb28181/app/global/app"

	"go.uber.org/zap"
)

// DeviceLinkSink 接收「可靠传输(TCP/TLS/WS)通道被动断开」通知。
//
// **为什么这活儿归设备域而不是 sip 层**:GB/T 28181-2022 §9.1.1 f) 说「若 TCP 通道
// 断开,则认为 SIP 代理异常掉线」,但「这个端点 = 哪台设备」以及「置离线要连带把
// 通道也置离线、还要写状态事件」是设备域的知识。sip 层只依赖 sipgo + handler,不持有
// DB;把动作抽成接口,一是让这里保持可单测(塞个假 sink 就能验证断开有没有被上抛),
// 二是设备域能直接复用 models.MarkOfflineWithReason 的整套原子语义。
type DeviceLinkSink interface {
	// DeviceLinkLost 在**可靠传输**断开、且确认没有同端点新连接顶上之后被调用。
	//
	// transport 形如 "TCP";remoteAddr 形如 "192.168.1.10:5060"(= 设备注册来源端点)。
	//
	// ⛔ 实现必须是**非阻塞**的:本方法在 sipgo read loop 的退出路径上被同步调用,
	// 在那里做数据库事务会把连接回收拖住(池里的连接要等它返回才彻底释放)。
	DeviceLinkLost(ctx context.Context, transport, remoteAddr string)
}

// WithDeviceLinkSink 装配可靠传输断开接收方(见 DeviceLinkSink)。
//
// 不装配时行为与历史版本**完全一致** —— 断开的连接只进 trace / security,不改设备状态。
func WithDeviceLinkSink(sink DeviceLinkSink) ServerOption {
	return func(options *serverOptions) { options.deviceLinkSink = sink }
}

// linkLossDecision 说明一次「可靠传输断开」为什么被采纳或被忽略。
type linkLossDecision int

const (
	// linkLossFire 该判设备离线。
	linkLossFire linkLossDecision = iota
	// linkLossSkipMuted 平台正在关停 —— 连接是平台自己关的。
	linkLossSkipMuted
	// linkLossSkipNotReliable 不是可靠传输(UDP 没有「通道断开」这回事)。
	linkLossSkipNotReliable
	// linkLossSkipNoAddr 报文里没带可用的远端地址,无法定位设备。
	linkLossSkipNoAddr
	// linkLossSkipSuperseded 同端点已有新连接顶上 —— 是重连,不是掉线。
	linkLossSkipSuperseded
)

// decideLinkLoss 判定一次「可靠传输断开」该不该升级成设备离线,并给出要判定的端点。
//
// **为什么抽成纯函数**(依赖以参数传入而不是直接读 s):要验证的规则里有一条
// 「同端点已有新连接时不判掉线」,它对应"平台侧先建好新连接、旧连接的 read loop
// 才退出"这个时序。若不做注入,就得起一套真 SIP 栈并精确复现该时序 —— 那是集成
// 测试的活,不该挡住这条判断本身的可测性。
func decideLinkLoss(
	info siplib.TransportReadProps,
	muted bool,
	hasConnection func(transport, addr string) bool,
) (string, linkLossDecision) {
	if muted {
		return "", linkLossSkipMuted
	}
	// 只有可靠传输才有「通道断开」这个事件。sipgo 当前也只从 TCP/TLS/WS 的 read loop
	// 调 close observer;这道判断是防止将来把它接到 UDP 上时,把「无连接」误判成「设备掉线」。
	if !siplib.IsReliable(siplib.NetworkToLower(info.Transport)) {
		return "", linkLossSkipNotReliable
	}
	if info.RemoteAddr == nil {
		return "", linkLossSkipNoAddr
	}
	remoteAddr := strings.TrimSpace(info.RemoteAddr.String())
	if remoteAddr == "" {
		return "", linkLossSkipNoAddr
	}
	// ⛔ 不能只判空串:`net.TCPAddr{}` 的零值 String() 是 **":0"**(不是空串),
	// 连接尚未完全建立时 sipgo 可能给出这种零值地址。带不出主机名或端口的地址
	// 定位不到任何设备,在这里就挡掉,免得把无意义端点一路传到设备域去查库。
	if host, port, err := net.SplitHostPort(remoteAddr); err != nil {
		return "", linkLossSkipNoAddr
	} else if strings.TrimSpace(host) == "" || strings.TrimSpace(port) == "0" {
		return "", linkLossSkipNoAddr
	}
	// 同端点已有新连接顶上 → 这是设备自愈重连(或平台侧重连窗口)的中间态,**不是**掉线。
	// 少了这一问,一次正常重连会被记成「设备掉线」+「设备上线」两条事件,把事件表
	// 污染成全是抖动,真故障反而看不出来。
	if hasConnection != nil && hasConnection(info.Transport, remoteAddr) {
		return remoteAddr, linkLossSkipSuperseded
	}
	return remoteAddr, linkLossFire
}

// notifyLinkLoss 是 close observer 的最后一环:把「不构成重连的断开」上抛给设备域。
func (s *Server) notifyLinkLoss(info siplib.TransportReadProps) {
	if s == nil || s.deviceLinkSink == nil {
		return
	}
	remoteAddr, decision := decideLinkLoss(info, s.linkLossMuted.Load(), s.HasConnection)
	switch decision {
	case linkLossFire:
		s.deviceLinkSink.DeviceLinkLost(context.Background(), info.Transport, remoteAddr)
	case linkLossSkipSuperseded:
		// 与本包其他日志一致走 app.Log(zap 风格)。Server.logger 是 slog 句柄,
		// 用途仅限构造期交给 sipgo,业务日志不用它。
		app.Log(context.Background()).Named("gb28181.sip").Info(
			"SIP 可靠传输断开,但同端点已有新连接,按重连处理",
			zap.String("event", "gb28181.sip.link_superseded"),
			zap.String("stage", "link_loss"), zap.String("outcome", "recovered"),
			zap.String("transport", info.Transport),
			zap.String("remote_addr", remoteAddr))
	}
}

// HasConnection 报告平台上是否仍存在指向 remoteAddr 的活跃**可靠传输**连接。
//
// 用途见 notifyLinkLoss:断开回调里先问一句「这个端点是不是已经有新连接顶上了」。
// 同端点重连(平台侧先建新连接、旧 read loop 才退出)时,不查这一问就会误判掉线。
func (s *Server) HasConnection(transport, remoteAddr string) bool {
	if s == nil || s.ua == nil || remoteAddr == "" {
		return false
	}
	network := siplib.NetworkToLower(transport)
	if !siplib.IsReliable(network) {
		return false
	}
	tp := s.ua.TransportLayer()
	if tp == nil {
		return false
	}
	conn, err := tp.GetConnection(network, remoteAddr)
	if err != nil || conn == nil {
		return false
	}
	// ⛔ GetConnection 会给连接 Ref(+1)(connectionPool.Get 的契约写明
	// "Make sure you TryClose after finish")。这里只查询、不写报文,必须立刻还回去:
	// 引用计数只增不减会让这条连接**永远关不掉**(TryClose 只在 ref 归零时才真关 socket)。
	_, _ = conn.TryClose()
	return true
}
