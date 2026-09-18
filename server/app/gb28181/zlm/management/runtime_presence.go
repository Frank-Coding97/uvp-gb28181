package management

import (
	"context"
	"errors"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
)

// NewRuntimePresenceReader adapts the fresh, node-guarded media detail path
// to ownership resolution. It deliberately bypasses RuntimeReader's cache so
// preflight and execution cannot reuse the same presence observation.
func NewRuntimePresenceReader(reader *RuntimeReader) MediaPresenceReader {
	return runtimePresenceReader{reader: reader}
}

type runtimePresenceReader struct{ reader *RuntimeReader }

func (r runtimePresenceReader) IsPresent(ctx context.Context, target OwnershipTarget) (bool, error) {
	if r.reader == nil {
		return false, NewServiceUnavailableError(nodeIDString(target.NodeID), "runtime presence reader is not configured")
	}
	if err := target.Validate(); err != nil {
		return false, err
	}
	_, err := (runtimeFreshMediaReader{reader: r.reader}).GetMediaInfoFresh(ctx, target.NodeID, zlm.StreamTarget{
		Schema: target.Media.Schema,
		VHost:  target.Media.Vhost,
		App:    target.Media.App,
		Stream: target.Media.Stream,
	})
	if errors.Is(err, ErrMediaNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

var _ MediaPresenceReader = runtimePresenceReader{}
