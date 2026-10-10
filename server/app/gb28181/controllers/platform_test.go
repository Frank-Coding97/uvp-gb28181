package controllers

import "testing"

func TestMaskPassword(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "short", in: "12", want: "**"},
		{name: "normal", in: "12345678", want: "1******8"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := maskPassword(tc.in); got != tc.want {
				t.Fatalf("maskPassword(%q)=%q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRegisterURI(t *testing.T) {
	got := registerURI("34020000002000000001", "3402000000", "192.168.10.106", 5061)
	want := "sip:34020000002000000001@192.168.10.106:5061"
	if got != want {
		t.Fatalf("registerURI()=%q, want %q", got, want)
	}
	if got := registerURI("", "3402000000", "192.168.10.106", 5061); got != "" {
		t.Fatalf("registerURI() with empty serverID=%q, want empty", got)
	}
}
