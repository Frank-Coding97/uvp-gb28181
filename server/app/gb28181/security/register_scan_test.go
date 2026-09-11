package security

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestRegisterScanRequiresDiverseIDsAndVerifiedTransactions(t *testing.T) {
	for _, tc := range []struct {
		name, transport string
		ids             int
		ban             bool
	}{
		{"fixed TCP configuration", "TCP", 1, false},
		{"two mistaken devices", "TCP", 2, false},
		{"TCP enumeration", "TCP", 3, true},
		{"unverified UDP enumeration", "UDP", 3, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clock := &fakeClock{now: time.Unix(100, 0)}
			s := NewScorer(DefaultPolicy(), clock, nil, []byte("test"))
			for i := 0; i < 10; i++ {
				event, d, err := s.Observe(Event{SourceIP: "198.51.100.10", Transport: tc.transport, Method: "REGISTER", DeviceID: fmt.Sprint(1000 + i%tc.ids), Reason: ReasonRegisterIDInvalid, TransactionID: fmt.Sprint(i)})
				require.NoError(t, err)
				if i == 9 && tc.ban {
					require.NotNil(t, d)
					require.True(t, d.Permanent)
					require.Equal(t, ReasonRegisterEnumeration, d.Reason)
					require.Equal(t, 10, d.TriggerCount)
				} else {
					require.Nil(t, d)
				}
				if i == 9 && tc.ids == 3 {
					require.Equal(t, ReasonRegisterEnumeration, event.Reason)
				}
				clock.now = clock.now.Add(30 * time.Second)
			}
		})
	}
}

func TestRegisterScanRetransmissionsAndWindow(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	s := NewScorer(DefaultPolicy(), clock, nil, nil)
	for i := 0; i < 100; i++ {
		_, d, err := s.Observe(Event{SourceIP: "198.51.100.10", Transport: "TCP", Method: "REGISTER", DeviceID: fmt.Sprint(1000 + i), Reason: ReasonRegisterIDInvalid, TransactionID: "same"})
		require.NoError(t, err)
		require.Nil(t, d)
	}
	clock.now = clock.now.Add(10 * time.Minute)
	for i := 0; i < 9; i++ {
		_, d, err := s.Observe(Event{SourceIP: "198.51.100.10", Transport: "TCP", Method: "REGISTER", DeviceID: fmt.Sprint(i + 1000), Reason: ReasonRegisterIDInvalid, TransactionID: fmt.Sprint(i)})
		require.NoError(t, err)
		require.Nil(t, d)
	}
	_, d, err := s.Observe(Event{SourceIP: "198.51.100.11", Transport: "TCP", Method: "REGISTER", DeviceID: "2000", Reason: ReasonRegisterIDInvalid, TransactionID: "last"})
	require.NoError(t, err)
	require.Nil(t, d)
}

func TestAutomaticBanProtectsAuthenticatedSharedSource(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	s := NewScorer(DefaultPolicy(), clock, nil, nil)
	require.NoError(t, s.UpdateTrustedEndpoint("34020000001320000001", "UDP", "198.51.100.10", clock.Now().Add(time.Minute)))
	clock.now = clock.now.Add(2 * time.Minute)
	_, ok := s.TrustedEndpoint("34020000001320000001")
	require.False(t, ok)
	for i := 0; i < 20; i++ {
		_, d, err := s.Observe(Event{SourceIP: "198.51.100.10", Transport: "TCP", Method: "INVITE", Reason: ReasonInviteRate, TransactionID: fmt.Sprint(i)})
		require.NoError(t, err)
		require.Nil(t, d, "expired registration must still protect shared egress")
	}
}

func TestUnverifiedUDPCannotPoisonLaterTCPBan(t *testing.T) {
	s := NewScorer(DefaultPolicy(), nil, nil, nil)
	for i := 0; i < 20; i++ {
		_, d, err := s.Observe(Event{SourceIP: "198.51.100.10", Transport: "UDP", Method: "INVITE", Reason: ReasonInviteRate, TransactionID: fmt.Sprint(i)})
		require.NoError(t, err)
		require.Nil(t, d)
	}
	_, d, err := s.Observe(Event{SourceIP: "198.51.100.10", Transport: "TCP", Method: "INVITE", Reason: ReasonInviteRate, TransactionID: "tcp"})
	require.NoError(t, err)
	require.Nil(t, d)
}

func TestRegisterScanUnbanResetsAndStateIsBounded(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	p := DefaultPolicy()
	p.MaxEventKeys = 4
	s := NewScorer(p, clock, nil, nil)
	for n := 1; n < 20; n++ {
		for i := 0; i < 150; i++ {
			_, _, err := s.Observe(Event{SourceIP: fmt.Sprintf("198.51.100.%d", n), Transport: "TCP", Method: "REGISTER", DeviceID: "5838", Reason: ReasonRegisterIDInvalid, TransactionID: fmt.Sprint(i)})
			require.NoError(t, err)
		}
	}
	require.LessOrEqual(t, len(s.registrations), 2*p.MaxEventKeys)
	for _, observations := range s.registrations {
		require.LessOrEqual(t, len(observations), maxRegisterObservations)
	}
	s.Unban("198.51.100.19")
	require.NotContains(t, s.registrations, "198.51.100.19")
	require.NotContains(t, s.registrations, "verified|198.51.100.19")
	for i := 0; i < 9; i++ {
		_, d, err := s.Observe(Event{SourceIP: "198.51.100.19", Transport: "TCP", Method: "REGISTER", DeviceID: fmt.Sprint(1000 + i), Reason: ReasonRegisterIDInvalid, TransactionID: fmt.Sprint(i)})
		require.NoError(t, err)
		require.Nil(t, d)
	}
}

func TestClaimingKnownDeviceDoesNotProtectAnotherSource(t *testing.T) {
	s := NewScorer(DefaultPolicy(), nil, nil, nil)
	id := "34020000001320000001"
	require.NoError(t, s.UpdateTrustedEndpoint(id, "UDP", "198.51.100.10", time.Now().Add(time.Hour)))
	for i := 0; i < 5; i++ {
		_, d, err := s.Observe(Event{SourceIP: "198.51.100.11", Transport: "TCP", DeviceID: id, RiskScope: ScopeSource, Reason: ReasonInviteRate, TransactionID: fmt.Sprint(i)})
		require.NoError(t, err)
		if i == 4 {
			require.NotNil(t, d)
		} else {
			require.Nil(t, d)
		}
	}
}

func TestUnverifiedRegisterDoesNotPromoteOneVerifiedRequestToBan(t *testing.T) {
	s := NewScorer(DefaultPolicy(), nil, nil, nil)
	for i := 0; i < 30; i++ {
		_, d, err := s.Observe(Event{SourceIP: "198.51.100.10", Transport: "UDP", Method: "REGISTER", DeviceID: fmt.Sprint(1000 + i), Reason: ReasonRegisterIDInvalid, TransactionID: fmt.Sprint(i)})
		require.NoError(t, err)
		require.Nil(t, d)
	}
	_, d, err := s.Observe(Event{SourceIP: "198.51.100.10", Transport: "TCP", Method: "REGISTER", DeviceID: "5555", Reason: ReasonRegisterIDInvalid, TransactionID: "tcp"})
	require.NoError(t, err)
	require.Nil(t, d)
}
