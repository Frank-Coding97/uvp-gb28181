package catalog

import (
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/repository"
)

func TestBuildSnapshotKeepsChannelAuthorizationPlatformIsolationAndReverseLookup(t *testing.T) {
	platformA := model.GbCascadePlatform{ID: 1, LocalDeviceID: "34020000002000000001", ProjectionRevision: 7}
	platformB := model.GbCascadePlatform{ID: 2, LocalDeviceID: "34020000002000000002", ProjectionRevision: 9}
	deviceA := model.GbCascadeDeviceProjection{ID: 11, PlatformID: platformA.ID, SourceDeviceID: 101, PublishedDeviceID: "34020000001320000001", Name: "A device", Manufacturer: "allowed", Model: "M1", Owner: "owner", CivilCode: "340200", Address: "A address", Parental: 1, Secrecy: 0, Active: true}
	deviceB := model.GbCascadeDeviceProjection{ID: 12, PlatformID: platformB.ID, SourceDeviceID: 101, PublishedDeviceID: "34020000001320000002", Active: true}
	channelA := model.GbCascadeChannelProjection{ID: 21, PlatformID: platformA.ID, DeviceProjectionID: deviceA.ID, SourceChannelID: 201, PublishedChannelID: "34020000001320000011", Name: "A channel", Active: true}
	channelB := model.GbCascadeChannelProjection{ID: 22, PlatformID: platformB.ID, DeviceProjectionID: deviceB.ID, SourceChannelID: 201, PublishedChannelID: "34020000001320000012", Active: true}

	snapshotA, err := Build(repository.ProjectionSnapshot{
		Platform: platformA,
		Devices:  []model.GbCascadeDeviceProjection{deviceA},
		Channels: []model.GbCascadeChannelProjection{channelA},
		Revision: platformA.ProjectionRevision,
	}, SourceFacts{DeviceOnline: map[uint64]bool{101: true}, ChannelOnline: map[uint64]bool{201: true}})
	require.NoError(t, err)
	require.Equal(t, uint64(7), snapshotA.Revision)
	require.Equal(t, 2, snapshotA.SumNum)
	require.Equal(t, []CatalogItem{
		{ID: deviceA.PublishedDeviceID, Kind: CatalogItemDevice, Name: "A device", Manufacturer: "allowed", Model: "M1", Owner: "owner", CivilCode: "340200", Address: "A address", Parental: 1, Secrecy: 0, Status: ResourceStatusOnline},
		{ID: channelA.PublishedChannelID, ParentID: deviceA.PublishedDeviceID, Kind: CatalogItemChannel, Name: "A channel", Status: ResourceStatusOnline},
	}, snapshotA.Items)

	target, ok := snapshotA.LookupPublishedChannel(channelA.PublishedChannelID)
	require.True(t, ok)
	require.Equal(t, uint64(101), target.SourceDeviceID)
	require.Equal(t, uint64(201), target.SourceChannelID)
	_, ok = snapshotA.LookupPublishedChannel(channelB.PublishedChannelID)
	require.False(t, ok)

	snapshotB, err := Build(repository.ProjectionSnapshot{
		Platform: platformB,
		Devices:  []model.GbCascadeDeviceProjection{deviceB},
		Channels: []model.GbCascadeChannelProjection{channelB},
		Revision: platformB.ProjectionRevision,
	}, SourceFacts{DeviceOnline: map[uint64]bool{101: true}, ChannelOnline: map[uint64]bool{201: true}})
	require.NoError(t, err)
	require.Equal(t, []CatalogItem{deviceItem(deviceB.PublishedDeviceID), channelItem(channelB.PublishedChannelID, deviceB.PublishedDeviceID)}, snapshotB.Items)
}

func TestBuildSnapshotEmptyAndOfflineResources(t *testing.T) {
	empty, err := Build(repository.ProjectionSnapshot{Platform: model.GbCascadePlatform{ID: 1, ProjectionRevision: 3}, Revision: 3}, SourceFacts{})
	require.NoError(t, err)
	require.Equal(t, uint64(3), empty.Revision)
	require.Zero(t, empty.SumNum)
	require.Empty(t, empty.Items)

	// A device row is supporting metadata, not an independent authorization.
	// Without an active channel projection, it must not leak into Catalog.
	orphan, err := Build(repository.ProjectionSnapshot{
		Platform: model.GbCascadePlatform{ID: 1, ProjectionRevision: 3},
		Devices:  []model.GbCascadeDeviceProjection{{ID: 1, PlatformID: 1, SourceDeviceID: 10, PublishedDeviceID: "34020000001320000001", Active: true}},
		Revision: 3,
	}, SourceFacts{})
	require.NoError(t, err)
	require.Zero(t, orphan.SumNum)
	require.Empty(t, orphan.Items)

	device := model.GbCascadeDeviceProjection{ID: 1, PlatformID: 1, SourceDeviceID: 10, PublishedDeviceID: "34020000001320000001", Active: true}
	channel := model.GbCascadeChannelProjection{PlatformID: 1, DeviceProjectionID: 1, SourceChannelID: 20, PublishedChannelID: "34020000001320000011", Active: true}
	offline, err := Build(repository.ProjectionSnapshot{Platform: model.GbCascadePlatform{ID: 1, ProjectionRevision: 4}, Devices: []model.GbCascadeDeviceProjection{device}, Channels: []model.GbCascadeChannelProjection{channel}, Revision: 4}, SourceFacts{DeviceOnline: map[uint64]bool{10: false}, ChannelOnline: map[uint64]bool{20: true}})
	require.NoError(t, err)
	require.Equal(t, 2, offline.SumNum)
	require.Equal(t, ResourceStatusOffline, offline.Items[0].Status)
	require.Equal(t, ResourceStatusOffline, offline.Items[1].Status)
}

