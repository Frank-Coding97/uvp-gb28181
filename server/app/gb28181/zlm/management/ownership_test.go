package management

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playback"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/repo"
)

func TestOwnershipResolverProvesLiveGenerationOnlyWithCurrentLocation(t *testing.T) {
	target := testOwnershipTarget(7, "live-1")
	current := play.LiveSession{DeviceID: "device-1", ChannelID: "channel-1", StreamID: "live-1", SSRC: "ssrc-1", Generation: 4, NodeID: 7, State: play.LiveStateReady}
	locations := stream.NewLocationMap()
	require.True(t, locations.BindCurrent(current.Ref()))

	resolver := NewOwnershipResolver(OwnershipDependencies{
		Presence: staticPresence(true),
		Sources:  []OwnershipSource{NewLiveOwnershipAdapter(staticLiveReader{session: current}, locations)},
	})
	resolved, err := resolver.Resolve(context.Background(), target)
	require.NoError(t, err)
	require.Equal(t, OwnershipStatusOwned, resolved.Status)
	require.Equal(t, OwnershipTypeRealtimePlayback, resolved.Owners[0].Type)
	require.True(t, resolved.Present)
	require.True(t, resolved.PresenceKnown)

	locations.Unbind(target.Media.Stream)
	uncertain, err := resolver.Resolve(context.Background(), target)
	require.NoError(t, err)
	require.Equal(t, OwnershipStatusUnknown, uncertain.Status)
	require.Equal(t, OwnershipConfidenceUncertain, uncertain.Owners[0].Confidence)
	require.False(t, uncertain.CanNormalClose())
}

func TestOwnershipResolverMarksLiveNodeOrGenerationConflict(t *testing.T) {
	target := testOwnershipTarget(7, "live-conflict")
	current := play.LiveSession{DeviceID: "device-1", ChannelID: "channel-1", StreamID: target.Media.Stream, SSRC: "ssrc-1", Generation: 4, NodeID: 7, State: play.LiveStateReady}

	tests := []struct {
		name string
		ref  stream.LiveRef
	}{
		{name: "node", ref: stream.LiveRef{StreamID: target.Media.Stream, SSRC: current.SSRC, Generation: current.Generation, NodeID: 8}},
		{name: "generation", ref: stream.LiveRef{StreamID: target.Media.Stream, SSRC: current.SSRC, Generation: current.Generation + 1, NodeID: target.NodeID}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			locations := stream.NewLocationMap()
			require.True(t, locations.BindCurrent(test.ref))
			resolver := NewOwnershipResolver(OwnershipDependencies{
				Presence: staticPresence(true),
				Sources:  []OwnershipSource{NewLiveOwnershipAdapter(staticLiveReader{session: current}, locations)},
			})

			resolved, err := resolver.Resolve(context.Background(), target)
			require.NoError(t, err)
			require.Equal(t, OwnershipStatusConflicted, resolved.Status)
			require.Len(t, resolved.Owners, 1)
			require.True(t, resolved.Owners[0].Conflict)
			require.False(t, resolved.CanNormalClose())
		})
	}
}

func TestOwnershipResolverIgnoresTerminalDevicePlaybackAndMatchesActiveNode(t *testing.T) {
	target := testOwnershipTarget(8, "pb-1")
	active := &playback.Session{ID: "pb-session", OwnerID: "user-9", DeviceID: "device-1", ChannelID: "channel-1", NodeID: "8", StreamID: target.Media.Stream, State: playback.StatePlaying}
	reader := staticPlaybackReader{session: active}
	resolver := NewOwnershipResolver(OwnershipDependencies{
		Presence: staticPresence(true),
		Sources:  []OwnershipSource{NewDevicePlaybackOwnershipAdapter(reader)},
	})

	resolved, err := resolver.Resolve(context.Background(), target)
	require.NoError(t, err)
	require.Equal(t, OwnershipStatusOwned, resolved.Status)
	require.Equal(t, "pb-session", resolved.Owners[0].Key)

	active.State = playback.StateEnded
	resolved, err = resolver.Resolve(context.Background(), target)
	require.NoError(t, err)
	require.Equal(t, OwnershipStatusUnknown, resolved.Status)
	require.Empty(t, resolved.Owners)
}

