package play

import (
	"context"
	"fmt"

	"uvplatform.com/uvp-gb28181/app/gb28181/zlm"
)

type singlePortClient interface {
	OpenSinglePortReceiver(context.Context, string, int) (zlm.SinglePortReceiver, error)
	CloseSinglePortReceiver(context.Context, string) error
}

// Cleanup uses the immutable media path, not the node's current receive mode.
// This also preserves single-port cleanup across process restarts.
type receiveModeClient struct {
	ZLM
	shared singlePortClient
}

func (c receiveModeClient) CloseRtpServer(ctx context.Context, streamID string) error {
	if zlm.IsSinglePortStreamID(streamID) {
		return c.shared.CloseSinglePortReceiver(ctx, streamID)
	}
	return c.ZLM.CloseRtpServer(ctx, streamID)
}

func wrapReceiveModeClient(client ZLM) ZLM {
	if shared, ok := client.(singlePortClient); ok {
		return receiveModeClient{ZLM: client, shared: shared}
	}
	return client
}

func (c receiveModeClient) OpenSinglePortReceiver(ctx context.Context, ssrc string, port int) (zlm.SinglePortReceiver, error) {
	return c.shared.OpenSinglePortReceiver(ctx, ssrc, port)
}

// GetMediaList 透传底层客户端的媒体列表查询（读取真实 readerCount）。
//
// ⛔ 这层包装内嵌的是 ZLM 接口，而该接口只有收流相关的三个方法，包装之后
// 底层 *zlm.Client 上的 GetMediaList 就"凭空消失"了。StopOnNoneReader 用
// client.(mediaListClient) 断言去查 readerCount，断言失败即返回
// ErrReaderCountUnavailable，于是无人观看时保守选择不关流 ⇒ 实时流永久泄漏
// （现场实测：ZLM 里 readerCount=0、aliveSecond 一路涨到 2.9 小时，而
// on_stream_none_reader 钩子每 20 秒就来一次、次次报 fixed_stop_failed）。
//
// 必须在包装层显式透传，否则 ZLM 的 on_stream_none_reader 形同虚设。
// 底层不支持时仍返回 ErrReaderCountUnavailable，让调用方保持"查不到就不关"
// 的保守语义，不能伪装成 0 个观众。
func (c receiveModeClient) GetMediaList(ctx context.Context, vhost, app, stream string) ([]zlm.MediaInfo, error) {
	lister, ok := c.ZLM.(mediaListClient)
	if !ok {
		return nil, ErrReaderCountUnavailable
	}
	return lister.GetMediaList(ctx, vhost, app, stream)
}

func sharedReceiver(client ZLM) (singlePortClient, error) {
	if receiver, ok := client.(singlePortClient); ok {
		return receiver, nil
	}
	return nil, fmt.Errorf("当前媒体客户端不支持单端口收流")
}

func (c receiveModeClient) CloseSinglePortReceiver(ctx context.Context, streamID string) error {
	return c.shared.CloseSinglePortReceiver(ctx, streamID)
}
