package service

import (
	"context"
	"errors"
	"testing"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"

	"github.com/stretchr/testify/require"
)

func TestManagementSetPublishGroupKeepsCommittedStateAndAllowsSameValueRuntimeRetry(t *testing.T) {
	ctx := context.Background()
	store := newManagementStoreFake()
	runtime := &managementRuntimeFake{}
	svc := NewManagementService(store, nil, runtime, fakeClock{})
	created, err := svc.Create(ctx, validPlatformInput(nil))
	require.NoError(t, err)
	snapshot := store.projections[created.ID]
	snapshot.Nodes = []model.GbCascadeBusinessNode{{PlatformID: created.ID, Code: "34020000002150000001", Name: "业务根", Kind: "group"}}
	store.projections[created.ID] = snapshot

	runtime.err = errors.New("runtime reload unavailable")
	committed, err := svc.SetPublishGroup(ctx, created.ID, created.ConfigRevision, true)
	require.ErrorIs(t, err, ErrRuntimeSyncFailed)
	require.NotNil(t, committed)
	require.True(t, committed.PublishGroup)
	stored, err := store.FindPlatform(ctx, created.ID)
	require.NoError(t, err)
	require.True(t, stored.PublishGroup, "runtime error must not roll back persisted setting")
	require.Equal(t, created.ConfigRevision+1, stored.ConfigRevision)
	require.Equal(t, created.ProjectionRevision+1, stored.ProjectionRevision)
	require.Equal(t, 2, runtime.reloads)

	runtime.err = nil
	retried, err := svc.SetPublishGroup(ctx, created.ID, committed.ConfigRevision, true)
	require.NoError(t, err)
	require.True(t, retried.PublishGroup)
	require.Equal(t, committed.ConfigRevision+1, retried.ConfigRevision)
	require.Equal(t, committed.ProjectionRevision, retried.ProjectionRevision)
	require.Equal(t, 3, runtime.reloads)
	require.True(t, retried.Enabled, "publication setting must not enable or disable the platform")
}

func TestManagementSetPublishGroupDoesNotReloadDisabledPlatform(t *testing.T) {
	ctx := context.Background()
	store := newManagementStoreFake()
	runtime := &managementRuntimeFake{}
	svc := NewManagementService(store, nil, runtime, fakeClock{})
	input := validPlatformInput(nil)
	input.Enabled = false
	created, err := svc.Create(ctx, input)
	require.NoError(t, err)
	snapshot := store.projections[created.ID]
	snapshot.Nodes = []model.GbCascadeBusinessNode{{PlatformID: created.ID, Code: "34020000002150000001", Name: "业务根", Kind: "group"}}
	store.projections[created.ID] = snapshot

	updated, err := svc.SetPublishGroup(ctx, created.ID, created.ConfigRevision, true)
	require.NoError(t, err)
	require.True(t, updated.PublishGroup)
	require.False(t, updated.Enabled)
	require.Zero(t, runtime.reloads)
}

func TestManagementCreateRequiresBusinessRootBeforePublishing(t *testing.T) {
	store := newManagementStoreFake()
	svc := NewManagementService(store, nil, nil, fakeClock{})
	input := validPlatformInput(nil)
	input.Enabled = false
	input.PublishGroup = true
	_, err := svc.Create(context.Background(), input)
	require.Error(t, err)
	require.Contains(t, err.Error(), "业务分组")
	require.Empty(t, store.platforms)
}

func TestManagementUpdateRequiresBusinessRootBeforePublishing(t *testing.T) {
	store := newManagementStoreFake()
	svc := NewManagementService(store, nil, nil, fakeClock{})
	input := validPlatformInput(nil)
	input.Enabled = false
	created, err := svc.Create(context.Background(), input)
	require.NoError(t, err)
	input.PublishGroup = true
	_, err = svc.Update(context.Background(), created.ID, created.ConfigRevision, input)
	require.Error(t, err)
	require.Contains(t, err.Error(), "业务分组")
	stored, err := store.FindPlatform(context.Background(), created.ID)
	require.NoError(t, err)
	require.False(t, stored.PublishGroup)
}

