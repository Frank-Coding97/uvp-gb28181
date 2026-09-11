package firewall

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/security"
)

type runnerCall struct {
	name  string
	args  []string
	input string
}

type fakeRunner struct {
	calls []runnerCall
	fn    func(string, []string, string) ([]byte, error)
}

func (r *fakeRunner) Run(_ context.Context, name string, args []string, input []byte) ([]byte, error) {
	r.calls = append(r.calls, runnerCall{name: name, args: append([]string(nil), args...), input: string(input)})
	if r.fn != nil {
		return r.fn(name, args, string(input))
	}
	return nil, nil
}

func TestDetectNftConfigUsesDefaultRouteAndLocalDestination(t *testing.T) {
	runner := &fakeRunner{fn: func(name string, args []string, _ string) ([]byte, error) {
		require.Equal(t, "ip", name)
		require.Equal(t, []string{"-j", "route", "get", "1.1.1.1"}, args)
		return []byte(`[{"dev":"eth0","prefsrc":"192.168.168.101"}]`), nil
	}}
	cfg, err := DetectNftConfig(context.Background(), runner, 56002, nil)
	require.NoError(t, err)
	require.Equal(t, "eth0", cfg.Interface)
	require.Equal(t, "192.168.168.101", cfg.Destination.String())
	require.Equal(t, uint16(56002), cfg.Port)
}

func TestNftBackendCreatesOwnedTableAndUsesValidatedTimeoutElement(t *testing.T) {
	runner := &fakeRunner{fn: func(name string, args []string, input string) ([]byte, error) {
		if name == "nft" && strings.Join(args, " ") == "-j list table inet uvp_sip_guard" {
			return nil, errors.New("table missing")
		}
		if name == "nft" && strings.Join(args, " ") == "-j list set inet uvp_sip_guard blocked_v4" {
			return []byte(`{"nftables":[{"set":{"elem":[]}}]}`), nil
		}
		return nil, nil
	}}
	backend, err := NewNftBackend(context.Background(), runner, NftConfig{Interface: "eth0", Destination: mustIP(t, "192.168.168.101"), Port: 56002})
	require.NoError(t, err)
	require.Contains(t, runner.calls[1].input, "add table inet uvp_sip_guard")
	require.Contains(t, runner.calls[1].input, `iifname "eth0" ip daddr 192.168.168.101`)
	require.Contains(t, runner.calls[1].input, "tcp dport 56002")
	require.NotContains(t, runner.calls[1].input, " accept\n")
	require.NotContains(t, runner.calls[1].input, "flush ruleset")

	require.NoError(t, backend.Add("203.0.113.9", time.Now().Add(90*time.Second)))
	last := runner.calls[len(runner.calls)-1]
	require.Equal(t, "nft", last.name)
	require.Contains(t, last.input, "add element inet uvp_sip_guard blocked_v4 { 203.0.113.9 timeout")
	require.NotContains(t, last.input, ";")
}

func TestNftBackendCreatesPermanentElementWithoutTimeout(t *testing.T) {
	runner := &fakeRunner{fn: func(name string, args []string, input string) ([]byte, error) {
		if name == "nft" && strings.Join(args, " ") == "-j list table inet uvp_sip_guard" {
			return nil, errors.New("table missing")
		}
		if name == "nft" && strings.Join(args, " ") == "-j list set inet uvp_sip_guard blocked_v4" {
			return []byte(`{"nftables":[{"set":{"elem":[]}}]}`), nil
		}
		return nil, nil
	}}
	backend, err := NewNftBackend(context.Background(), runner, NftConfig{Interface: "eth0", Destination: mustIP(t, "192.168.168.101"), Port: 56002})
	require.NoError(t, err)
	require.NoError(t, backend.Add("203.0.113.10", time.Time{}))
	last := runner.calls[len(runner.calls)-1]
	require.Contains(t, last.input, "add element inet uvp_sip_guard blocked_v4 { 203.0.113.10 }")
	require.NotContains(t, last.input, "timeout")
}

func TestNftBackendListsPermanentElementsFromNftJSON(t *testing.T) {
	runner := &fakeRunner{fn: func(name string, args []string, _ string) ([]byte, error) {
		if name == "nft" && strings.Join(args, " ") == "-j list table inet uvp_sip_guard" {
			return nil, errors.New("table missing")
		}
		if name == "nft" && strings.Join(args, " ") == "-j list set inet uvp_sip_guard blocked_v4" {
			return []byte(`{"nftables":[{"set":{"elem":["203.0.113.10","203.0.113.11"]}}]}`), nil
		}
		return nil, nil
	}}
	backend, err := NewNftBackend(context.Background(), runner, NftConfig{Interface: "eth0", Destination: mustIP(t, "192.168.168.101"), Port: 56002})
	require.NoError(t, err)

	rules, err := backend.List()
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"203.0.113.10", "203.0.113.11"}, rules)
}

func TestNftBackendRebuildsExistingOwnedTable(t *testing.T) {
	runner := &fakeRunner{fn: func(name string, args []string, _ string) ([]byte, error) {
		if name == "nft" && strings.Join(args, " ") == "-j list table inet uvp_sip_guard" {
			return []byte(`{"nftables":[{"table":{"family":"inet","name":"uvp_sip_guard"}}]}`), nil
		}
		return nil, nil
	}}

	_, err := NewNftBackend(context.Background(), runner, NftConfig{
		Interface: "eth0", Destination: mustIP(t, "192.168.168.101"), Port: 56002,
	})
	require.NoError(t, err)
	require.Len(t, runner.calls, 2)
	require.Contains(t, runner.calls[1].input, "delete table inet uvp_sip_guard")
	require.Contains(t, runner.calls[1].input, "add table inet uvp_sip_guard")
}

func TestNftBackendRejectsSourceFromUnconfiguredAddressFamily(t *testing.T) {
	runner := &fakeRunner{fn: func(name string, args []string, _ string) ([]byte, error) {
		if name == "nft" && strings.Join(args, " ") == "-j list table inet uvp_sip_guard" {
			return nil, errors.New("table missing")
		}
		return nil, nil
	}}
	backend, err := NewNftBackend(context.Background(), runner, NftConfig{
		Interface: "eth0", Destination: mustIP(t, "192.168.168.101"), Port: 56002,
	})
	require.NoError(t, err)

	err = backend.Add("2001:db8::9", time.Now().Add(time.Minute))
	require.ErrorContains(t, err, "address family")
}

func TestAgentReconcileRemovesResidualKernelRules(t *testing.T) {
	backend := NewMemoryBackend()
	clock := &testClock{now: time.Unix(100, 0)}
	require.NoError(t, backend.Add("203.0.113.1", clock.now.Add(time.Hour)))
	agent := New(backend, clock, nil)
	desired := security.BanDecision{DecisionID: "d2", SourceIP: "203.0.113.2", CreatedAt: clock.now, TTL: time.Hour}
	require.NoError(t, agent.Reconcile([]security.BanDecision{desired}))
	rules, err := backend.List()
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"203.0.113.2"}, rules)
}

func mustIP(t *testing.T, raw string) net.IP {
	t.Helper()
	ip := net.ParseIP(raw)
	require.NotNil(t, ip)
	return ip
}
