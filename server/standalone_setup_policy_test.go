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
