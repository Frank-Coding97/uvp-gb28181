package play

// AuthorizedRequest carries the device epoch observed by the caller's
// authoritative permission query. Playback must never replace that epoch with
// a newer value after media I/O, which would reauthorize an old request.
type AuthorizedRequest struct {
	DeviceID    string
	ChannelID   string
	ClientIP    string
	DeviceEpoch int64
}
