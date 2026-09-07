//go:build windows

package firewall

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestPowerShellFirewallScriptUsesBoundedUTF8AndInterfaceFilter(t *testing.T) {
	for _, fragment := range []string{
		"[Console]::InputEncoding = $utf8",
		"[Console]::OutputEncoding = $utf8",
		"Get-NetFirewallInterfaceFilter -AssociatedNetFirewallRule",
		"LocalPort = @([string]$item.local_port -split ',')",
		"Group = [string]$item.group",
	} {
		if !strings.Contains(powerShellFirewallScript, fragment) {
			t.Fatalf("PowerShell script missing required fragment")
		}
	}
}

// This opt-in check only enumerates adapters. Set the expected alias from the
// native host environment when validating that PowerShell's UTF-8 boundary
// preserves a localized adapter name; it never logs the alias or changes rules.
func TestSystemAdapterInterfacesPreserveUTF8AliasOptIn(t *testing.T) {
	if os.Getenv("UVP_FIREWALL_NATIVE_INTERFACES") != "1" {
		t.Skip("set UVP_FIREWALL_NATIVE_INTERFACES=1 for native adapter validation")
	}
	want := os.Getenv("UVP_FIREWALL_EXPECT_ALIAS")
	if strings.TrimSpace(want) == "" || !utf8.ValidString(want) {
		t.Fatal("UVP_FIREWALL_EXPECT_ALIAS must be a non-empty UTF-8 adapter alias")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	interfaces, err := NewSystemAdapter().Interfaces(ctx)
	if err != nil {
		t.Fatalf("enumerating native interfaces: %v", err)
	}
	for _, candidate := range interfaces {
		if !utf8.ValidString(candidate.Alias) {
			t.Fatal("native adapter alias was not valid UTF-8")
		}
		if candidate.Alias == want {
			return
		}
	}
	t.Fatal("expected native adapter alias was not returned")
}
