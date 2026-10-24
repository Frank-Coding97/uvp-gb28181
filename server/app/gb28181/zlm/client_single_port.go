package zlm

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

// SinglePortStreamID matches ZLM RtpSession::printSSRC, including leading zeros.
func SinglePortStreamID(ssrc string) (string, error) {
	value, err := strconv.ParseUint(ssrc, 10, 32)
	if err != nil || value == 0 {
		return "", fmt.Errorf("单端口 SSRC 非法")
	}
	return fmt.Sprintf("%08X", value), nil
}

// IsSinglePortStreamID identifies the persisted single-port media path. Live
// multi-port IDs are ten decimal digits or fixed device/channel paths.
func IsSinglePortStreamID(streamID string) bool {
	if len(streamID) != 8 {
		return false
	}
	value, err := strconv.ParseUint(streamID, 16, 32)
	return err == nil && value != 0 && fmt.Sprintf("%08X", value) == streamID
}

type SinglePortReceiver struct {
	StreamID string
	Port     int
}

// OpenSinglePortReceiver checks the configured shared listener without opening
// a per-stream socket. getServerConfig alone cannot prove OS socket readiness.
func (c *Client) OpenSinglePortReceiver(ctx context.Context, ssrc string, port int) (SinglePortReceiver, error) {
	streamID, err := SinglePortStreamID(ssrc)
	if err != nil {
		return SinglePortReceiver{}, err
	}
	if port < 1024 || port > 65534 {
		return SinglePortReceiver{}, fmt.Errorf("单端口端口需在 1024-65534 之间")
	}
	cfg, err := c.GetServerConfig(ctx)
	if err != nil {
		return SinglePortReceiver{}, err
	}
	actual, err := strconv.Atoi(cfg["rtp_proxy.port"])
	if err != nil || actual != port {
		return SinglePortReceiver{}, fmt.Errorf("单端口监听配置不匹配，请检查 rtp_proxy.port 并在修改后重启媒体节点")
	}
	return SinglePortReceiver{StreamID: streamID, Port: port}, nil
}

// CloseSinglePortReceiver closes only this RTP process and waits for its
// disappearance before SSRC reuse. The shared listener is never closed.
func (c *Client) CloseSinglePortReceiver(ctx context.Context, streamID string) error {
	if !IsSinglePortStreamID(streamID) {
		return fmt.Errorf("单端口流标识非法")
	}
	_, err := c.CloseStreams(ctx, map[string]string{"vhost": "__defaultVhost__", "app": "rtp", "stream": streamID, "force": "1"})
	if err != nil {
		return err
	}
	timeout := time.NewTimer(3 * time.Second)
	defer timeout.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		var result struct {
			baseResp
			Exist *bool `json:"exist"`
		}
		if err := c.call(ctx, "getRtpInfo", map[string]string{"vhost": "__defaultVhost__", "app": "rtp", "stream_id": streamID}, &result); err != nil {
			return err
		}
		if result.Code != 0 || result.Exist == nil {
			return fmt.Errorf("单端口流清理回读失败: code=%d", result.Code)
		}
		if !*result.Exist {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout.C:
			return fmt.Errorf("单端口流清理尚未完成")
		case <-ticker.C:
		}
	}
}