func TestManagementUpdateRejectsInvalidBusinessRootAndNonSystemIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, localID string
		nodes         []model.GbCascadeBusinessNode
	}{
		{"organization alone", "34020000002000000001", []model.GbCascadeBusinessNode{{Code: "34020000002160000001", Kind: "organization"}}},
		{"wrong root type", "34020000002000000001", []model.GbCascadeBusinessNode{{Code: "34020000002160000001", Kind: "group"}}},
		{"two roots", "34020000002000000001", []model.GbCascadeBusinessNode{{Code: "34020000002150000001", Kind: "group"}, {Code: "34020000002150000002", Kind: "group"}}},
		{"non-system identity", "34020000001320000001", []model.GbCascadeBusinessNode{{Code: "34020000002150000001", Kind: "group"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newManagementStoreFake()
			svc := NewManagementService(store, nil, nil, fakeClock{})
			input := validPlatformInput(nil)
			input.Enabled = false
			input.LocalDeviceID = tc.localID
			created, err := svc.Create(context.Background(), input)
			require.NoError(t, err)
			snapshot := store.projections[created.ID]
			snapshot.Nodes = tc.nodes
			store.projections[created.ID] = snapshot
			input.PublishGroup = true
			_, err = svc.Update(context.Background(), created.ID, created.ConfigRevision, input)
			require.ErrorIs(t, err, ErrInvalidBusinessGroup)
		})
	}
}

func (s *managementStoreFake) ReplaceBusinessGroup(_ context.Context, platformID, revision uint64, nodes []repository.BusinessNodeInput, bindings []repository.BusinessBindingInput) error {
	platform, ok := s.platforms[platformID]
	if !ok {
		return repository.ErrPlatformNotFound
	}
	snapshot := s.projections[platformID]
	if snapshot.Revision != revision {
		return repository.ErrRevisionConflict
	}
	snapshot.Nodes = nil
	snapshot.Bindings = nil
	for _, node := range nodes {
		snapshot.Nodes = append(snapshot.Nodes, model.GbCascadeBusinessNode{PlatformID: platformID, Code: node.Code, Name: node.Name, Kind: node.Kind, ParentCode: node.ParentCode, Sort: node.Sort})
	}
	for _, binding := range bindings {
		snapshot.Bindings = append(snapshot.Bindings, model.GbCascadeBusinessBinding{PlatformID: platformID, SourceChannelID: binding.SourceChannelID, NodeCode: binding.NodeCode})
	}
	snapshot.Revision++
	platform.ProjectionRevision = snapshot.Revision
	snapshot.Platform = platform
	s.platforms[platformID] = platform
	s.projections[platformID] = snapshot
	return nil
}

func TestManagementBusinessGroupUsesProjectionRevisionAndKeepsShares(t *testing.T) {
	ctx := context.Background()
	store := newManagementStoreFake()
	svc := NewManagementService(store, nil, nil, fakeClock{})
	input := validPlatformInput(nil)
	input.Enabled = false
	created, err := svc.Create(ctx, input)
	require.NoError(t, err)
	require.NoError(t, svc.ReplaceProjection(ctx, created.ID, 0, []repository.DeviceProjectionInput{{SourceDeviceID: 1, PublishedDeviceID: "34020000001320000001"}}, []repository.ChannelProjectionInput{{SourceDeviceID: 1, SourceChannelID: 2, PublishedChannelID: "34020000001320000002"}}))
	nodes := []repository.BusinessNodeInput{{Code: "34020000002150000001", Name: "业务", Kind: "group"}, {Code: "34020000002160000001", Name: "东区", Kind: "organization", ParentCode: "34020000002150000001"}}
	require.NoError(t, svc.ReplaceBusinessGroup(ctx, created.ID, 1, nodes, []repository.BusinessBindingInput{{SourceChannelID: 2, NodeCode: nodes[1].Code}}))
	snapshot, err := svc.Projection(ctx, created.ID)
	require.NoError(t, err)
	require.Len(t, snapshot.Channels, 1)
	require.Len(t, snapshot.Bindings, 1)
	require.Len(t, snapshot.Nodes, 2)
	require.Equal(t, uint64(2), snapshot.Revision)
	require.ErrorIs(t, svc.ReplaceBusinessGroup(ctx, created.ID, 1, nodes, nil), repository.ErrRevisionConflict)
	input.PublishGroup = true
	_, err = svc.Update(ctx, created.ID, created.ConfigRevision, input)
	require.NoError(t, err)
}

