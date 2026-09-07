package main

import "testing"

func TestManagementBrowserURLRejectsExternalOrShellTargets(t *testing.T) {
	for _, tc := range []struct {
		url   string
		valid bool
	}{{"http://127.0.0.1:8280/", true}, {"http://[::1]:8280/", true}, {"file:///C:/Windows/cmd.exe", false}, {"http://example.com/", false}, {"http://user:pass@127.0.0.1/", false}, {"http://127.0.0.1/?secret=value", false}} {
		if validManagementURL(tc.url) != tc.valid {
			t.Fatalf("unexpected URL acceptance: %s", tc.url)
		}
	}
}