func TestBuildSnapshotReparentsOnlyToPublishedNodesAndIsRevisionStable(t *testing.T) {
	platform := model.GbCascadePlatform{ID: 1, LocalDeviceID: "34020000002000000001", PublishPlatform: true, ProjectionRevision: 8}
	device := model.GbCascadeDeviceProjection{ID: 1, PlatformID: 1, SourceDeviceID: 10, PublishedDeviceID: "34020000001320000001", Active: true}
	channel := model.GbCascadeChannelProjection{PlatformID: 1, DeviceProjectionID: 1, SourceChannelID: 20, PublishedChannelID: "34020000001320000011", ParentOverride: "34020000001329999999", Active: true}

	snapshot, err := Build(repository.ProjectionSnapshot{Platform: platform, Devices: []model.GbCascadeDeviceProjection{device}, Channels: []model.GbCascadeChannelProjection{channel}, Revision: 8}, SourceFacts{DeviceOnline: map[uint64]bool{10: true}, ChannelOnline: map[uint64]bool{20: true}})
	require.NoError(t, err)
	require.Equal(t, uint64(8), snapshot.Revision)
	require.Equal(t, []CatalogItem{
		{ID: platform.LocalDeviceID, Kind: CatalogItemPlatform, Name: platform.Name, Status: ResourceStatusOnline},
		{ID: device.PublishedDeviceID, ParentID: platform.LocalDeviceID, Kind: CatalogItemDevice, Status: ResourceStatusOnline},
		{ID: channel.PublishedChannelID, ParentID: device.PublishedDeviceID, Kind: CatalogItemChannel, Status: ResourceStatusOnline},
	}, snapshot.Items)

	for _, item := range snapshot.Items {
		if item.ParentID != "" {
			require.Truef(t, snapshot.HasPublishedID(item.ParentID), "dangling parent %q", item.ParentID)
		}
	}
}

func TestBuildSnapshotRejectsInvalidOrAmbiguousPublishedIDs(t *testing.T) {
	platform := model.GbCascadePlatform{ID: 1, ProjectionRevision: 1}
	invalidDevice := model.GbCascadeDeviceProjection{ID: 1, PlatformID: 1, SourceDeviceID: 10, PublishedDeviceID: "not-a-gb-id", Active: true}
	_, err := Build(repository.ProjectionSnapshot{Platform: platform, Devices: []model.GbCascadeDeviceProjection{invalidDevice}, Revision: 1}, SourceFacts{})
	require.ErrorIs(t, err, ErrInvalidPublishedID)

	device := model.GbCascadeDeviceProjection{ID: 1, PlatformID: 1, SourceDeviceID: 10, PublishedDeviceID: "34020000001320000001", Active: true}
	collision := model.GbCascadeChannelProjection{PlatformID: 1, DeviceProjectionID: 1, SourceChannelID: 20, PublishedChannelID: device.PublishedDeviceID, Active: true}
	_, err = Build(repository.ProjectionSnapshot{Platform: platform, Devices: []model.GbCascadeDeviceProjection{device}, Channels: []model.GbCascadeChannelProjection{collision}, Revision: 1}, SourceFacts{})
	require.ErrorIs(t, err, ErrInvalidPublishedID)
}

func deviceItem(id string) CatalogItem {
	return CatalogItem{ID: id, Kind: CatalogItemDevice, Status: ResourceStatusOnline}
}

func channelItem(id, parentID string) CatalogItem {
	return CatalogItem{ID: id, ParentID: parentID, Kind: CatalogItemChannel, Status: ResourceStatusOnline}
}

func TestChannelsOnlyOmitsDeviceNodeAndRetainsSourceOwnership(t *testing.T) {
	original := Snapshot{Items: []CatalogItem{
		{ID: "device", Kind: CatalogItemDevice, Parental: 1},
		{ID: "one", Kind: CatalogItemChannel, ParentID: "device"},
		{ID: "two", Kind: CatalogItemChannel, ParentID: "device"},
		{ID: "three", Kind: CatalogItemChannel, ParentID: "device"},
	}, channels: map[string]PublishedChannelTarget{"one": {PlatformID: 1, SourceDeviceID: 10, SourceChannelID: 20}}}
	flat := original.ChannelsOnly()
	require.Equal(t, 3, flat.SumNum)
	require.Len(t, flat.Items, 3)
	for _, item := range flat.Items {
		require.Empty(t, item.ParentID)
		require.Zero(t, item.Parental)
		require.Equal(t, CatalogItemChannel, item.Kind)
	}
	require.False(t, flat.HasPublishedID("device"))
	target, ok := flat.LookupPublishedChannel("one")
	require.True(t, ok)
	require.Equal(t, uint64(10), target.SourceDeviceID)
	require.Len(t, original.Items, 4)
}
