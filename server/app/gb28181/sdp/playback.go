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

// BuildPlaybackSDP builds a GB28181 historical playback offer. PlayFrom is
// optional and defaults to Start; when present it must remain inside [Start, End).
func BuildPlaybackSDP(params PlaybackParams) (string, error) {
	playFrom, err := validatePlaybackParams(params)
	if err != nil {
		return "", err
	}

	transport := "RTP/AVP"
	if params.TCPMode {
		transport = "TCP/RTP/AVP"
	}
	var body strings.Builder
	body.WriteString("v=0\r\n")
	body.WriteString(fmt.Sprintf("o=%s 0 0 IN IP4 %s\r\n", params.ServerID, params.RecvIP))
	body.WriteString("s=Playback\r\n")
	body.WriteString(fmt.Sprintf("u=%s:0\r\n", params.ChannelID))
	body.WriteString(fmt.Sprintf("c=IN IP4 %s\r\n", params.RecvIP))
	body.WriteString(fmt.Sprintf("t=%d %d\r\n", playFrom.Unix(), params.End.Unix()))
	writeVideoMediaDescription(&body, params.RecvPort, transport, params.Extended)
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
