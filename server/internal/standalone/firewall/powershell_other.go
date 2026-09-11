//go:build !windows

package firewall

import "context"

type unsupportedAdapter struct{}

func NewSystemAdapter() Adapter {
	return unsupportedAdapter{}
}

func (unsupportedAdapter) Interfaces(context.Context) ([]InterfaceInfo, error) {
	return nil, errorFor(ReasonUnsupportedPlatform)
}

func (unsupportedAdapter) Inspect(context.Context, []Rule) ([]RuleState, error) {
	return nil, errorFor(ReasonUnsupportedPlatform)
}

func (unsupportedAdapter) Upsert(context.Context, []Rule) error {
	return errorFor(ReasonUnsupportedPlatform)
}

func (unsupportedAdapter) Remove(context.Context, []string) error {
	return errorFor(ReasonUnsupportedPlatform)
}
