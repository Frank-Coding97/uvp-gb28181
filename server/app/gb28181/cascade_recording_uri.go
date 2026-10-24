package gb28181

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/gb28181/recordquery"
	"uvplatform.com/uvp-gb28181/app/gb28181/sdp"
)

type cascadeRecordingQuerier interface {
	QueryForCascade(context.Context, recordquery.CascadeQueryRequest) (recordquery.QueryResult, error)
}

func (h *cascadeVideoRuntime) resolveRecordingURI(ctx context.Context, source cascadeRecordingTarget, channelID uint64, offer sdp.CascadeVideoOffer) (string, error) {
	uri, err := sdp.ParseHistoricalURI(offer.RecordingURI)
	if err != nil {
		return "", err
	}
	if uri.FilePath == "" {
		uri.DeviceID = source.channel.ChannelID
		return uri.String(), nil
	}
	if h.recordings == nil || h.recordingLocation == nil {
		return "", fmt.Errorf("recording file authorization unavailable")
	}
	// A storage URI alone cannot prove which camera owns a file. Resolve it
	// against records queried for this authorized channel, never by storage ID.
	result, err := h.recordings.QueryForCascade(ctx, recordquery.CascadeQueryRequest{
		ChannelID: uint(channelID), DeviceCode: source.device.DeviceID, ChannelCode: source.channel.ChannelID,
		Destination: net.JoinHostPort(source.device.IP, strconv.Itoa(source.device.Port)), Transport: source.device.Transport,
		ProtocolVersion: protocol.Version(source.device.EffectiveVersion), StartTime: offer.Start, EndTime: offer.End,
		Type: manscdp.RecordInfoQueryTypeAll,
	})
	if err != nil {
		return "", err
	}
	if result.Incomplete {
		return "", fmt.Errorf("recording file query is incomplete")
	}
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
		if parseErr != nil || selected.FilePath != uri.FilePath {
			continue
		}
		start, startErr := recordquery.ParseWallClock(record.StartTime, h.recordingLocation)
		end, endErr := recordquery.ParseWallClock(record.EndTime, h.recordingLocation)
		if startErr != nil || endErr != nil || offer.Start.Before(start) || offer.End.After(end) {
			continue
		}
		return selected.String(), nil
	}
	return "", fmt.Errorf("file is not within the authorized channel and recording range")
}
