package zlm

import (
	"context"
	"fmt"
)

const CapabilityGetMediaTrafficStatistic = "getMediaTrafficStatistic"

// MediaTrafficStatistic is the socket-level media traffic snapshot returned
// by ZLMediaKit-UVP. Rates are application socket bytes per second.
type MediaTrafficStatistic struct {
	UpstreamBytesPerSecond   uint64 `json:"upstreamBytesPerSecond"`
	DownstreamBytesPerSecond uint64 `json:"downstreamBytesPerSecond"`
	OriginSocketCount        int    `json:"originSocketCount"`
	PlayerSocketCount        int    `json:"playerSocketCount"`
	Unit                     string `json:"unit"`
}

func (c *Client) GetMediaTrafficStatistic(ctx context.Context) (MediaTrafficStatistic, error) {
	var response struct {
		baseResp
		Data *MediaTrafficStatistic `json:"data"`
	}
	if err := c.call(ctx, CapabilityGetMediaTrafficStatistic, nil, &response); err != nil {
		return MediaTrafficStatistic{}, c.runtimeNodeError(CapabilityGetMediaTrafficStatistic, err)
	}
	if response.Code != 0 {
		return MediaTrafficStatistic{}, c.runtimeResponseError(CapabilityGetMediaTrafficStatistic, response.Code, response.Msg)
	}
	if response.Data == nil {
		return MediaTrafficStatistic{}, c.runtimeNodeError(CapabilityGetMediaTrafficStatistic, fmt.Errorf("响应缺少 data"))
	}
	return *response.Data, nil
}
