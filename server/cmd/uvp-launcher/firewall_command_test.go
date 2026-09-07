package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/internal/standalone"
	"uvplatform.cn/uvp-gb28181/internal/standalone/firewall"
)

func TestParseFirewallCommandRejectsPathAndRuleInjectionFlags(t *testing.T) {
	for _, args := range [][]string{
		{"status", "--interface-id", testGUIDForLauncher},
		{"apply", "--interface-id", testGUIDForLauncher, "--install-dir", "C:\\x"},
		{"apply", "--interface-id", testGUIDForLauncher, "--program", "x"},
		{"apply", "--interface-id", testGUIDForLauncher, "--ports", "1"},
		{"apply", "--interface-id", testGUIDForLauncher, "--profile", "Any"},
		{"apply", "--interface-id", testGUIDForLauncher, "--remote", "*"},
		{"apply", "--interface-id", testGUIDForLauncher, "--rulename", "other"},
		{"apply", "--interface-id", testGUIDForLauncher, "--yes", "--yes"},
	} {
		if _, err := parseFirewallPublic(args); err == nil {
			t.Fatalf("parseFirewallPublic(%v) accepted an unsafe or repeated option", args)
		}
	}
	if _, err := parseFirewallInternal([]string{"apply", "--interface-id", testGUIDForLauncher, "--yes"}); err == nil {
		t.Fatal("internal parser accepted public confirmation flag")
	}
	if _, err := parseFirewallInternal([]string{"apply", "--interface-id", testGUIDForLauncher, "--program"}); err == nil {
		t.Fatal("internal parser accepted a program argument")
	}
}

const testGUIDForLauncher = "12345678-1234-1234-1234-123456789abc"

func launcherInputs(t *testing.T) firewall.Inputs {
	t.Helper()
	root := t.TempDir()
	return firewall.Inputs{
		CanonicalRoot:   root,
		BackendExe:      filepath.Join(root, "backend.exe"),
		MediaExe:        filepath.Join(root, "media.exe"),
		ReleaseVerified: true,
		SIP:             firewall.SIPConfig{ListenIP: "192.168.10.52", Port: 15070},
		MediaListeners: []standalone.MediaListener{
			{Network: "tcp", Address: "0.0.0.0:18080"},
			{Network: "udp", Address: "0.0.0.0:40000"},
		},
	}
}

type launcherAdapter struct {
	mu      sync.Mutex
	iface   firewall.InterfaceInfo
	rules   map[string]firewall.Rule
	upserts int
	removes int
}

func newLauncherAdapter() *launcherAdapter {
	return &launcherAdapter{
		iface: firewall.InterfaceInfo{
			ID:      testGUIDForLauncher,
			Alias:   "Ethernet",
			Up:      true,
			Private: true,
			IPv4:    []string{"192.168.10.52"},
		},
		rules: make(map[string]firewall.Rule),
	}
}

func (a *launcherAdapter) Interfaces(context.Context) ([]firewall.InterfaceInfo, error) {
	return []firewall.InterfaceInfo{a.iface}, nil
}

func (a *launcherAdapter) Inspect(_ context.Context, expected []firewall.Rule) ([]firewall.RuleState, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	states := make([]firewall.RuleState, 0, len(expected))
	for _, want := range expected {
		got, present := a.rules[want.Name]
		matches := present && len(want.Program) == 0
		if present && len(want.Program) != 0 {
			matches = got == want
		}
		states = append(states, firewall.RuleState{Name: want.Name, Present: present, Matches: matches})
	}
	return states, nil
}

func (a *launcherAdapter) Upsert(_ context.Context, rules []firewall.Rule) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.upserts++
	for _, rule := range rules {
		a.rules[rule.Name] = rule
	}
	return nil
}

func (a *launcherAdapter) Remove(_ context.Context, names []string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.removes++
	for _, name := range names {
		delete(a.rules, name)
	}
	return nil
}

