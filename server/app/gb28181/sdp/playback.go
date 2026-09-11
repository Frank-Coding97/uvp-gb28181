package sdp

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrInvalidPlaybackArgument = errors.New("sdp: invalid playback argument")

// PlaybackArgumentError identifies the input field that cannot be represented
// by a GB28181 Playback offer.
type PlaybackArgumentError struct {
	Field string
	Err   error
}

func (e *PlaybackArgumentError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s %s: %v", ErrInvalidPlaybackArgument, e.Field, e.Err)
}

func (e *PlaybackArgumentError) Unwrap() error { return ErrInvalidPlaybackArgument }

type PlaybackParams struct {
	ServerID  string
	ChannelID string
	RecvIP    string
	RecvPort  int
	SSRC      string
	TCPMode   bool
	Start     time.Time
	End       time.Time
	PlayFrom  time.Time
	Extended  bool
}

type DownloadParams struct {
	ServerID      string
	ChannelID     string
	RecvIP        string
	RecvPort      int
	SSRC          string
	TCPMode       bool
	Start         time.Time
	End           time.Time
	DownloadSpeed uint32
	Extended      bool
}

// BuildPlaybackSDP builds a GB28181 historical playback offer. PlayFrom is
// optional and defaults to Start; when present it must remain inside [Start, End).
func BuildPlaybackSDP(params PlaybackParams) (string, error) {
	playFrom, err := validatePlaybackParams(params)
	if err != nil {
		return "", err
	}
	return buildHistoricalSDP(params, "Playback", playFrom, params.End, 0)
}

// BuildDownloadSDP builds a GB/T 28181 device recording download offer.
func BuildDownloadSDP(params DownloadParams) (string, error) {
	speed := params.DownloadSpeed
	if speed == 0 {
		speed = 4
	}
	if speed != 1 && speed != 2 && speed != 4 && speed != 8 {
		return "", invalidPlaybackArgument("DownloadSpeed", "must be one of 1, 2, 4, 8")
	}
	playback := PlaybackParams{
		ServerID: params.ServerID, ChannelID: params.ChannelID, RecvIP: params.RecvIP,
		RecvPort: params.RecvPort, SSRC: params.SSRC, TCPMode: params.TCPMode,
		Start: params.Start, End: params.End, PlayFrom: params.Start, Extended: params.Extended,
	}
	start, err := validatePlaybackParams(playback)
	if err != nil {
		return "", err
	}
	return buildHistoricalSDP(playback, "Download", start, params.End, speed)
}

func buildHistoricalSDP(params PlaybackParams, sessionName string, start, end time.Time, downloadSpeed uint32) (string, error) {

	transport := "RTP/AVP"
	if params.TCPMode {
		transport = "TCP/RTP/AVP"
	}
	var body strings.Builder
	body.WriteString("v=0\r\n")
	body.WriteString(fmt.Sprintf("o=%s 0 0 IN IP4 %s\r\n", params.ServerID, params.RecvIP))
	body.WriteString(fmt.Sprintf("s=%s\r\n", sessionName))
	body.WriteString(fmt.Sprintf("u=%s:0\r\n", params.ChannelID))
	body.WriteString(fmt.Sprintf("c=IN IP4 %s\r\n", params.RecvIP))
	body.WriteString(fmt.Sprintf("t=%d %d\r\n", start.Unix(), end.Unix()))
	writeVideoMediaDescription(&body, params.RecvPort, transport, params.Extended)
	if downloadSpeed > 0 {
		body.WriteString(fmt.Sprintf("a=downloadspeed:%d\r\n", downloadSpeed))
	}
	if params.TCPMode {
		body.WriteString("a=setup:passive\r\n")
		body.WriteString("a=connection:new\r\n")
	}
	body.WriteString(fmt.Sprintf("y=%s\r\n", params.SSRC))
	return body.String(), nil
}

func validatePlaybackParams(params PlaybackParams) (time.Time, error) {
	if strings.TrimSpace(params.ServerID) == "" {
		return time.Time{}, invalidPlaybackArgument("ServerID", "must not be empty")
	}
	if strings.TrimSpace(params.ChannelID) == "" {
		return time.Time{}, invalidPlaybackArgument("ChannelID", "must not be empty")
	}
	if strings.TrimSpace(params.RecvIP) == "" {
		return time.Time{}, invalidPlaybackArgument("RecvIP", "must not be empty")
	}
	if params.RecvPort < 1 || params.RecvPort > 65535 {
		return time.Time{}, invalidPlaybackArgument("RecvPort", "must be between 1 and 65535")
	}
	if !validPlaybackSSRC(params.SSRC) {
		return time.Time{}, invalidPlaybackArgument("SSRC", "must be 10 digits and start with 1")
	}
	if params.Start.IsZero() {
		return time.Time{}, invalidPlaybackArgument("Start", "must not be zero")
	}
	if params.End.IsZero() || params.End.Unix() <= params.Start.Unix() {
		return time.Time{}, invalidPlaybackArgument("End", "must be later than Start at wire precision")
	}

	playFrom := params.PlayFrom
	if playFrom.IsZero() {
		playFrom = params.Start
	}
	if playFrom.Before(params.Start) || !playFrom.Before(params.End) || playFrom.Unix() >= params.End.Unix() {
		return time.Time{}, invalidPlaybackArgument("PlayFrom", "must be inside [Start, End)")
	}
	return playFrom, nil
}

func validPlaybackSSRC(value string) bool {
	if len(value) != 10 || value[0] != '1' {
		return false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func invalidPlaybackArgument(field, reason string) error {
	return &PlaybackArgumentError{Field: field, Err: errors.New(reason)}
}
