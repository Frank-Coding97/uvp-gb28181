//go:build !windows

package standalone

func validateLocalVolume(path string) error { return nil }