func TestOwnershipResolverTalkRequiresProvenVHost(t *testing.T) {
	target := testOwnershipTarget(9, "talk-source")
	target.Media.App = "talk"
	session := gbmodels.GbTalkSession{SessionID: "talk-1", ChannelID: 11, DeviceID: "device-1", NodeID: 9, App: "talk", SourceStream: target.Media.Stream, State: gbmodels.TalkSessionActive}
	reader := staticTalkReader{sessions: []gbmodels.GbTalkSession{session}}

	withoutVHost := NewOwnershipResolver(OwnershipDependencies{
		Presence: staticPresence(true),
		Sources:  []OwnershipSource{NewTalkOwnershipAdapter(reader, "")},
	})
	resolved, err := withoutVHost.Resolve(context.Background(), target)
	require.NoError(t, err)
	require.Equal(t, OwnershipStatusUnknown, resolved.Status)
	require.Equal(t, OwnershipConfidenceUncertain, resolved.Owners[0].Confidence)

	withVHost := NewOwnershipResolver(OwnershipDependencies{
		Presence: staticPresence(true),
		Sources:  []OwnershipSource{NewTalkOwnershipAdapter(reader, target.Media.Vhost)},
	})
	resolved, err = withVHost.Resolve(context.Background(), target)
	require.NoError(t, err)
	require.Equal(t, OwnershipStatusOwned, resolved.Status)
	require.Equal(t, OwnershipTypeTalk, resolved.Owners[0].Type)
}

func TestOwnershipResolverRecordingMatchesExactMediaAndActiveStates(t *testing.T) {
	target := testOwnershipTarget(10, "recording-1")
	session := &gbmodels.GbRecordingSession{ID: 21, ChannelID: 31, DeviceID: "device-1", NodeID: 10, VHost: target.Media.Vhost, App: target.Media.App, Stream: target.Media.Stream, State: gbmodels.RecordingSessionStateRecording}
	reader := staticRecordingReader{session: session}
	resolver := NewOwnershipResolver(OwnershipDependencies{
		Presence: staticPresence(true),
		Sources:  []OwnershipSource{NewRecordingSessionOwnershipAdapter(reader)},
	})

	resolved, err := resolver.Resolve(context.Background(), target)
	require.NoError(t, err)
	require.Equal(t, OwnershipStatusOwned, resolved.Status)
	require.Equal(t, OwnershipTypeRecordingSession, resolved.Owners[0].Type)

	session.State = gbmodels.RecordingSessionStateFailed
	resolved, err = resolver.Resolve(context.Background(), target)
	require.NoError(t, err)
	require.Equal(t, OwnershipStatusUnknown, resolved.Status)
	require.Empty(t, resolved.Owners)
}

func TestOwnershipResolverManagedLedgerDoesNotInventZLMPresence(t *testing.T) {
	target := testOwnershipTarget(11, "managed-1")
	ledger := staticManagedReader{rows: []gbmodels.GbZLMManagedResource{{
		NodeID: target.NodeID, ResourceType: "pull_proxy", ResourceKey: "proxy-1", Schema: target.Media.Schema, Vhost: target.Media.Vhost, App: target.Media.App, Stream: target.Media.Stream,
		CreatedBy: 88,
	}}}
	resolver := NewOwnershipResolver(OwnershipDependencies{
		Presence: staticPresence(false),
		Sources:  []OwnershipSource{NewManagedResourceOwnershipAdapter(ledger)},
	})

	resolved, err := resolver.Resolve(context.Background(), target)
	require.NoError(t, err)
	require.Equal(t, OwnershipStatusManaged, resolved.Status)
	require.False(t, resolved.Present)
	require.True(t, resolved.PresenceKnown)
	require.True(t, resolved.CanNormalClose())
}

