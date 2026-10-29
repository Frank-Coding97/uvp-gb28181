package device

import (
	"context"
	"sync"
)

type StatusObserver interface {
	DeviceStatusChanged(context.Context, string, bool, string)
}

var statusObserver struct {
	sync.RWMutex
	value StatusObserver
}

func SetStatusObserver(observer StatusObserver) {
	statusObserver.Lock()
	statusObserver.value = observer
	statusObserver.Unlock()
}

func notifyStatusObserver(ctx context.Context, deviceID string, online bool, reason string) {
	statusObserver.RLock()
	observer := statusObserver.value
	statusObserver.RUnlock()
	if observer != nil {
		observer.DeviceStatusChanged(ctx, deviceID, online, reason)
	}
}
