package device

import (
	"context"
	"net"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"

	"go.uber.org/zap"
)

// linkLossBudget 单次离线判定的整体时间预算(解析地址 + 查设备 + 事务置离线)。
const linkLossBudget = 5 * time.Second

// linkLossWarnMessages 是「跳过离线判定」的固定原因文案。日志门禁要求 logger
// message 为编译期常量，所以原因以常量形式集中在这里，warn 按常量分派后再落日志；
// 未登记的原因不允许把变量直接交给 logger（那会让门禁形同虚设）。
const (
	linkLossAddrUnparsable    = "可靠传输断开的远端地址无法拆分主机与端口,跳过离线判定"
	linkLossPortInvalid       = "可靠传输断开的远端端口不是合法数字,跳过离线判定"
	linkLossDeviceQueryFailed = "按端点查设备失败,跳过离线判定"
	linkLossMarkOfflineFailed = "置设备离线失败"
	linkLossUnclassified      = "可靠传输断开处置跳过(原因未分类)"
)

// LinkWatcher 把「可靠传输通道断开」翻译成设备离线状态。
//
// **它实现 sip 侧的 DeviceLinkSink,但刻意不 import sip** —— sip → handler → device
// 已经是一条既有依赖链,device 反向 import sip 会成环。Go 的接口是隐式实现,只要
// [LinkWatcher.DeviceLinkLost] 的签名对得上就行,bootstrap 负责把两者接起来。
//
// 对应标准:GB/T 28181-2022 §9.1.1 f)「若 TCP 通道断开,则认为 SIP 代理异常掉线」。
// 它与心跳超时扫描器是**两条独立通路**,各有不可替代的覆盖面:
//   - 心跳超时(offline_scanner):兜底,要等 keepalive_interval × timeout_count(缺省 180s);
//     但它是**唯一**能发现 UDP 设备掉线的途径 —— UDP 没有连接,没有"断开"这个事件。
//   - 本条:仅覆盖 TCP/TLS/WS,但**瞬时**。拔网线 / 设备掉电 / 进程崩溃都会被立刻发现,
//     不必再干等三个心跳周期。
type LinkWatcher struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewLinkWatcher 构造;db 为 nil 时所有回调静默退化(便于 db-less 的单测装配)。
func NewLinkWatcher(db *gorm.DB, logger *zap.Logger) *LinkWatcher {
	return &LinkWatcher{db: db, logger: logger}
}

// DeviceLinkLost 实现 sip 侧的上抛契约。签名必须与 sip.DeviceLinkSink 一致。
func (w *LinkWatcher) DeviceLinkLost(_ context.Context, transport, remoteAddr string) {
	if w == nil || w.db == nil {
		return
	}
	// ⛔ 必须异步:sipgo 在 read loop 的**退出路径**上同步调 close observer,这里若直接
	// 开事务,连接回收(池里删旧连接、fd 释放)要等事务提交才继续走完。
	go w.markEndpointOffline(transport, remoteAddr)
}

func (w *LinkWatcher) markEndpointOffline(transport, remoteAddr string) {
	ctx, cancel := context.WithTimeout(context.Background(), linkLossBudget)
	defer cancel()

	host, portText, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		w.warn(linkLossAddrUnparsable, transport, remoteAddr, err)
		return
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 {
		w.warn(linkLossPortInvalid, transport, remoteAddr, err)
		return
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return
	}

	// 按「注册来源端点」定位设备 —— 这个 ip:port 就是设备注册时 req.Source(),
	// 也就是 platform 记录在 gb_devices 里的那一对(见 handler/register.go)。
	var dev gbmodels.GbDevice
	query := w.db.WithContext(ctx).
		Where("ip = ? AND port = ? AND status = ?", host, port, gbmodels.DeviceStatusOnline).
		Limit(1).
		Find(&dev)
	if query.Error != nil {
		w.warn(linkLossDeviceQueryFailed, transport, remoteAddr, query.Error)
		return
	}
	if query.RowsAffected == 0 {
		// 两种可能,都**不该**置离线:
		//   a) 设备断链后已重连到新端点(NAT 换端口)并重新注册 —— 记录里的 ip/port
		//      已是新值,按旧端点自然查不到;
		//   b) 该端点本来就不属于任何在线设备(伪造报文 / 设备早已离线)。
		return
	}
	// 设备注册时声明的传输方式必须与断开的一致。否则会出现「这台设备用 UDP 注册,
	// 而某个恰好同地址的 TCP 连接断了」(同机另一个程序)把它牵连离线的情况。
	if !strings.EqualFold(strings.TrimSpace(dev.Transport), strings.TrimSpace(transport)) {
		return
	}

	// MarkOfflineWithReason 内部对「已离线」是幂等的:同端点被上抛多次(多代连接先后
	// 退出)只有第一次真正落库并记事件。
	if err := gbmodels.MarkOfflineWithReason(
		ctx, dev.DeviceID, gbmodels.DeviceEventLinkClosed, gbmodels.DeviceEventSourceLinkWatcher,
	); err != nil {
		w.warn(linkLossMarkOfflineFailed, transport, remoteAddr, err)
		return
	}
	if w.logger != nil {
		w.logger.Info("设备可靠传输通道断开,已判离线",
			zap.String("event", "gb28181.device.link_closed_offline"),
			zap.String("stage", "link_loss"), zap.String("outcome", "offline"),
			zap.String("device_id", dev.DeviceID),
			zap.String("transport", transport),
			zap.String("remote_addr", remoteAddr))
	}
}

func (w *LinkWatcher) warn(message, transport, remoteAddr string, err error) {
	if w == nil || w.logger == nil {
		return
	}
	fields := []zap.Field{
		zap.String("event", "gb28181.device.link_loss_skipped"),
		zap.String("stage", "link_loss"), zap.String("outcome", "skipped"),
		zap.String("transport", transport),
		zap.String("remote_addr", remoteAddr),
	}
	if err != nil {
		fields = append(fields, zap.Error(err))
	}
	switch message {
	case linkLossAddrUnparsable:
		w.logger.Warn(linkLossAddrUnparsable, fields...)
	case linkLossPortInvalid:
		w.logger.Warn(linkLossPortInvalid, fields...)
	case linkLossDeviceQueryFailed:
		w.logger.Warn(linkLossDeviceQueryFailed, fields...)
	case linkLossMarkOfflineFailed:
		w.logger.Warn(linkLossMarkOfflineFailed, fields...)
	default:
		w.logger.Warn(linkLossUnclassified, fields...)
	}
}
