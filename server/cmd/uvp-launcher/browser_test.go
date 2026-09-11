package main

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestBootstrapBrowserURLAcceptsOnlyFixedLocalFragment(t *testing.T) {
	token := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	good := "http://127.0.0.1:8280/#/standalone-setup?bootstrap_token=" + token
	if validManagementURL(good) || !validBootstrapURL(good) {
		t.Fatal("bootstrap URL must use its dedicated validator")
	}
	for _, bad := range []string{
		strings.Replace(good, "127.0.0.1", "example.com", 1),
		strings.Replace(good, "/#", "/other#", 1),
		good + "&extra=1", good + "=", good + "\n",
		strings.Replace(good, "#/standalone-setup?", "?", 1),
		strings.Replace(good, "http:", "file:", 1),
	} {
		if validBootstrapURL(bad) {
			t.Fatal("invalid bootstrap URL accepted")
		}
	}
}

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
