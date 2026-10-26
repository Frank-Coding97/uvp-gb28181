package gb28181

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/gb28181/recordquery"
	"uvplatform.com/uvp-gb28181/app/gb28181/sdp"
)

type cascadeRecordingQuerier interface {
	QueryForCascade(context.Context, recordquery.CascadeQueryRequest) (recordquery.QueryResult, error)
}

// cascadeDefaultRecordingLookback 是「上级没给回放时间范围」时向设备查询录像清单
// 的默认回溯窗口。
//
// 为什么需要一个窗口而不是「查全部」：设备侧 RecordInfo 查询按时间段下发，
// 不给时间段时大多数设备会直接回空。24 小时是所有实测上级平台（WVP / LiveGBS /
// EasyGBS）回放面板默认的「今天」范围，足够覆盖「点一下就能播」的场景。
const cascadeDefaultRecordingLookback = 24 * time.Hour

// cascadeRecordingSource 是上级回放请求最终落实到的录像来源。
type cascadeRecordingSource struct {
	// URI 回给上级的 u= 行（规范化的录像标识）。
	URI string
	// Start/End 实际播放区间。上级写 `t=0 0` 时由录像清单回填。
	Start time.Time
	End   time.Time
	// RangeResolved 表示时间范围是不是从录像清单补出来的。
	RangeResolved bool
}

// resolveRecordingSource 解析上级在回放 SDP 里给出的录像来源，并在上级**没写
// 时间范围**（`t=0 0`）时用设备录像清单把真实区间补出来。
//
// 三种输入都要能走通：
//  1. 有范围 + 有文件路径（WVP / LiveGBS 的常规写法）——按文件校验并原样播放；
//  2. 有范围 + 无文件路径（`u=<通道>:<类型>`）——只规范化标识，不查设备；
//  3. 无范围（`t=0 0`，EasyGBS 实测写法）——查设备录像清单兜底：
//     有文件路径就按文件定位，没有就取回溯窗口内**最新**的一段录像。
func (h *cascadeVideoRuntime) resolveRecordingSource(ctx context.Context, source cascadeRecordingTarget, channelID uint64, offer sdp.CascadeVideoOffer, now time.Time) (cascadeRecordingSource, error) {
	resolved := cascadeRecordingSource{Start: offer.Start, End: offer.End}
	var wanted *sdp.HistoricalURI
	if offer.RecordingURI != "" {
		uri, err := sdp.ParseHistoricalURI(offer.RecordingURI)
		if err != nil {
			return resolved, err
		}
		if uri.FilePath == "" {
			// 「该通道任意录像」：标识规范化后即可，不需要问设备要清单。
			uri.DeviceID = source.channel.ChannelID
			resolved.URI = uri.String()
		} else {
			wanted = &uri
		}
	}
	if !offer.RangeUnknown && wanted == nil {
		return resolved, nil
	}
	if h.recordings == nil || h.recordingLocation == nil {
		return resolved, fmt.Errorf("recording file authorization unavailable")
	}
	queryStart, queryEnd := offer.Start, offer.End
	if offer.RangeUnknown {
		lookback := h.recordingLookback
		if lookback <= 0 {
			lookback = cascadeDefaultRecordingLookback
		}
		queryEnd = now
		queryStart = now.Add(-lookback)
	}
	// A storage URI alone cannot prove which camera owns a file. Resolve it
	// against records queried for this authorized channel, never by storage ID.
	result, err := h.recordings.QueryForCascade(ctx, recordquery.CascadeQueryRequest{
		ChannelID: uint(channelID), DeviceCode: source.device.DeviceID, ChannelCode: source.channel.ChannelID,
		Destination: net.JoinHostPort(source.device.IP, strconv.Itoa(source.device.Port)), Transport: source.device.Transport,
		ProtocolVersion: protocol.Version(source.device.EffectiveVersion), StartTime: queryStart, EndTime: queryEnd,
		Type: manscdp.RecordInfoQueryTypeAll,
	})
	if err != nil {
		return resolved, err
	}
	if result.Incomplete {
		return resolved, fmt.Errorf("recording file query is incomplete")
	}
	var latest cascadeRecordingSource
	for _, record := range result.Records {
		if record.DeviceID != source.channel.ChannelID {
			continue
		}
		file := record.FilePath
		if file == "" {
			continue
		}
		if !strings.HasPrefix(file, "http://") {
			file = (&url.URL{Scheme: "http", Host: source.device.DeviceID, Path: "/" + strings.TrimPrefix(file, "/")}).String()
		}
		selected, parseErr := sdp.ParseHistoricalURI(file)
		if parseErr != nil {
			continue
		}
		if wanted != nil && selected.FilePath != wanted.FilePath {
			continue
		}
		start, startErr := recordquery.ParseWallClock(record.StartTime, h.recordingLocation)
		end, endErr := recordquery.ParseWallClock(record.EndTime, h.recordingLocation)
		if startErr != nil || endErr != nil || !end.After(start) {
			continue
		}
		if wanted != nil {
			// 上级给了范围：要求录像完整覆盖该范围（原契约，不改）。
			if !offer.RangeUnknown && (offer.Start.Before(start) || offer.End.After(end)) {
				continue
			}
			return cascadeRecordingSource{URI: selected.String(), Start: start, End: end, RangeResolved: offer.RangeUnknown}, nil
		}
		// 上级没给范围也没给文件：取窗口内最新的那一段。
		if latest.URI == "" || end.After(latest.End) {
			latest = cascadeRecordingSource{URI: selected.String(), Start: start, End: end, RangeResolved: true}
		}
	}
	if latest.URI != "" {
		return latest, nil
	}
	if offer.RangeUnknown {
		return resolved, fmt.Errorf("no recording found in the last %s for this channel", queryEnd.Sub(queryStart))
	}
	return resolved, fmt.Errorf("file is not within the authorized channel and recording range")
}
