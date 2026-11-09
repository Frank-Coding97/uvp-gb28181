package sip

import (
	"net/http"
	"time"

	siplib "github.com/emiago/sipgo/sip"
)

// deviceTimeSyncTx 在 SIP 成功应答上补一个 Date 头，供设备校时使用。
//
// 国标没有独立的「校时」命令：设备的校时依据就是注册 / 心跳应答里的 Date 头，
// 设备拿它与本地时钟比对，超过自身阈值后**修改自己的系统时间**。也就是说这个
// 头一旦下发，改的就是现场设备的时钟，且会影响它的录像时间轴与报警时间。
// 因此它被 gb28181.sip.device_time_sync.enabled 控制，默认关闭；关闭时根本不会
// 构造这个包装（见 newDeviceTimeSyncTx），报文与历史版本逐字节一致。
type deviceTimeSyncTx struct {
	siplib.ServerTransaction
}

// newDeviceTimeSyncTx 只在开关打开时包装事务；关闭时原样返回，保证默认路径
// 零行为变化（包括依赖具体事务类型断言的调用方）。
func newDeviceTimeSyncTx(tx siplib.ServerTransaction, enabled bool) siplib.ServerTransaction {
	if !enabled || tx == nil {
		return tx
	}
	return deviceTimeSyncTx{ServerTransaction: tx}
}

func (t deviceTimeSyncTx) Respond(res *siplib.Response) error {
	attachDeviceTimeHeader(res)
	return t.ServerTransaction.Respond(res)
}

// Origin 透传底层事务的原始请求。级联 INVITE / 广播取消校验用
// `tx.(interface{ Origin() *sip.Request })` 判定对端，这里必须保持等价语义：
// 底层不支持就返回 nil，与「断言失败」的分支一致。
func (t deviceTimeSyncTx) Origin() *siplib.Request {
	if provider, ok := t.ServerTransaction.(interface{ Origin() *siplib.Request }); ok {
		return provider.Origin()
	}
	return nil
}

// RequestLease 同上，供请求生命周期统计使用。
func (t deviceTimeSyncTx) RequestLease() siplib.RequestLease {
	if provider, ok := t.ServerTransaction.(interface{ RequestLease() siplib.RequestLease }); ok {
		return provider.RequestLease()
	}
	return nil
}

// attachDeviceTimeHeader 给 2xx 应答补 Date。只补成功应答：
//   - 4xx/5xx 没有校时语义，401 的 WWW-Authenticate 另有自己的 nonce 时间；
//   - 调用方已显式写了 Date 就不覆盖，避免出现两个时间。
//
// Date 插在 Content-Length 之前，保证消息体长度仍留在最后一个头。
func attachDeviceTimeHeader(res *siplib.Response) {
	if res == nil || !res.IsSuccess() {
		return
	}
	if res.GetHeader("Date") != nil {
		return
	}
	res.AppendHeaderAfter(siplib.NewHeader("Date", time.Now().UTC().Format(http.TimeFormat)), "Content-Length")
}
