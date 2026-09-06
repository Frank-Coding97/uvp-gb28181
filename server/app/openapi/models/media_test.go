package models

import (
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestOpenAPIMediaGrantAndViewerLifecycleConstraints(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.AutoMigrate(&PlayGrant{}, &Viewer{}))

	now := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	pending := PlayGrant{GrantID: "00000000-0000-4000-8000-000000000001", ClientID: 7,
		Scope: "play:live:apply", ClientEpoch: 1, ScopeEpoch: 1, DeviceEpoch: 1,
		IssuedAt: now, ExpiresAt: now.Add(time.Minute), State: GrantStatePending}
	require.NoError(t, db.Create(&pending).Error, "pending grant may wait for a real media session")

	for _, state := range []GrantState{GrantStateIssued, GrantStateBound} {
		row := pending
		row.GrantID = "00000000-0000-4000-8000-00000000000" + string(state)[:1]
		row.State = state
		require.Error(t, db.Create(&row).Error, "state %s must not be persisted without the complete media binding", state)
	}

	bound := mediaGrant("00000000-0000-4000-8000-000000000010", GrantStateBound)
	require.NoError(t, db.Create(&bound).Error)
	require.Error(t, db.Create(&bound).Error, "grant id is unique")

	viewer := mediaViewer("00000000-0000-4000-8000-000000000010", strings.Repeat("a", 32), "session-a")
	require.NoError(t, db.Create(&viewer).Error)
	duplicateViewer := viewer
	duplicateViewer.ID = 0
	require.Error(t, db.Create(&duplicateViewer).Error, "same node boot and identifier is one session")
	crossBootViewer := viewer
	crossBootViewer.ID = 0
	crossBootViewer.BootNonce = strings.Repeat("b", 32)
	crossBootViewer.GrantID = "00000000-0000-4000-8000-000000000011"
	require.NoError(t, db.Create(&crossBootViewer).Error, "the same identifier in another media process is distinct")

	missingIdentity := mediaViewer("00000000-0000-4000-8000-000000000012", "", "")
	require.Error(t, db.Create(&missingIdentity).Error, "viewer identity must never use an empty or synthetic key")

	invalidState := mediaGrant("00000000-0000-4000-8000-000000000013", GrantState("unknown"))
	require.Error(t, db.Create(&invalidState).Error)
	invalidEpoch := mediaGrant("00000000-0000-4000-8000-000000000014", GrantStateBound)
	invalidEpoch.ClientEpoch = -1
	require.Error(t, db.Create(&invalidEpoch).Error)
}

func mediaGrant(id string, state GrantState) PlayGrant {
	now := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	generation := uint64(9)
	return PlayGrant{
		GrantID: id, ClientID: 7, Scope: "play:live:apply", ClientEpoch: 1,
		ScopeEpoch: 2, DeviceEpoch: 3, DeviceID: stringPtr("34020000001320000001"),
		ChannelID: stringPtr("34020000001310000001"), NodeUUID: stringPtr("node-a"),
		BootNonce: stringPtr(strings.Repeat("a", 32)), Schema: stringPtr("ws"), VHost: stringPtr("__defaultVhost__"),
		App: stringPtr("rtp"), Stream: stringPtr("34020000001320000001_34020000001310000001"),
		MediaGeneration: &generation, Protocol: stringPtr("ws-flv"), IssuedAt: now,
		ExpiresAt: now.Add(time.Minute), State: state,
	}
}

func mediaViewer(grantID, bootNonce, identifier string) Viewer {
	now := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	return Viewer{
		GrantID: grantID, NodeUUID: "node-a", BootNonce: bootNonce, Identifier: identifier,
		Schema: "ws", VHost: "__defaultVhost__", App: "rtp",
		Stream: "34020000001320000001_34020000001310000001", State: ViewerStatePending,
		CreatedAt: now, UpdatedAt: now,
	}
}

func stringPtr(value string) *string { return &value }
