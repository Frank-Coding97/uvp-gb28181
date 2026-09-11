package main

import "testing"

func TestStandaloneAdminPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, user, password string
		min                  int
		special, valid       bool
	}{
		{"valid", "admin", "strong!pass", 6, true, true},
		{"short", "admin", "a!", 6, true, false},
		{"missing special", "admin", "strongpass", 6, true, false},
		{"configured length", "admin", "strong!pass", 20, true, false},
		{"optional special", "admin", "strongpass", 6, false, true},
		{"empty user", " ", "strong!pass", 6, true, false},
		{"invalid utf8", string([]byte{255}), "strong!pass", 6, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := validStandaloneAdmin(tc.user, tc.password, tc.min, tc.special); got != tc.valid {
				t.Fatalf("validation=%v want %v", got, tc.valid)
			}
		})
	}
}

func TestStandaloneSetupOriginMatchesLauncher(t *testing.T) {
	for _, tc := range []struct{ address, origin string }{
		{"127.0.0.1:8280", "http://127.0.0.1:8280"},
		{":8280", "http://127.0.0.1:8280"},
		{"0.0.0.0:8280", "http://127.0.0.1:8280"},
		{"[::]:8280", "http://[::1]:8280"},
		{"192.168.10.52:8280", ""},
		{"127.0.0.1:0", ""},
	} {
		t.Run(tc.address, func(t *testing.T) {
			origin, err := standaloneSetupOrigin(tc.address)
			if origin != tc.origin || (err != nil) != (tc.origin == "") {
				t.Fatalf("origin=%q err=%v", origin, err)
			}
		})
	}
}
