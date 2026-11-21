package auth

import "context"

const (
	PlayLiveReadScope = "play:live:read"
	PlayLiveListScope = "play:live:list"
)

type PlayListRequest struct {
	PlayRequest
	Page     int
	PageSize int
}

// PlayReader exposes snapshots of the current client's live leases. Reads
// never start media, create credentials, or extend an authorization lease.
type PlayReader interface {
	Read(context.Context, PlayRequest) (any, error)
	List(context.Context, PlayListRequest) (any, error)
}