func TestManagementBusinessGroupResourceChangesReloadAfterCommit(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name      string
		call      func(*ManagementService, *businessGroupResourceStoreFake, uint64) error
		wantCalls func(*businessGroupResourceStoreFake) int
	}{
		{
			name: "legacy shares",
			call: func(svc *ManagementService, store *businessGroupResourceStoreFake, id uint64) error {
				return svc.ReplaceBusinessGroupWithShares(ctx, id, 0, nil, nil, []uint64{11}, nil)
			},
			wantCalls: func(store *businessGroupResourceStoreFake) int { return store.shareCalls },
		},
		{
			name: "resource changes",
			call: func(svc *ManagementService, store *businessGroupResourceStoreFake, id uint64) error {
				return svc.ReplaceBusinessGroupWithResourceChanges(ctx, id, 0, nil, nil, nil, repository.BusinessGroupResourceChanges{ShareChannelIDs: []uint64{11}}, nil)
			},
			wantCalls: func(store *businessGroupResourceStoreFake) int { return store.resourceCalls },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &businessGroupResourceStoreFake{managementStoreFake: newManagementStoreFake()}
			runtime := &managementRuntimeFake{}
			svc := NewManagementService(store, nil, runtime, fakeClock{})
			input := validPlatformInput(nil)
			input.Enabled = false
			created, err := svc.Create(ctx, input)
			require.NoError(t, err)

			require.NoError(t, tc.call(svc, store, created.ID))
			require.Equal(t, 1, tc.wantCalls(store))
			require.Equal(t, 1, runtime.reloads)
			snapshot, err := svc.Projection(ctx, created.ID)
			require.NoError(t, err)
			require.EqualValues(t, 1, snapshot.Revision)
		})
	}
}

func TestManagementBusinessGroupResourceChangesReportsReloadFailureAfterCommit(t *testing.T) {
	ctx := context.Background()
	store := &businessGroupResourceStoreFake{managementStoreFake: newManagementStoreFake()}
	runtime := &managementRuntimeFake{err: errors.New("runtime reload unavailable")}
	svc := NewManagementService(store, nil, runtime, fakeClock{})
	input := validPlatformInput(nil)
	input.Enabled = false
	created, err := svc.Create(ctx, input)
	require.NoError(t, err)

	err = svc.ReplaceBusinessGroupWithResourceChanges(ctx, created.ID, 0, nil, nil, nil, repository.BusinessGroupResourceChanges{ShareChannelIDs: []uint64{11}}, nil)
	require.ErrorIs(t, err, ErrRuntimeSyncFailed)
	require.Equal(t, 1, runtime.reloads)
	snapshot, err := svc.Projection(ctx, created.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, snapshot.Revision, "database commit must survive runtime failure")
}

func TestManagementBusinessGroupResourceChangesDoesNotReloadWhenStoreFails(t *testing.T) {
	ctx := context.Background()
	store := &businessGroupResourceStoreFake{managementStoreFake: newManagementStoreFake()}
	store.resourceErr = repository.ErrRevisionConflict
	runtime := &managementRuntimeFake{}
	svc := NewManagementService(store, nil, runtime, fakeClock{})
	input := validPlatformInput(nil)
	input.Enabled = false
	created, err := svc.Create(ctx, input)
	require.NoError(t, err)

	err = svc.ReplaceBusinessGroupWithResourceChanges(ctx, created.ID, 0, nil, nil, nil, repository.BusinessGroupResourceChanges{ShareChannelIDs: []uint64{11}}, nil)
	require.ErrorIs(t, err, repository.ErrRevisionConflict)
	require.Zero(t, runtime.reloads)
	snapshot, err := svc.Projection(ctx, created.ID)
	require.NoError(t, err)
	require.Zero(t, snapshot.Revision)
}

type businessGroupResourceStoreFake struct {
	*managementStoreFake
	shareCalls    int
	resourceCalls int
	shareErr      error
	resourceErr   error
}

func (s *businessGroupResourceStoreFake) ReplaceBusinessGroupWithShares(ctx context.Context, platformID, revision uint64, nodes []repository.BusinessNodeInput, bindings []repository.BusinessBindingInput, _ []uint64, _ repository.BusinessGroupSourceAuthorizer) error {
	s.shareCalls++
	if s.shareErr != nil {
		return s.shareErr
	}
	return s.managementStoreFake.ReplaceBusinessGroup(ctx, platformID, revision, nodes, bindings)
}

func (s *businessGroupResourceStoreFake) ReplaceBusinessGroupWithResourceChanges(ctx context.Context, platformID, revision uint64, nodes []repository.BusinessNodeInput, bindings []repository.BusinessBindingInput, _ []uint64, _ repository.BusinessGroupResourceChanges, _ repository.BusinessGroupSourceAuthorizer) error {
	s.resourceCalls++
	if s.resourceErr != nil {
		return s.resourceErr
	}
	return s.managementStoreFake.ReplaceBusinessGroup(ctx, platformID, revision, nodes, bindings)
}
