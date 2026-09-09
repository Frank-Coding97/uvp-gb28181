package workrecording

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestSnapshotKeepsUnknownAndIndependentProgress(t *testing.T) {
	now := time.Date(2026, 9, 9, 1, 0, 0, 0, time.UTC)
	s := Snapshot{ID: "job-1", ChannelID: 8, State: StateUnknown, Version: 3, LastCheckedAt: &now, FileState: FileFinalizing, FormState: FormDraft}
	data, err := json.Marshal(s)
	require.NoError(t, err)
	var values map[string]any
	require.NoError(t, json.Unmarshal(data, &values))
	require.Equal(t, "unknown", values["state"])
	require.Equal(t, "job-1", values["id"])
	require.Equal(t, float64(3), values["version"])
	require.Equal(t, "finalizing", values["fileState"])
	require.Equal(t, "draft", values["formState"])
	require.NotEmpty(t, values["lastCheckedAt"])
	require.False(t, s.CanStart())
}

func TestStartRequestValidatesStableIdentity(t *testing.T) {
	for _, r := range []StartRequest{{}, {ChannelID: 1}, {RequestID: "a"}, {ChannelID: 1, RequestID: " "}} {
		require.Error(t, r.Validate())
	}
	require.NoError(t, (StartRequest{ChannelID: 1, RequestID: "request-1"}).Validate())
}

func TestSameRequestCannotChangeTarget(t *testing.T) {
	first := StartRequest{ChannelID: 1, RequestID: "request-1"}
	require.NoError(t, first.Matches(first))
	require.ErrorIs(t, first.Matches(StartRequest{ChannelID: 2, RequestID: "request-1"}), ErrRequestConflict)
}
