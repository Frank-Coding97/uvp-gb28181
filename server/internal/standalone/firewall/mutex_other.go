//go:build !windows

package firewall

import (
	"context"
	"sync"
)

var processInstanceLocks sync.Map

func lockFirewallInstance(ctx context.Context, root string) (func(), error) {
	hash, ok := instanceHash(root)
	if !ok {
		return nil, errorFor(ReasonInvalidInput)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	newLock := make(chan struct{}, 1)
	newLock <- struct{}{}
	value, _ := processInstanceLocks.LoadOrStore(hash, newLock)
	lock := value.(chan struct{})
	select {
	case <-lock:
		return func() { lock <- struct{}{} }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