func TestOwnershipResolverManagedLedgerMatchesOnlyCompleteMediaIdentity(t *testing.T) {
	target := testOwnershipTarget(21, "same-stream")
	ledger := staticManagedReader{rows: []gbmodels.GbZLMManagedResource{
		{NodeID: target.NodeID, ResourceType: "pull_proxy", ResourceKey: "exact", Schema: target.Media.Schema, Vhost: target.Media.Vhost, App: target.Media.App, Stream: target.Media.Stream, CreatedBy: 1},
		{NodeID: target.NodeID, ResourceType: "pull_proxy", ResourceKey: "other-schema", Schema: "rtmp", Vhost: target.Media.Vhost, App: target.Media.App, Stream: target.Media.Stream, CreatedBy: 2},
		{NodeID: target.NodeID, ResourceType: "pull_proxy", ResourceKey: "other-vhost", Schema: target.Media.Schema, Vhost: "tenant-vhost", App: target.Media.App, Stream: target.Media.Stream, CreatedBy: 3},
	}}
	resolver := NewOwnershipResolver(OwnershipDependencies{
		Presence: staticPresence(true),
		Sources:  []OwnershipSource{NewManagedResourceOwnershipAdapter(ledger)},
	})

	resolved, err := resolver.Resolve(context.Background(), target)
	require.NoError(t, err)
	require.Equal(t, OwnershipStatusManaged, resolved.Status)
	require.Len(t, resolved.Owners, 1)
	require.Equal(t, "exact", resolved.Owners[0].Key)
	require.Equal(t, OwnershipConfidenceProven, resolved.Owners[0].Confidence)
	require.True(t, resolved.CanNormalClose())

	otherSchema := target
	otherSchema.Media.Schema = "rtmp"
	resolved, err = resolver.Resolve(context.Background(), otherSchema)
	require.NoError(t, err)
	require.Equal(t, OwnershipStatusManaged, resolved.Status)
	require.Len(t, resolved.Owners, 1)
	require.Equal(t, "other-schema", resolved.Owners[0].Key)

	unknownTuple := target
	unknownTuple.Media.Vhost = "missing-vhost"
	resolved, err = resolver.Resolve(context.Background(), unknownTuple)
	require.NoError(t, err)
	require.Equal(t, OwnershipStatusUnknown, resolved.Status)
	require.Empty(t, resolved.Owners)
	require.False(t, resolved.CanNormalClose())
}

func TestOwnershipResolverTreatsLegacyManagedRowWithoutSchemaOrVhostAsUncertain(t *testing.T) {
	target := testOwnershipTarget(22, "legacy-stream")
	ledger := staticManagedReader{rows: []gbmodels.GbZLMManagedResource{{
		NodeID: target.NodeID, ResourceType: "pull_proxy", ResourceKey: "legacy", App: target.Media.App, Stream: target.Media.Stream,
		CreatedBy: 88,
	}}}
	resolver := NewOwnershipResolver(OwnershipDependencies{
		Presence: staticPresence(true),
		Sources:  []OwnershipSource{NewManagedResourceOwnershipAdapter(ledger)},
	})

	resolved, err := resolver.Resolve(context.Background(), target)
	require.NoError(t, err)
	require.Equal(t, OwnershipStatusUnknown, resolved.Status)
	require.Len(t, resolved.Owners, 1)
	require.Equal(t, OwnershipTypeManaged, resolved.Owners[0].Type)
	require.Equal(t, OwnershipConfidenceUncertain, resolved.Owners[0].Confidence)
	require.False(t, resolved.CanNormalClose())
}

func TestOwnershipResolverAggregatesLegitimateBusinessHoldersWithoutConflict(t *testing.T) {
	target := testOwnershipTarget(15, "shared-1")
	resolver := NewOwnershipResolver(OwnershipDependencies{
		Presence: staticPresence(true),
		Sources: []OwnershipSource{
			staticOwnershipSource{evidence: OwnershipEvidence{Type: OwnershipTypeCascade, Key: "shared-1", Confidence: OwnershipConfidenceUncertain}},
			staticOwnershipSource{evidence: OwnershipEvidence{Type: OwnershipTypeRecordingSession, Key: "recording/7", Confidence: OwnershipConfidenceProven}},
			staticOwnershipSource{evidence: OwnershipEvidence{Type: OwnershipTypeRealtimePlayback, Key: "live/3", Confidence: OwnershipConfidenceProven}},
			staticOwnershipSource{evidence: OwnershipEvidence{Type: OwnershipTypeRecordingPlan, Key: "shared-1", Confidence: OwnershipConfidenceUncertain}},
		},
	})

	resolved, err := resolver.Resolve(context.Background(), target)
	require.NoError(t, err)
	require.Equal(t, OwnershipStatusOwned, resolved.Status)
	require.Equal(t, OwnershipTypeRealtimePlayback, resolved.Owners[0].Type)
	require.Len(t, resolved.Impacts, 4)
	require.False(t, resolved.CanNormalClose())
}

