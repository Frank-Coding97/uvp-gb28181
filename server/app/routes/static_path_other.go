//go:build !windows

package routes

func rejectStaticPathReparsePoints(_ string) error { return nil }
