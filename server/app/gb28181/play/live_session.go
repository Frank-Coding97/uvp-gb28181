package play

import "uvplatform.cn/uvp-gb28181/app/gb28181/stream"

type LiveMode string

const (
	LiveModeDynamic LiveMode = "dynamic"
	LiveModeFixed   LiveMode = "fixed"
)

type LiveState string

const (
	LiveStateStarting LiveState = "starting"
	LiveStateReady    LiveState = "ready"
	LiveStateStopping LiveState = "stopping"
)

type LiveSession struct {
	DeviceID    string
	ChannelID   string
	StreamID    string
	SSRC        string
	Generation  uint64
	NodeID      int64
	ModeAtStart LiveMode
	State       LiveState
}

func (s LiveSession) Ref() stream.LiveRef {
	return stream.LiveRef{StreamID: s.StreamID, SSRC: s.SSRC, Generation: s.Generation, NodeID: s.NodeID}
}
