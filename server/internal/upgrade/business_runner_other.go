//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package upgrade

import "context"

type BusinessCommandRunner struct{}

func NewBusinessCommandRunner(uint32, uint32) BusinessCommandRunner { return BusinessCommandRunner{} }
func (BusinessCommandRunner) Run(ctx context.Context, name string, args ...string) error {
	return (OSCommandRunner{}).Run(ctx, name, args...)
}
func (BusinessCommandRunner) RunDir(ctx context.Context, dir, name string, args ...string) error {
	return (OSCommandRunner{}).RunDir(ctx, dir, name, args...)
}
func (BusinessCommandRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return (OSCommandRunner{}).Output(ctx, name, args...)
}
