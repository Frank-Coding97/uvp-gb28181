package service

import (
	"context"
	"testing"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"

	"github.com/stretchr/testify/require"
)

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
