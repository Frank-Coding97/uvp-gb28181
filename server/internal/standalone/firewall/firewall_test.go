package firewall

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

const testGUID = "12345678-1234-1234-1234-123456789abc"

func testInputs(t *testing.T) Inputs {
	t.Helper()
	root := t.TempDir()
	return Inputs{
		CanonicalRoot:   root,
		BackendExe:      filepath.Join(root, "backend", "uvp-server.exe"),
		MediaExe:        filepath.Join(root, "media", "MediaServer.exe"),
		ReleaseVerified: true,
		SIP:             SIPConfig{ListenIP: "192.168.10.52", Port: 15070},
		MediaListeners: []standalone.MediaListener{
			{Network: "tcp", Address: "0.0.0.0:18080"},
			{Network: "tcp", Address: "0.0.0.0:40000"},
			{Network: "udp", Address: "0.0.0.0:40000"},
			{Network: "udp", Address: "0.0.0.0:18000"},
			{Network: "tcp", Address: "0.0.0.0:18000"},
		},
	}
}

func testInterface() InterfaceInfo {
	return InterfaceInfo{
		ID:      testGUID,
		Alias:   "Ethernet",
		Up:      true,
		Private: true,
		IPv4:    []string{"192.168.10.52"},
	}
}

func TestBuildRulesUsesOnlyInstancePortsAndConstrainedScope(t *testing.T) {
	inputs := testInputs(t)
	rules, err := BuildRules(inputs, testInterface())
	if err != nil {
		t.Fatalf("BuildRules() error = %v", err)
	}
	if len(rules) != 4 {
		t.Fatalf("rule count = %d, want 4", len(rules))
	}
	if rules[0].Protocol != "TCP" || rules[0].LocalPort != "15070" || rules[0].Role != "backend" {
		t.Fatalf("backend TCP rule = %+v", rules[0])
	}
	if rules[1].Protocol != "UDP" || rules[1].LocalPort != "15070" || rules[1].Program != inputs.BackendExe {
		t.Fatalf("backend UDP rule = %+v", rules[1])
	}
	if rules[2].Protocol != "TCP" || rules[2].Program != inputs.MediaExe || rules[2].LocalPort != "18000,18080,40000,30000-35000" {
		t.Fatalf("media TCP rule = %+v", rules[2])
	}
	if rules[3].Protocol != "UDP" || rules[3].LocalPort != "18000,40000,30000-35000" {
		t.Fatalf("media UDP rule = %+v", rules[3])
	}
	group := RuleGroup(inputs.CanonicalRoot)
	if group == "" {
		t.Fatal("RuleGroup returned empty group")
	}
	for _, rule := range rules {
		if rule.Group != group || rule.Direction != "Inbound" || rule.Action != "Allow" || rule.Profile != "Private" ||
			rule.RemoteAddress != "LocalSubnet" || rule.InterfaceAlias != "Ethernet" ||
			rule.EdgeTraversalPolicy != "Block" || !rule.Enabled {
			t.Fatalf("rule scope = %+v", rule)
		}
		if rule.LocalPort == "8280" || rule.LocalPort == "6379" {
			t.Fatalf("management or redis port leaked into rule: %+v", rule)
		}
	}
}