func TestOwnershipResolverManagedAndBusinessHolderRemainOwned(t *testing.T) {
	target := testOwnershipTarget(16, "managed-shared")
	resolver := NewOwnershipResolver(OwnershipDependencies{
		Presence: staticPresence(true),
		Sources: []OwnershipSource{
			staticOwnershipSource{evidence: OwnershipEvidence{Type: OwnershipTypeRealtimePlayback, Key: "live/4", Confidence: OwnershipConfidenceProven}},
			staticOwnershipSource{evidence: OwnershipEvidence{Type: OwnershipTypeManaged, ResourceType: "pull_proxy", Key: "proxy-1", Confidence: OwnershipConfidenceProven}},
		},
	})
	resolved, err := resolver.Resolve(context.Background(), target)
	require.NoError(t, err)
	require.Equal(t, OwnershipStatusOwned, resolved.Status)
	require.False(t, resolved.CanNormalClose())
}

func TestOwnershipResolverUncertainManagedEvidenceFailsClosed(t *testing.T) {
	target := testOwnershipTarget(20, "managed-uncertain")
	resolver := NewOwnershipResolver(OwnershipDependencies{
		Presence: staticPresence(true),
		Sources: []OwnershipSource{staticOwnershipSource{evidence: OwnershipEvidence{
			Type: OwnershipTypeManaged, Key: "managed", Confidence: OwnershipConfidenceUncertain,
		}}},
	})

	resolved, err := resolver.Resolve(context.Background(), target)
	require.NoError(t, err)
	require.Equal(t, OwnershipStatusUnknown, resolved.Status)
	require.False(t, resolved.CanNormalClose())
}

func TestOwnershipResolverUnknownAndConflictedFailClosed(t *testing.T) {
	target := testOwnershipTarget(12, "unknown-1")
	resolver := NewOwnershipResolver(OwnershipDependencies{Presence: staticPresence(true)})
	unknown, err := resolver.Resolve(context.Background(), target)
	require.NoError(t, err)
	require.Equal(t, OwnershipStatusUnknown, unknown.Status)
	require.False(t, unknown.CanNormalClose())

	conflicted := NewOwnershipResolver(OwnershipDependencies{
		Presence: staticPresence(true),
		Sources: []OwnershipSource{
			staticOwnershipSource{evidence: OwnershipEvidence{Type: OwnershipTypeRealtimePlayback, Key: "live", Confidence: OwnershipConfidenceProven, Conflict: true}},
			staticOwnershipSource{evidence: OwnershipEvidence{Type: OwnershipTypeTalk, Key: "talk", Confidence: OwnershipConfidenceProven}},
		},
	})
	got, err := conflicted.Resolve(context.Background(), target)
	require.NoError(t, err)
	require.Equal(t, OwnershipStatusConflicted, got.Status)
	require.False(t, got.CanNormalClose())
}

func TestOwnershipResolverNormalizesUnrecognizedSourceEvidence(t *testing.T) {
	target := testOwnershipTarget(17, "unknown-type")
	resolver := NewOwnershipResolver(OwnershipDependencies{
		Presence: staticPresence(true),
		Sources: []OwnershipSource{staticOwnershipSource{evidence: OwnershipEvidence{
			Type: OwnershipType("private_source"), Key: "source", Confidence: OwnershipConfidenceProven,
		}}},
	})

	resolved, err := resolver.Resolve(context.Background(), target)
	require.NoError(t, err)
	require.Equal(t, OwnershipStatusUnknown, resolved.Status)
	require.Len(t, resolved.Owners, 1)
	require.Equal(t, OwnershipTypeUnknown, resolved.Owners[0].Type)
	require.Equal(t, OwnershipConfidenceUncertain, resolved.Owners[0].Confidence)
	require.False(t, resolved.CanNormalClose())
}

