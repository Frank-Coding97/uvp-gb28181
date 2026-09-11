//go:build !windows

package routes

import "testing"

func TestRejectStaticPathReparsePointsIsNoopOutsideWindows(t *testing.T) {
	if err := rejectStaticPathReparsePoints("/tmp/root/file"); err != nil {
		t.Fatalf("rejectStaticPathReparsePoints returned %v outside Windows", err)
	}
}
