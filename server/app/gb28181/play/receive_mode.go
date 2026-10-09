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

func sharedReceiver(client ZLM) (singlePortClient, error) {
	if receiver, ok := client.(singlePortClient); ok {
		return receiver, nil
	}
	return nil, fmt.Errorf("当前媒体客户端不支持单端口收流")
}

func (c receiveModeClient) CloseSinglePortReceiver(ctx context.Context, streamID string) error {
	return c.shared.CloseSinglePortReceiver(ctx, streamID)
}
