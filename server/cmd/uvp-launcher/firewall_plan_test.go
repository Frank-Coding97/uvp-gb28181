package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"uvplatform.cn/uvp-gb28181/internal/standalone/firewall"
)

func TestFirewallPlanDriftRejectsBeforeMutation(t *testing.T) {
	for _, change := range []string{"sip_port", "media_port", "release", "alias", "profile"} {
		t.Run(change, func(t *testing.T) {
			inputs := launcherInputs(t)
			adapter := newLauncherAdapter()
			hash := testFirewallPlanHash(t, inputs, adapter)
			switch change {
			case "sip_port":
				inputs.SIP.Port++
			case "media_port":
				inputs.MediaListeners[0].Address = "0.0.0.0:18081"
			case "release":
				inputs.ReleaseID = "changed"
			case "alias":
				adapter.iface.Alias = "renamed"
			case "profile":
				adapter.iface.Private = false
			}
			options := firewallCommandOptions{
				out: &bytes.Buffer{}, errOut: &bytes.Buffer{},
				requireElevated: func() error { return nil },
				load:            func(context.Context) (firewall.Inputs, error) { return inputs, nil },
				adapter:         func() firewall.Adapter { return adapter },
			}
			if code := runFirewallInternal([]string{"apply", "--interface-id", testGUIDForLauncher, "--plan-sha256", hash}, options); code == 0 {
				t.Fatal("changed plan reported success")
			}
			if adapter.upserts != 0 || adapter.removes != 0 {
				t.Fatal("changed plan mutated firewall")
			}
		})
	}
}

func TestFirewallInternalElevationCheckedBeforeIO(t *testing.T) {
	calls := 0
	options := firewallCommandOptions{
		out:             &bytes.Buffer{},
		requireElevated: func() error { return firewall.NewError(firewall.ReasonPermissionDenied) },
		load:            func(context.Context) (firewall.Inputs, error) { calls++; return firewall.Inputs{}, nil },
		adapter:         func() firewall.Adapter { calls++; return newLauncherAdapter() },
	}
	code := runFirewallInternal([]string{"apply", "--interface-id", testGUIDForLauncher, "--plan-sha256", strings.Repeat("a", 64)}, options)
	if code != firewallExitCode(firewall.ReasonPermissionDenied) || calls != 0 {
		t.Fatalf("code=%d IO=%d", code, calls)
	}
}

func TestFirewallPlanHashAndExitCodesAreBounded(t *testing.T) {
	for _, value := range []string{"", strings.Repeat("g", 64), strings.Repeat("A", 64), strings.Repeat("a", 63), "a;cmd"} {
		if validFirewallPlanHash(value) {
			t.Fatal("invalid plan hash accepted")
		}
	}
	for _, reason := range firewallExitReasons {
		if got := firewallReasonFromExitCode(uint32(firewallExitCode(reason))); got != reason {
			t.Fatalf("reason roundtrip failed: %s", reason)
		}
	}
	if firewallReasonFromExitCode(999) != firewall.ReasonElevationFailed {
		t.Fatal("unbounded child exit accepted")
	}
}

func TestFirewallReadOnlyDatabaseRejectsDirectory(t *testing.T) {
	if db, closeDB, err := openFirewallReadOnlyDatabase(t.TempDir()); err == nil || db != nil {
		closeDB()
		t.Fatal("directory was accepted as database")
	}
}
