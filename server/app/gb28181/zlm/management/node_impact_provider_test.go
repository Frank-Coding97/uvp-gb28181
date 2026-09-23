package management

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	zlmservice "uvplatform.cn/uvp-gb28181/app/gb28181/zlm/service"
)

type nodeImpactSnapshotFake struct {
	media        []zlm.MediaInfo
	sessions     []zlm.Session
	mediaErr     error
	sessionsErr  error
	mediaCalls   int
	sessionCalls int
}

func (f *nodeImpactSnapshotFake) GetMediaListFresh(context.Context, int64) ([]zlm.MediaInfo, error) {
	f.mediaCalls++
	return append([]zlm.MediaInfo(nil), f.media...), f.mediaErr
}

func (f *nodeImpactSnapshotFake) GetAllSessionsFresh(context.Context, int64) ([]zlm.Session, error) {
	f.sessionCalls++
	return append([]zlm.Session(nil), f.sessions...), f.sessionsErr
}

func TestRuntimeNodeImpactProviderCountsAndHashesExactSets(t *testing.T) {
	reader := &nodeImpactSnapshotFake{
		media: []zlm.MediaInfo{
			{Schema: "rtsp", VHost: "vhost-b", App: "live", Stream: "stream-b", IsRecordingHLS: true},
			{Schema: "fmp4", VHost: "vhost-a", App: "rtp", Stream: "stream-a", IsRecordingMP4: true},
		},
		sessions: []zlm.Session{
			{ID: "session-b", Identifier: "identifier-b", Type: "TcpSession", PeerIP: "192.0.2.2", PeerPort: 2002, LocalIP: "192.0.2.10", LocalPort: 80},
			{ID: "session-a", Identifier: "identifier-a", Type: "TcpSession", PeerIP: "192.0.2.1", PeerPort: 2001, LocalIP: "192.0.2.10", LocalPort: 80},
		},
	}
	provider := NewRuntimeNodeImpactProvider(reader)

	first, err := provider.ReadNodeImpact(context.Background(), &node.Node{ID: 9})
	require.NoError(t, err)
	require.Equal(t, 2, first.Streams)
	require.Equal(t, 2, first.Recordings)
	require.Equal(t, 2, first.Sessions)
	require.False(t, first.Truncated)
	require.Len(t, first.EvidenceFingerprint, 64)
	require.NotContains(t, first.EvidenceFingerprint, "stream-a")

	reader.media[0], reader.media[1] = reader.media[1], reader.media[0]
	reader.sessions[0], reader.sessions[1] = reader.sessions[1], reader.sessions[0]
	second, err := provider.ReadNodeImpact(context.Background(), &node.Node{ID: 9})
	require.NoError(t, err)
	require.Equal(t, first.EvidenceFingerprint, second.EvidenceFingerprint, "ordering must not change the exact-set digest")
	require.Equal(t, 2, reader.mediaCalls)
	require.Equal(t, 2, reader.sessionCalls)
}

func TestRuntimeNodeImpactProviderFingerprintChangesWhenIdentityChangesAtSameCounts(t *testing.T) {
	reader := &nodeImpactSnapshotFake{
		media:    []zlm.MediaInfo{{Schema: "rtsp", VHost: "v", App: "live", Stream: "one", IsRecordingMP4: true}},
		sessions: []zlm.Session{{ID: "session-one", Identifier: "identifier-one"}},
	}
	provider := NewRuntimeNodeImpactProvider(reader)
	first, err := provider.ReadNodeImpact(context.Background(), &node.Node{ID: 3})
	require.NoError(t, err)

	reader.media[0].Stream = "two"
	reader.sessions[0].Identifier = "identifier-two"
	second, err := provider.ReadNodeImpact(context.Background(), &node.Node{ID: 3})
	require.NoError(t, err)

	require.Equal(t, first.Streams, second.Streams)
	require.Equal(t, first.Recordings, second.Recordings)
	require.Equal(t, first.Sessions, second.Sessions)
	require.NotEqual(t, first.EvidenceFingerprint, second.EvidenceFingerprint)
}

func TestRuntimeNodeImpactProviderFailsClosedOnIncompleteEvidence(t *testing.T) {
	for _, test := range []struct {
		name   string
		reader *nodeImpactSnapshotFake
	}{
		{name: "media unavailable", reader: &nodeImpactSnapshotFake{mediaErr: errors.New("secret upstream detail")}},
		{name: "sessions unavailable", reader: &nodeImpactSnapshotFake{sessionsErr: errors.New("secret upstream detail")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := NewRuntimeNodeImpactProvider(test.reader)
			_, err := provider.ReadNodeImpact(context.Background(), &node.Node{ID: 7})
			require.Error(t, err)
		})
	}

	provider := NewRuntimeNodeImpactProvider(nil)
	_, err := provider.ReadNodeImpact(context.Background(), &node.Node{ID: 7})
	require.ErrorIs(t, err, zlmservice.ErrNodeImpactProviderUnavailable)
}

func TestRuntimeNodeImpactProviderBoundsCountsButHashesCompleteEvidence(t *testing.T) {
	reader := &nodeImpactSnapshotFake{}
	for index := 0; index < zlmservice.MaxNodeImpactItems+1; index++ {
		reader.media = append(reader.media, zlm.MediaInfo{Schema: "rtsp", VHost: "v", App: "live", Stream: strings.Repeat("x", index%3+1)})
		reader.sessions = append(reader.sessions, zlm.Session{ID: strings.Repeat("s", index%5+1)})
	}
	provider := NewRuntimeNodeImpactProvider(reader)

	impact, err := provider.ReadNodeImpact(context.Background(), &node.Node{ID: 11})
	require.NoError(t, err)
	require.Equal(t, zlmservice.MaxNodeImpactItems+1, impact.Streams)
	require.Equal(t, zlmservice.MaxNodeImpactItems+1, impact.Sessions)
	require.True(t, impact.Truncated)
	require.Len(t, impact.EvidenceFingerprint, 64)
}