func TestOwnershipFingerprintIsOrderIndependentButChangesWithTargetOrOwner(t *testing.T) {
	one := testOwnershipTarget(1, "one")
	two := testOwnershipTarget(2, "two")
	a := OwnershipSnapshot{Target: one, Present: true, PresenceKnown: true, Status: OwnershipStatusManaged,
		Owners: []OwnershipEvidence{{Type: OwnershipTypeManaged, Key: "a", Confidence: OwnershipConfidenceProven}}}
	b := OwnershipSnapshot{Target: two, Present: true, PresenceKnown: true, Status: OwnershipStatusUnknown,
		Owners: []OwnershipEvidence{{Type: OwnershipTypeUnknown, Key: "unknown", Confidence: OwnershipConfidenceUncertain}}}

	first := FingerprintOwnershipSnapshots([]OwnershipSnapshot{a, b})
	second := FingerprintOwnershipSnapshots([]OwnershipSnapshot{b, a})
	require.Equal(t, first, second)
	require.Len(t, first, 64)

	b.Target.Media.Stream = "two-changed"
	require.NotEqual(t, first, FingerprintOwnershipSnapshots([]OwnershipSnapshot{a, b}))
	b.Target.Media.Stream = "two"
	b.Owners[0].Key = "unknown-changed"
	require.NotEqual(t, first, FingerprintOwnershipSnapshots([]OwnershipSnapshot{a, b}))
}

func TestOwnershipExecuteRechecksFingerprintBeforeCallingAction(t *testing.T) {
	target := testOwnershipTarget(13, "race-1")
	presence := &mutablePresence{present: true}
	resolver := NewOwnershipResolver(OwnershipDependencies{
		Presence: presence,
		Sources:  []OwnershipSource{staticOwnershipSource{evidence: OwnershipEvidence{Type: OwnershipTypeManaged, Key: "managed", Confidence: OwnershipConfidenceProven}}},
	})
	preflight, err := resolver.Preflight(context.Background(), target)
	require.NoError(t, err)
	presence.present = false

	var calls atomic.Int32
	err = resolver.Execute(context.Background(), preflight, func(context.Context, OwnershipTarget) error {
		calls.Add(1)
		return nil
	})
	require.Error(t, err)
	var managementErr *ManagementError
	require.ErrorAs(t, err, &managementErr)
	require.Equal(t, CodeOwnershipConflict, managementErr.Code)
	require.Zero(t, calls.Load())

	preflight, err = resolver.Preflight(context.Background(), target)
	require.NoError(t, err)
	err = resolver.Execute(context.Background(), preflight, func(context.Context, OwnershipTarget) error {
		calls.Add(1)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, int32(1), calls.Load())
}

func TestOwnershipForceIsSeparateFromNormalExecution(t *testing.T) {
	target := testOwnershipTarget(14, "force-1")
	resolver := NewOwnershipResolver(OwnershipDependencies{
		Presence: staticPresence(true),
		Sources:  []OwnershipSource{staticOwnershipSource{evidence: OwnershipEvidence{Type: OwnershipTypeUnknown, Key: "uncertain", Confidence: OwnershipConfidenceUncertain}}},
	})
	preflight, err := resolver.Preflight(context.Background(), target)
	require.NoError(t, err)
	called := false
	require.Error(t, resolver.Execute(context.Background(), preflight, func(context.Context, OwnershipTarget) error {
		called = true
		return nil
	}))
	require.False(t, called)
	require.NoError(t, resolver.ExecuteForce(context.Background(), preflight, "incident-42", func(context.Context, OwnershipTarget) error {
		called = true
		return nil
	}))
	require.True(t, called)
	require.Error(t, resolver.ExecuteForce(context.Background(), preflight, "", func(context.Context, OwnershipTarget) error { return nil }))
}

func TestOwnershipExecuteBatchRechecksEveryTargetBeforeCallingAction(t *testing.T) {
	presence := &mutablePresence{present: true}
	resolver := NewOwnershipResolver(OwnershipDependencies{
		Presence: presence,
		Sources:  []OwnershipSource{staticOwnershipSource{evidence: OwnershipEvidence{Type: OwnershipTypeManaged, Key: "managed", Confidence: OwnershipConfidenceProven}}},
	})
	preflight, err := resolver.PreflightBatch(context.Background(), []OwnershipTarget{
		testOwnershipTarget(19, "batch-2"),
		testOwnershipTarget(18, "batch-1"),
	})
	require.NoError(t, err)
	require.Equal(t, FingerprintOwnershipSnapshots(preflight.Snapshots), preflight.Fingerprint)
	presence.present = false

	var calls atomic.Int32
	err = resolver.ExecuteBatch(context.Background(), preflight, func(context.Context, OwnershipTarget) error {
		calls.Add(1)
		return nil
	})
	require.Error(t, err)
	var managementErr *ManagementError
	require.ErrorAs(t, err, &managementErr)
	require.Equal(t, CodeOwnershipConflict, managementErr.Code)
	require.Zero(t, calls.Load())
}

func TestOwnershipPreflightJSONUsesStableLowerCamelCaseFields(t *testing.T) {
	target := testOwnershipTarget(7, "contract")
	snapshot := OwnershipSnapshot{
		Target: target, Present: true, PresenceKnown: true, Status: OwnershipStatusManaged,
		Fingerprint: "snapshot-fingerprint",
	}

	encoded, err := json.Marshal(OwnershipPreflight{
		Target: target, Snapshot: snapshot, Fingerprint: "single-fingerprint",
	})
	require.NoError(t, err)
	var single map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &single))
	require.Contains(t, single, "target")
	require.Contains(t, single, "snapshot")
	require.Contains(t, single, "fingerprint")
	require.NotContains(t, single, "Target")

	encoded, err = json.Marshal(OwnershipBatchPreflight{
		Targets: []OwnershipTarget{target}, Snapshots: []OwnershipSnapshot{snapshot}, Fingerprint: "batch-fingerprint",
	})
	require.NoError(t, err)
	var batch map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &batch))
	require.Contains(t, batch, "targets")
	require.Contains(t, batch, "snapshots")
	require.Contains(t, batch, "fingerprint")
	require.NotContains(t, batch, "Targets")
}

