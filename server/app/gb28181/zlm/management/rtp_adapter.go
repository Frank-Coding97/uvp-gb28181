package management

import (
	"context"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
)

// NodeRTPClientAdapter is the production RTP seam for RTPService. Every call
// is resolved through NodeExecutor, so a request cannot choose an endpoint or
// inject arbitrary ZLM parameters.
type NodeRTPClientAdapter struct {
	executor *NodeExecutor
}

func NewNodeRTPClientAdapter(executor *NodeExecutor) *NodeRTPClientAdapter {
	return &NodeRTPClientAdapter{executor: executor}
}

func (a *NodeRTPClientAdapter) ListRtpServers(ctx context.Context, nodeID int64) ([]zlm.RtpServerInfo, error) {
	if err := a.requireConfigured(nodeID); err != nil {
		return nil, err
	}
	var result []zlm.RtpServerInfo
	if err := a.executor.ExecuteRead(ctx, nodeID, func(operationCtx context.Context, client *zlm.Client) error {
		var err error
		result, err = client.ListRtpServers(operationCtx)
		return err
	}); err != nil {
		return nil, err
	}
	return result, nil
}

func (a *NodeRTPClientAdapter) OpenRtpServer(ctx context.Context, nodeID int64, request RTPServerCreateRequest) (*RTPServerOpenResult, error) {
	if err := a.requireConfigured(nodeID); err != nil {
		return nil, err
	}
	var opened *zlm.OpenRtpServerResult
	if err := a.executor.ExecuteWrite(ctx, nodeID, func(operationCtx context.Context, client *zlm.Client) error {
		var err error
		opened, err = client.OpenRtpServerWithSSRC(operationCtx, zlm.OpenRtpServerRequest{
			VHost: request.VHost, App: request.App, StreamID: request.Stream,
			SSRC: request.SSRC, Port: request.Port, TCPMode: request.TCPMode,
			OnlyTrack: request.OnlyTrack, LocalIP: request.LocalIP, Reuse: request.Reuse,
		})
		return err
	}); err != nil {
		return nil, err
	}
	if opened == nil {
		return nil, nil
	}
	return &RTPServerOpenResult{Key: request.Stream, Port: opened.Port}, nil
}

func (a *NodeRTPClientAdapter) CloseRtpServer(ctx context.Context, nodeID int64, vhost, appName, streamID string) (*RTPServerCloseResult, error) {
	if err := a.requireConfigured(nodeID); err != nil {
		return nil, err
	}
	var closed *zlm.RtpServerCloseResult
	if err := a.executor.ExecuteWrite(ctx, nodeID, func(operationCtx context.Context, client *zlm.Client) error {
		var err error
		closed, err = client.CloseRtpServerWithResult(operationCtx, vhost, appName, streamID)
		return err
	}); err != nil {
		return nil, err
	}
	if closed == nil {
		return nil, nil
	}
	return &RTPServerCloseResult{Stream: closed.StreamID, Hit: closed.Hit, Released: closed.Released}, nil
}

func (a *NodeRTPClientAdapter) requireConfigured(nodeID int64) error {
	if a == nil || a.executor == nil {
		return NewInternalError(nodeIDString(nodeID), "RTP client adapter is not configured")
	}
	return nil
}

var _ RTPClient = (*NodeRTPClientAdapter)(nil)
