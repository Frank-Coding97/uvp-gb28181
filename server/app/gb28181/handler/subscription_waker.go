package handler

import "context"

// SubscriptionWaker marks enabled subscriptions due when a device recovers online.
type SubscriptionWaker interface {
	WakeDeviceByCode(context.Context, string) error
}