func testOwnershipTarget(nodeID int64, streamID string) OwnershipTarget {
	return OwnershipTarget{NodeID: nodeID, Media: MediaIdentity{Schema: "rtsp", Vhost: "__defaultVhost__", App: "rtp", Stream: streamID}}
}

type staticPresence bool

func (p staticPresence) IsPresent(context.Context, OwnershipTarget) (bool, error) {
	return bool(p), nil
}

type mutablePresence struct{ present bool }

func (p *mutablePresence) IsPresent(context.Context, OwnershipTarget) (bool, error) {
	return p.present, nil
}

type staticLiveReader struct{ session play.LiveSession }

func (r staticLiveReader) CurrentSession(string) (play.LiveSession, bool) {
	return r.session, r.session.StreamID != ""
}

type staticPlaybackReader struct{ session *playback.Session }

func (r staticPlaybackReader) GetByStreamID(string) (*playback.Session, bool) {
	return r.session, r.session != nil
}

type staticTalkReader struct{ sessions []gbmodels.GbTalkSession }

func (r staticTalkReader) ListNonterminal(context.Context) ([]gbmodels.GbTalkSession, error) {
	return r.sessions, nil
}

type staticRecordingReader struct{ session *gbmodels.GbRecordingSession }

func (r staticRecordingReader) FindSessionByMedia(context.Context, int64, string, string, string) (*gbmodels.GbRecordingSession, error) {
	return r.session, nil
}

type staticManagedReader struct {
	rows []gbmodels.GbZLMManagedResource
}

func (r staticManagedReader) List(context.Context, repo.ManagedResourceFilter) ([]gbmodels.GbZLMManagedResource, error) {
	return r.rows, nil
}

type staticOwnershipSource struct{ evidence OwnershipEvidence }

func (s staticOwnershipSource) Resolve(context.Context, OwnershipTarget) ([]OwnershipEvidence, error) {
	return []OwnershipEvidence{s.evidence}, nil
}