func TestPolicyRejectsUntrustedOrUnownedInputs(t *testing.T) {
	base := testInputs(t)
	cases := []struct {
		name   string
		mutate func(*Inputs, *InterfaceInfo)
		want   Reason
	}{
		{"unverified release", func(in *Inputs, _ *InterfaceInfo) { in.ReleaseVerified = false }, ReasonReleaseUnverified},
		{"relative backend", func(in *Inputs, _ *InterfaceInfo) { in.BackendExe = "backend.exe" }, ReasonInvalidExecutable},
		{"missing media", func(in *Inputs, _ *InterfaceInfo) { in.MediaListeners = nil }, ReasonMediaInvalid},
		{"unsupported media network", func(in *Inputs, _ *InterfaceInfo) { in.MediaListeners[0].Network = "icmp" }, ReasonMediaInvalid},
		{"down adapter", func(_ *Inputs, iface *InterfaceInfo) { iface.Up = false }, ReasonInterfaceDown},
		{"public adapter", func(_ *Inputs, iface *InterfaceInfo) { iface.Private = false }, ReasonInterfaceNotPrivate},
		{"loopback adapter", func(_ *Inputs, iface *InterfaceInfo) { iface.Loopback = true }, ReasonInterfaceLoopback},
		{"no IPv4", func(_ *Inputs, iface *InterfaceInfo) { iface.IPv4 = nil }, ReasonInterfaceNoIPv4},
		{"SIP on another adapter", func(_ *Inputs, iface *InterfaceInfo) { iface.IPv4 = []string{"192.168.10.53"} }, ReasonSIPNotOnInterface},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := base
			in.MediaListeners = append([]standalone.MediaListener(nil), base.MediaListeners...)
			iface := testInterface()
			tc.mutate(&in, &iface)
			_, err := BuildRules(in, iface)
			if got := reasonOf(err); got != tc.want {
				t.Fatalf("reason = %q, want %q (err=%v)", got, tc.want, err)
			}
		})
	}
}

func TestSelectInterfaceAllowsWildcardSIPWithUsablePrivateAdapter(t *testing.T) {
	inputs := testInputs(t)
	inputs.SIP.ListenIP = "0.0.0.0"
	selected, err := SelectInterface(inputs.SIP, []InterfaceInfo{testInterface()}, testGUID)
	if err != nil || selected.ID != testGUID {
		t.Fatalf("wildcard selection = %+v, err=%v", selected, err)
	}
	withoutAddress := testInterface()
	withoutAddress.IPv4 = nil
	_, err = SelectInterface(inputs.SIP, []InterfaceInfo{withoutAddress}, testGUID)
	if reasonOf(err) != ReasonInterfaceNoIPv4 {
		t.Fatalf("wildcard without usable IPv4 reason = %q, want %q", reasonOf(err), ReasonInterfaceNoIPv4)
	}
}

func TestSelectInterfaceRequiresOneExactPrivateAdapter(t *testing.T) {
	inputs := testInputs(t)
	_, err := SelectInterface(inputs.SIP, []InterfaceInfo{testInterface(), testInterface()}, testGUID)
	if reasonOf(err) != ReasonInterfaceAmbiguous {
		t.Fatalf("duplicate interface reason = %q, want %q", reasonOf(err), ReasonInterfaceAmbiguous)
	}
	_, err = SelectInterface(inputs.SIP, []InterfaceInfo{testInterface()}, "not-a-guid")
	if reasonOf(err) != ReasonInvalidInput {
		t.Fatalf("invalid GUID reason = %q, want %q", reasonOf(err), ReasonInvalidInput)
	}
}

func TestRuleNamesAreStableAndInstanceScoped(t *testing.T) {
	root := filepath.Join(t.TempDir(), "instance")
	one := RuleNames(root)
	two := RuleNames(root)
	other := RuleNames(filepath.Join(t.TempDir(), "instance"))
	if len(one) != 4 || one[0] != two[0] {
		t.Fatalf("unstable names: %v %v", one, two)
	}
	if one[0] == other[0] {
		t.Fatalf("different roots share rule name %q", one[0])
	}
	if len(one[0]) != len("UVP-")+32+len("-backend-tcp") {
		t.Fatalf("unexpected bounded name %q", one[0])
	}
}

type fakeAdapter struct {
	mu          sync.Mutex
	ifaces      []InterfaceInfo
	rules       map[string]Rule
	foreign     map[string]Rule
	maxInFlight int
	inFlight    int
	upsertCalls int
	removeCalls int
	block       <-chan struct{}
	reached     chan struct{}
	mismatch    bool
}

func newFakeAdapter() *fakeAdapter {
	return &fakeAdapter{
		ifaces:  []InterfaceInfo{testInterface()},
		rules:   make(map[string]Rule),
		foreign: make(map[string]Rule),
	}
}