func TestFirewallPublicMutationPreviewsAndElevatesOnlyAfterYes(t *testing.T) {
	inputs := launcherInputs(t)
	adapter := newLauncherAdapter()
	var elevatedAction firewall.Action
	var elevatedID string
	var out bytes.Buffer
	options := firewallCommandOptions{
		in:       bytes.NewBufferString(""),
		out:      &out,
		errOut:   &bytes.Buffer{},
		load:     func(context.Context) (firewall.Inputs, error) { return inputs, nil },
		identity: func(context.Context) (firewall.Inputs, error) { return inputs, nil },
		adapter:  func() firewall.Adapter { return adapter },
		elevate: func(_ context.Context, action firewall.Action, id string, planHash string) error {
			elevatedAction, elevatedID = action, id
			return nil
		},
	}
	if code := runFirewallPublic([]string{"apply", "--interface-id", testGUIDForLauncher, "--yes"}, options); code != 0 {
		t.Fatalf("public apply exit code = %d, output=%s", code, out.String())
	}
	if elevatedAction != firewall.ActionApply || elevatedID != testGUIDForLauncher {
		t.Fatalf("elevation = %q %q", elevatedAction, elevatedID)
	}
	if !bytes.Contains(out.Bytes(), []byte("preview")) || !bytes.Contains(out.Bytes(), []byte("30000-35000")) {
		t.Fatalf("preview omitted bounded rule details: %s", out.String())
	}
	if adapter.upserts != 0 {
		t.Fatal("public preview mutated rules before elevation")
	}

	out.Reset()
	elevatedAction = ""
	options.in = bytes.NewBufferString("no\n")
	if code := runFirewallPublic([]string{"remove"}, options); code == 0 {
		t.Fatalf("remove without yes was reported success: %s", out.String())
	}
	if elevatedAction != "" {
		t.Fatal("confirmation rejection still elevated")
	}
	var result firewall.Result
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()[bytes.LastIndex(out.Bytes(), []byte("{")):]), &result); err != nil {
		t.Fatalf("decode rejection result: %v (%s)", err, out.String())
	}
	if result.Reason != firewall.ReasonConfirmationRequired {
		t.Fatalf("rejection reason = %q", result.Reason)
	}
}

func TestFirewallInternalMutationUsesServiceReadback(t *testing.T) {
	inputs := launcherInputs(t)
	adapter := newLauncherAdapter()
	var out bytes.Buffer
	options := firewallCommandOptions{
		requireElevated: func() error { return nil },
		out:             &out,
		errOut:          &bytes.Buffer{},
		load:            func(context.Context) (firewall.Inputs, error) { return inputs, nil },
		adapter:         func() firewall.Adapter { return adapter },
	}
	if code := runFirewallInternal([]string{"apply", "--interface-id", testGUIDForLauncher, "--plan-sha256", testFirewallPlanHash(t, inputs, adapter)}, options); code != 0 {
		t.Fatalf("internal apply exit code = %d, output=%s", code, out.String())
	}
	var result firewall.Result
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &result); err != nil {
		t.Fatalf("decode internal result: %v", err)
	}
	if !result.Success || result.Reason != "" || adapter.upserts != 1 {
		t.Fatalf("internal result = %+v, upserts=%d", result, adapter.upserts)
	}
}

func TestFirewallPublicStatusHasNoMutationFlags(t *testing.T) {
	command, err := parseFirewallPublic([]string{"status"})
	if err != nil || command.action != firewall.ActionStatus {
		t.Fatalf("status parse = %+v, %v", command, err)
	}
	if _, err := parseFirewallPublic([]string{"status", "--yes"}); err == nil {
		t.Fatal("status accepted confirmation")
	}
}

func TestOpenFirewallDatabaseIsReadOnlyAndDoesNotCreateMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "firewall.db")
	db, err := gormhelper.NewSQLiteClient(path)
	if err != nil {
		t.Fatalf("create fixture database: %v", err)
	}
	if err := db.Exec("CREATE TABLE marker (value TEXT)").Error; err != nil {
		t.Fatalf("create fixture table: %v", err)
	}
	raw, err := db.DB()
	if err != nil {
		t.Fatalf("get fixture database: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close fixture database: %v", err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat fixture database: %v", err)
	}
	readOnly, closeDB, err := openFirewallReadOnlyDatabase(path)
	if err != nil {
		t.Fatalf("open read-only database: %v", err)
	}
	defer closeDB()
	var count int
	if err := readOnly.Raw("SELECT count(*) FROM marker").Scan(&count).Error; err != nil {
		t.Fatalf("read from read-only database: %v", err)
	}
	if err := readOnly.Exec("CREATE TABLE must_fail (value TEXT)").Error; err == nil {
		t.Fatal("read-only database accepted a write")
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat read-only database: %v", err)
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("read-only open changed database metadata: before=%v after=%v", before, after)
	}

	missing := filepath.Join(t.TempDir(), "missing.db")
	if _, _, err := openFirewallReadOnlyDatabase(missing); err == nil {
		t.Fatal("missing database unexpectedly opened")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("missing database was created: stat err=%v", err)
	}
}

func testFirewallPlanHash(t *testing.T, inputs firewall.Inputs, adapter *launcherAdapter) string {
	t.Helper()
	rules, err := firewall.BuildRules(inputs, adapter.iface)
	if err != nil {
		t.Fatal(err)
	}
	return firewallPlanHash(inputs, firewall.ActionApply, rules)
}
