package businessreadiness_test

import (
	"context"
	"net"
	"net/netip"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/businessreadiness"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

func setCandidateMediaAddresses(t *testing.T, registry *node.Registry, id int64, receiveHost, playbackHost string) {
	t.Helper()
	candidate, ok := registry.Get(id)
	require.True(t, ok)
	candidate.ReceiveHost = receiveHost
	candidate.PlaybackHost = playbackHost
	require.NoError(t, registry.Update(context.Background(), *candidate))
	require.True(t, registry.SetAutoOnDemandReady(id, true))
}

func TestProbeRejectsInvalidCandidateMediaAddresses(t *testing.T) {
	tests := []struct {
		name     string
		receive  string
		playback string
	}{
		{name: "missing receive", receive: "", playback: "192.0.2.11"},
		{name: "missing playback", receive: "192.0.2.10", playback: ""},
		{name: "wildcard", receive: "0.0.0.0", playback: "192.0.2.11"},
		{name: "multicast", receive: "239.1.1.1", playback: "192.0.2.11"},
		{name: "limited broadcast", receive: "255.255.255.255", playback: "192.0.2.11"},
		{name: "ipv6", receive: "::1", playback: "192.0.2.11"},
		{name: "hostname does not resolve", receive: "media.example", playback: "192.0.2.11"},
		{name: "localhost is not a media IP", receive: "localhost", playback: "192.0.2.11"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry, added, media, _, client, now, _ := setupProbe(t)
			setCandidateMediaAddresses(t, registry, added.ID, tt.receive, tt.playback)
			probe := businessreadiness.NewProbe(businessreadiness.ProbeConfig{
				Client: client, Now: now,
				LocalAddressAvailable: func(string) bool { return true },
			})
			input := probeInput(registry, media)
			input.RequireLocalMediaAddresses = true

			result := probe.Check(context.Background(), input)
			require.False(t, result.BusinessReady)
			require.Equal(t, businessreadiness.ReasonMediaAddressUnavailable, result.Reason)
			require.Equal(t, 0, client.count(), "invalid saved media address must fail before ZLM readback")
		})
	}
}

func TestProbeUsesCandidateMediaAddressesAfterUniqueSelection(t *testing.T) {
	registry, added, media, _, client, now, _ := setupProbe(t)
	setCandidateMediaAddresses(t, registry, added.ID, "192.0.2.10", "192.0.2.11")

	var checked []string
	probe := businessreadiness.NewProbe(businessreadiness.ProbeConfig{
		Client: client, Now: now,
		LocalAddressAvailable: func(host string) bool {
			checked = append(checked, host)
			return true
		},
	})
	input := probeInput(registry, media)
	input.RequireLocalMediaAddresses = true
	// Management endpoint configuration is not the source of the saved media
	// addresses. Leave both fields empty to catch accidental fallback to it.
	input.ZLM.ReceiveHost = ""
	input.ZLM.PlaybackHost = ""

	result := probe.Check(context.Background(), input)
	require.Equal(t, businessreadiness.ReasonHookUnconfirmed, result.Reason)
	require.Equal(t, []string{"192.0.2.10", "192.0.2.11"}, checked)
	require.Equal(t, 1, client.count())

	current, ok := registry.Get(added.ID)
	require.True(t, ok)
	require.Equal(t, "192.0.2.10", current.ReceiveHost)
	require.Equal(t, "192.0.2.11", current.PlaybackHost)
}

func TestProbeRequiresBothCandidateMediaAddressesToBeAvailable(t *testing.T) {
	registry, added, media, _, client, now, _ := setupProbe(t)
	setCandidateMediaAddresses(t, registry, added.ID, "192.0.2.10", "192.0.2.11")

	var checked []string
	probe := businessreadiness.NewProbe(businessreadiness.ProbeConfig{
		Client: client, Now: now,
		LocalAddressAvailable: func(host string) bool {
			checked = append(checked, host)
			return host == "192.0.2.10"
		},
	})
	input := probeInput(registry, media)
	input.RequireLocalMediaAddresses = true

	result := probe.Check(context.Background(), input)
	require.Equal(t, businessreadiness.ReasonMediaAddressUnavailable, result.Reason)
	require.Equal(t, []string{"192.0.2.10", "192.0.2.11"}, checked)
	require.Equal(t, 0, client.count(), "address scan failure must fail before ZLM readback")
}

func TestProbeAllowsPublicCandidateMediaAddressesWhenAvailable(t *testing.T) {
	registry, added, media, _, client, now, _ := setupProbe(t)
	setCandidateMediaAddresses(t, registry, added.ID, "203.0.113.10", "198.51.100.11")
	probe := businessreadiness.NewProbe(businessreadiness.ProbeConfig{
		Client: client, Now: now,
		LocalAddressAvailable: func(string) bool { return true },
	})
	input := probeInput(registry, media)
	input.RequireLocalMediaAddresses = true

	result := probe.Check(context.Background(), input)
	require.Equal(t, businessreadiness.ReasonHookUnconfirmed, result.Reason)
	require.Equal(t, 1, client.count())
}

func TestProbeDefaultAddressScannerSupportsLoopback(t *testing.T) {
	if !hasLocalIPv4("127.0.0.1") {
		t.Skip("test host has no IPv4 loopback interface")
	}
	registry, added, media, _, client, now, _ := setupProbe(t)
	setCandidateMediaAddresses(t, registry, added.ID, "127.0.0.1", "127.0.0.1")
	probe := businessreadiness.NewProbe(businessreadiness.ProbeConfig{Client: client, Now: now})
	input := probeInput(registry, media)
	input.RequireLocalMediaAddresses = true

	result := probe.Check(context.Background(), input)
	require.Equal(t, businessreadiness.ReasonHookUnconfirmed, result.Reason)
}

func TestProbeDoesNotAssumePublicAddressesAreLocalWhenRequirementDisabled(t *testing.T) {
	registry, added, media, _, client, now, _ := setupProbe(t)
	setCandidateMediaAddresses(t, registry, added.ID, "203.0.113.10", "198.51.100.11")
	checks := 0
	probe := businessreadiness.NewProbe(businessreadiness.ProbeConfig{
		Client: client, Now: now,
		LocalAddressAvailable: func(string) bool {
			checks++
			return false
		},
	})
	input := probeInput(registry, media)
	input.RequireLocalMediaAddresses = false

	result := probe.Check(context.Background(), input)
	require.Equal(t, businessreadiness.ReasonHookUnconfirmed, result.Reason)
	require.Zero(t, checks)
}

func hasLocalIPv4(want string) bool {
	expected, err := netip.ParseAddr(want)
	if err != nil {
		return false
	}
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, address := range addresses {
		text := strings.TrimSpace(address.String())
		if prefix, err := netip.ParsePrefix(text); err == nil {
			if candidate := prefix.Addr().Unmap(); candidate.Is4() && candidate == expected {
				return true
			}
			continue
		}
		if candidate, err := netip.ParseAddr(text); err == nil {
			candidate = candidate.Unmap()
			if candidate.Is4() && candidate == expected {
				return true
			}
		}
	}
	return false
}