func (a *fakeAdapter) enter() {
	a.mu.Lock()
	a.inFlight++
	if a.inFlight > a.maxInFlight {
		a.maxInFlight = a.inFlight
	}
	a.mu.Unlock()
}

func (a *fakeAdapter) leave() {
	a.mu.Lock()
	a.inFlight--
	a.mu.Unlock()
}

func (a *fakeAdapter) Interfaces(context.Context) ([]InterfaceInfo, error) {
	a.enter()
	defer a.leave()
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]InterfaceInfo(nil), a.ifaces...), nil
}

func (a *fakeAdapter) Inspect(_ context.Context, expected []Rule) ([]RuleState, error) {
	a.enter()
	defer a.leave()
	a.mu.Lock()
	defer a.mu.Unlock()
	states := make([]RuleState, 0, len(expected))
	for _, want := range expected {
		got, present := a.rules[want.Name]
		if foreign, isForeign := a.foreign[want.Name]; isForeign {
			present = true
			got = foreign
		}
		matches := present && len(want.Program) == 0
		conflict := present && want.Group != "" && got.Group != want.Group
		if len(want.Program) != 0 && present && !a.mismatch {
			matches = got == want
		}
		if conflict {
			matches = false
		}
		states = append(states, RuleState{Name: want.Name, Group: got.Group, Present: present, Matches: matches, Conflict: conflict})
	}
	return states, nil
}

func (a *fakeAdapter) Upsert(_ context.Context, rules []Rule) error {
	a.enter()
	defer a.leave()
	if a.reached != nil {
		select {
		case <-a.reached:
		default:
			close(a.reached)
		}
	}
	if a.block != nil {
		<-a.block
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.upsertCalls++
	for _, rule := range rules {
		a.rules[rule.Name] = rule
	}
	return nil
}

func (a *fakeAdapter) Remove(_ context.Context, names []string) error {
	a.enter()
	defer a.leave()
	a.mu.Lock()
	defer a.mu.Unlock()
	a.removeCalls++
	for _, name := range names {
		delete(a.rules, name)
	}
	return nil
}

func TestServiceApplyStatusRemoveIsReadBackAndExact(t *testing.T) {
	inputs := testInputs(t)
	adapter := newFakeAdapter()
	adapter.foreign["foreign-rule"] = Rule{Name: "foreign-rule"}
	service := NewService(adapter, func(context.Context) (Inputs, error) { return inputs, nil })

	applied, err := service.Apply(context.Background(), testGUID)
	if err != nil || !applied.Success || !applied.Converged {
		t.Fatalf("apply = %+v, err=%v", applied, err)
	}
	status, err := service.Status(context.Background())
	if err != nil || !status.Success || !status.Converged {
		t.Fatalf("status = %+v, err=%v", status, err)
	}
	removed, err := service.Remove(context.Background(), testGUID)
	if err != nil || !removed.Success || !removed.Converged {
		t.Fatalf("remove = %+v, err=%v", removed, err)
	}
	if _, ok := adapter.foreign["foreign-rule"]; !ok {
		t.Fatal("remove touched foreign rule")
	}
	if adapter.upsertCalls != 1 || adapter.removeCalls != 1 {
		t.Fatalf("calls = upsert %d remove %d", adapter.upsertCalls, adapter.removeCalls)
	}
}

func TestServiceRejectsMismatchedReadback(t *testing.T) {
	adapter := newFakeAdapter()
	adapter.mismatch = true
	service := NewService(adapter, func(context.Context) (Inputs, error) { return testInputs(t), nil })
	result, err := service.Apply(context.Background(), testGUID)
	if reasonOf(err) != ReasonReadbackFailed || result.Success || result.Reason != ReasonReadbackFailed {
		t.Fatalf("mismatch result = %+v, err=%v", result, err)
	}
}

func TestServiceRemoveUsesOnlyVerifiedInstanceIdentity(t *testing.T) {
	inputs := testInputs(t)
	inputs.SIP.ListenIP = "not-a-listen-address"
	inputs.MediaListeners = nil
	adapter := newFakeAdapter()
	for _, name := range RuleNames(inputs.CanonicalRoot) {
		adapter.rules[name] = Rule{Name: name, Group: RuleGroup(inputs.CanonicalRoot)}
	}
	service := NewService(adapter, func(context.Context) (Inputs, error) { return inputs, nil })
	result, err := service.Remove(context.Background())
	if err != nil || !result.Success || !result.Converged {
		t.Fatalf("remove with invalid live config = %+v, err=%v", result, err)
	}
}

func TestServiceRejectsForeignRuleBeforeMutation(t *testing.T) {
	inputs := testInputs(t)
	adapter := newFakeAdapter()
	names := RuleNames(inputs.CanonicalRoot)
	adapter.foreign[names[0]] = Rule{Name: names[0], Group: "unrelated-group"}
	service := NewService(adapter, func(context.Context) (Inputs, error) { return inputs, nil })
	result, err := service.Apply(context.Background(), testGUID)
	if reasonOf(err) != ReasonRuleConflict || result.Success || adapter.upsertCalls != 0 {
		t.Fatalf("foreign apply = %+v, err=%v, upserts=%d", result, err, adapter.upsertCalls)
	}
	result, err = service.Remove(context.Background())
	if reasonOf(err) != ReasonRuleConflict || result.Success || adapter.removeCalls != 0 {
		t.Fatalf("foreign remove = %+v, err=%v, removes=%d", result, err, adapter.removeCalls)
	}
}

func TestServiceStatusChecksCompleteExpectedRuleFields(t *testing.T) {
	inputs := testInputs(t)
	adapter := newFakeAdapter()
	rules, err := BuildRules(inputs, testInterface())
	if err != nil {
		t.Fatalf("BuildRules() error = %v", err)
	}
	for _, rule := range rules {
		rule.LocalPort = "1"
		adapter.rules[rule.Name] = rule
	}
	service := NewService(adapter, func(context.Context) (Inputs, error) { return inputs, nil })
	result, err := service.Status(context.Background())
	if err != nil || result.Success == false || result.Converged || result.Reason != ReasonRuleDrift {
		t.Fatalf("drift status = %+v, err=%v", result, err)
	}
}

func TestServiceStatusReportsForeignGroupConflict(t *testing.T) {
	inputs := testInputs(t)
	adapter := newFakeAdapter()
	names := RuleNames(inputs.CanonicalRoot)
	adapter.foreign[names[0]] = Rule{Name: names[0], Group: "unrelated-group"}
	service := NewService(adapter, func(context.Context) (Inputs, error) { return inputs, nil })
	result, err := service.Status(context.Background())
	if err != nil || !result.Success || result.Converged || result.Reason != ReasonRuleConflict {
		t.Fatalf("foreign status = %+v, err=%v", result, err)
	}
	if len(result.Rules) != 4 || !result.Rules[0].Conflict {
		t.Fatalf("foreign status rules = %+v", result.Rules)
	}
}

func TestServiceSerializesConcurrentOperationsAndHonorsWaitContext(t *testing.T) {
	inputs := testInputs(t)
	adapter := newFakeAdapter()
	service := NewService(adapter, func(context.Context) (Inputs, error) { return inputs, nil })
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = service.Apply(context.Background(), testGUID)
		}()
	}
	wg.Wait()
	adapter.mu.Lock()
	maxInFlight := adapter.maxInFlight
	adapter.mu.Unlock()
	if maxInFlight != 1 {
		t.Fatalf("adapter calls overlapped: max in flight %d", maxInFlight)
	}

	hold := make(chan struct{})
	adapter.reached = make(chan struct{})
	adapter.block = hold
	firstDone := make(chan struct{})
	go func() {
		_, _ = service.Apply(context.Background(), testGUID)
		close(firstDone)
	}()
	select {
	case <-adapter.reached:
	case <-time.After(time.Second):
		t.Fatal("first apply did not reach the adapter")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := service.Apply(ctx, testGUID)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting apply error = %v, want deadline", err)
	}
	close(hold)
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("blocked apply did not finish")
	}
}
