package repository

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

func TestReplaceBusinessGroupWithResourceChangesUnsharesAndReusesProjection(t *testing.T) {
	repo := newTestRepository(t)
	require.NoError(t, repo.db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}))
	ctx := context.Background()
	platform := newPlatform("upstream-a", "34020000002000000001")
	platform.PTZEnabled = false
	require.NoError(t, repo.CreatePlatform(ctx, platform))
	device, channels := createBusinessGroupSource(t, repo, "34020000001320000011", "34020000001320000012")
	first, second := channels[0], channels[1]
	require.NoError(t, repo.ReplaceProjection(ctx, platform.ID, 0,
		[]DeviceProjectionInput{{SourceDeviceID: uint64(device.ID), PublishedDeviceID: device.DeviceID}},
		[]ChannelProjectionInput{
			{SourceDeviceID: uint64(device.ID), SourceChannelID: uint64(first.ID), PublishedChannelID: first.ChannelID},
			{SourceDeviceID: uint64(device.ID), SourceChannelID: uint64(second.ID), PublishedChannelID: second.ChannelID},
		},
	))
	root := "34020000002150000001"
	organization := "34020000002160000001"
	nodes := []BusinessNodeInput{
		{Code: root, Name: "业务根", Kind: "group"},
		{Code: organization, Name: "组织", Kind: "organization", ParentCode: root},
	}
	bindings := []BusinessBindingInput{{SourceChannelID: uint64(first.ID), NodeCode: organization}}
	require.NoError(t, repo.ReplaceBusinessGroup(ctx, platform.ID, 1, nodes, bindings))

	var firstProjection, secondProjection model.GbCascadeChannelProjection
	require.NoError(t, repo.db.Where("platform_id = ? AND source_channel_id = ?", platform.ID, first.ID).First(&firstProjection).Error)
	firstProjection.PTZAllowed = true
	require.NoError(t, repo.db.Save(&firstProjection).Error)
	changes := BusinessGroupResourceChanges{UnshareChannelIDs: []uint64{uint64(first.ID)}}
	var authorizedDevices, authorizedChannels []uint64
	authorize := func(_ *gorm.DB, devices, channels []uint64) error {
		authorizedDevices = append(authorizedDevices, devices...)
		authorizedChannels = append(authorizedChannels, channels...)
		return nil
	}
	require.NoError(t, repo.ReplaceBusinessGroupWithResourceChanges(ctx, platform.ID, 2, nodes, nil, nil, changes, authorize))
	require.Contains(t, authorizedDevices, uint64(device.ID))
	require.Equal(t, []uint64{uint64(first.ID)}, authorizedChannels)

	snapshot, err := repo.ProjectionSnapshot(ctx, platform.ID)
	require.NoError(t, err)
	require.EqualValues(t, 3, snapshot.Revision)
	require.Empty(t, snapshot.Bindings)
	require.Len(t, snapshot.Channels, 1)
	require.Equal(t, uint64(second.ID), snapshot.Channels[0].SourceChannelID)
	var inactiveChannel model.GbCascadeChannelProjection
	require.NoError(t, repo.db.Unscoped().Where("platform_id = ? AND source_channel_id = ?", platform.ID, first.ID).First(&inactiveChannel).Error)
	require.False(t, inactiveChannel.Active)
	require.Equal(t, firstProjection.PublishedChannelID, inactiveChannel.PublishedChannelID)
	var activeDevice model.GbCascadeDeviceProjection
	require.NoError(t, repo.db.Where("platform_id = ? AND source_device_id = ?", platform.ID, uint64(device.ID)).First(&activeDevice).Error)
	require.True(t, activeDevice.Active, "device remains shared while another channel projection is active")

	require.NoError(t, repo.ReplaceBusinessGroupWithResourceChanges(ctx, platform.ID, 3, nodes, nil, nil,
		BusinessGroupResourceChanges{ShareChannelIDs: []uint64{uint64(first.ID)}, ChannelPermissions: []BusinessChannelPermissionInput{{SourceChannelID: uint64(first.ID), PTZAllowed: true}}}, nil))
	snapshot, err = repo.ProjectionSnapshot(ctx, platform.ID)
	require.NoError(t, err)
	require.EqualValues(t, 4, snapshot.Revision)
	require.Len(t, snapshot.Channels, 2)
	require.True(t, snapshot.Channels[0].PTZAllowed, "explicit permission update overrides platform default")
	require.NoError(t, repo.db.Unscoped().Where("platform_id = ? AND source_channel_id = ?", platform.ID, first.ID).First(&secondProjection).Error)
	require.Equal(t, firstProjection.PublishedChannelID, secondProjection.PublishedChannelID)
}

func TestReplaceBusinessGroupWithResourceChangesDeactivateDeviceWhenLastChannelIsUnshared(t *testing.T) {
	repo := newTestRepository(t)
	require.NoError(t, repo.db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}))
	ctx := context.Background()
	platform := newPlatform("upstream-a", "34020000002000000001")
	require.NoError(t, repo.CreatePlatform(ctx, platform))
	device, channels := createBusinessGroupSource(t, repo, "34020000001320000011", "34020000001320000012")
	require.NoError(t, repo.ReplaceProjection(ctx, platform.ID, 0,
		[]DeviceProjectionInput{{SourceDeviceID: uint64(device.ID), PublishedDeviceID: device.DeviceID}},
		[]ChannelProjectionInput{{SourceDeviceID: uint64(device.ID), SourceChannelID: uint64(channels[0].ID), PublishedChannelID: channels[0].ChannelID}},
	))
	require.NoError(t, repo.ReplaceBusinessGroupWithResourceChanges(ctx, platform.ID, 1, nil, nil, nil,
		BusinessGroupResourceChanges{UnshareChannelIDs: []uint64{uint64(channels[0].ID)}}, nil))
	var stored model.GbCascadeDeviceProjection
	require.NoError(t, repo.db.Unscoped().Where("platform_id = ? AND source_device_id = ?", platform.ID, device.ID).First(&stored).Error)
	require.False(t, stored.Active)
}

func TestReplaceBusinessGroupWithResourceChangesRejectsInvalidChangesWithoutPartialWrites(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		changes BusinessGroupResourceChanges
	}{
		{name: "unknown unshare rolls back staged share", changes: BusinessGroupResourceChanges{ShareChannelIDs: []uint64{1}, UnshareChannelIDs: []uint64{999}}},
		{name: "add and remove conflict", changes: BusinessGroupResourceChanges{ShareChannelIDs: []uint64{1}, UnshareChannelIDs: []uint64{1}}},
		{name: "permission for unshared channel", changes: BusinessGroupResourceChanges{ChannelPermissions: []BusinessChannelPermissionInput{{SourceChannelID: 1, PTZAllowed: true}}}},
		{name: "duplicate permission", changes: BusinessGroupResourceChanges{ChannelPermissions: []BusinessChannelPermissionInput{{SourceChannelID: 1}, {SourceChannelID: 1}}}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			repo := newTestRepository(t)
			require.NoError(t, repo.db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}))
			ctx := context.Background()
			platform := newPlatform("upstream-a", "34020000002000000001")
			require.NoError(t, repo.CreatePlatform(ctx, platform))
			_, channels := createBusinessGroupSource(t, repo, "34020000001320000011")
			if testCase.name == "unknown unshare rolls back staged share" || testCase.name == "add and remove conflict" {
				testCase.changes.ShareChannelIDs = []uint64{uint64(channels[0].ID)}
			}
			if testCase.name == "add and remove conflict" {
				testCase.changes.UnshareChannelIDs = []uint64{uint64(channels[0].ID)}
			}
			if testCase.name == "permission for unshared channel" || testCase.name == "duplicate permission" {
				for index := range testCase.changes.ChannelPermissions {
					testCase.changes.ChannelPermissions[index].SourceChannelID = uint64(channels[0].ID)
				}
			}
			root := "34020000002150000001"
			nodes := []BusinessNodeInput{{Code: root, Name: "重复根", Kind: "group"}, {Code: root, Name: "重复根2", Kind: "group"}}
			err := repo.ReplaceBusinessGroupWithResourceChanges(ctx, platform.ID, 0, nodes, nil, nil, testCase.changes, nil)
			require.ErrorIs(t, err, ErrInvalidProjection)
			snapshot, snapshotErr := repo.ProjectionSnapshot(ctx, platform.ID)
			require.NoError(t, snapshotErr)
			require.Zero(t, snapshot.Revision)
			require.Empty(t, snapshot.Devices)
			require.Empty(t, snapshot.Channels)
			require.Empty(t, snapshot.Nodes)
			require.Empty(t, snapshot.Bindings)
		})
	}
}

func TestReplaceBusinessGroupWithResourceChangesRollsBackNewShareWhenTreeIsInvalid(t *testing.T) {
	repo := newTestRepository(t)
	require.NoError(t, repo.db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}))
	ctx := context.Background()
	platform := newPlatform("upstream-a", "34020000002000000001")
	require.NoError(t, repo.CreatePlatform(ctx, platform))
	_, channels := createBusinessGroupSource(t, repo, "34020000001320000011")
	duplicateRoot := "34020000002150000001"
	err := repo.ReplaceBusinessGroupWithResourceChanges(ctx, platform.ID, 0,
		[]BusinessNodeInput{{Code: duplicateRoot, Name: "根", Kind: "group"}, {Code: duplicateRoot, Name: "重复根", Kind: "group"}}, nil, nil,
		BusinessGroupResourceChanges{ShareChannelIDs: []uint64{uint64(channels[0].ID)}}, nil)
	require.ErrorIs(t, err, ErrInvalidProjection)
	snapshot, err := repo.ProjectionSnapshot(ctx, platform.ID)
	require.NoError(t, err)
	require.Zero(t, snapshot.Revision)
	require.Empty(t, snapshot.Devices)
	require.Empty(t, snapshot.Channels)
	require.Empty(t, snapshot.Nodes)
	require.Empty(t, snapshot.Bindings)
}

func TestReplaceBusinessGroupWithResourceChangesRejectsStaleRevisionBeforeMutation(t *testing.T) {
	repo := newTestRepository(t)
	require.NoError(t, repo.db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}))
	ctx := context.Background()
	platform := newPlatform("upstream-a", "34020000002000000001")
	require.NoError(t, repo.CreatePlatform(ctx, platform))
	_, channels := createBusinessGroupSource(t, repo, "34020000001320000011")
	err := repo.ReplaceBusinessGroupWithResourceChanges(ctx, platform.ID, 1, nil, nil, nil,
		BusinessGroupResourceChanges{ShareChannelIDs: []uint64{uint64(channels[0].ID)}}, nil)
	require.ErrorIs(t, err, ErrRevisionConflict)
	snapshot, err := repo.ProjectionSnapshot(ctx, platform.ID)
	require.NoError(t, err)
	require.Zero(t, snapshot.Revision)
	require.Empty(t, snapshot.Devices)
	require.Empty(t, snapshot.Channels)
}

func TestReplaceBusinessGroupWithResourceChangesRollsBackUnshareWhenTreeIsInvalid(t *testing.T) {
	repo := newTestRepository(t)
	require.NoError(t, repo.db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}))
	ctx := context.Background()
	platform := newPlatform("upstream-a", "34020000002000000001")
	require.NoError(t, repo.CreatePlatform(ctx, platform))
	device, channels := createBusinessGroupSource(t, repo, "34020000001320000011")
	channel := channels[0]
	require.NoError(t, repo.ReplaceProjection(ctx, platform.ID, 0,
		[]DeviceProjectionInput{{SourceDeviceID: uint64(device.ID), PublishedDeviceID: device.DeviceID}},
		[]ChannelProjectionInput{{SourceDeviceID: uint64(device.ID), SourceChannelID: uint64(channel.ID), PublishedChannelID: channel.ChannelID}},
	))
	root := "34020000002150000001"
	organization := "34020000002160000001"
	validNodes := []BusinessNodeInput{{Code: root, Name: "根", Kind: "group"}, {Code: organization, Name: "组织", Kind: "organization", ParentCode: root}}
	require.NoError(t, repo.ReplaceBusinessGroup(ctx, platform.ID, 1, validNodes, []BusinessBindingInput{{SourceChannelID: uint64(channel.ID), NodeCode: organization}}))
	invalidNodes := []BusinessNodeInput{{Code: root, Name: "重复根", Kind: "group"}, {Code: root, Name: "重复根2", Kind: "group"}}
	err := repo.ReplaceBusinessGroupWithResourceChanges(ctx, platform.ID, 2, invalidNodes,
		[]BusinessBindingInput{{SourceChannelID: uint64(channel.ID), NodeCode: organization}}, nil,
		BusinessGroupResourceChanges{UnshareChannelIDs: []uint64{uint64(channel.ID)}}, nil)
	require.ErrorIs(t, err, ErrInvalidProjection)
	snapshot, err := repo.ProjectionSnapshot(ctx, platform.ID)
	require.NoError(t, err)
	require.EqualValues(t, 2, snapshot.Revision)
	require.Len(t, snapshot.Channels, 1)
	require.Len(t, snapshot.Bindings, 1)
	require.Equal(t, root, snapshot.Nodes[0].Code)
}

func TestReplaceBusinessGroupWithResourceChangesPublishesDirectoryAtomically(t *testing.T) {
	repo := newTestRepository(t)
	require.NoError(t, repo.db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}))
	ctx := context.Background()
	platform := newPlatform("directory-publish", "34020000002000000001")
	require.NoError(t, repo.CreatePlatform(ctx, platform))
	_, channels := createBusinessGroupSource(t, repo, "34020000001320000011", "34020000001320000012")
	root := "34020000002150000001"
	organization := "34020000002160000001"
	nodes := []BusinessNodeInput{
		{Code: root, Name: "业务根", Kind: "group"},
		{Code: organization, Name: "东区", Kind: "organization", ParentCode: root},
	}
	bindings := []BusinessBindingInput{
		{SourceChannelID: uint64(channels[0].ID), NodeCode: organization},
		{SourceChannelID: uint64(channels[1].ID), NodeCode: organization},
	}
	publish := true
	modeAuthorizerCalls := 0
	err := repo.ReplaceBusinessGroupWithOptions(ctx, platform.ID, 0, nodes, bindings, nil,
		BusinessGroupResourceChanges{ShareChannelIDs: []uint64{uint64(channels[0].ID), uint64(channels[1].ID)}},
		BusinessGroupPublicationUpdate{PublishGroup: &publish, ExpectedConfigRevision: platform.ConfigRevision},
		nil, func() error { modeAuthorizerCalls++; return nil })
	require.NoError(t, err)
	require.Equal(t, 1, modeAuthorizerCalls)

	stored, err := repo.FindPlatform(ctx, platform.ID)
	require.NoError(t, err)
	require.True(t, stored.PublishGroup)
	require.EqualValues(t, platform.ConfigRevision+1, stored.ConfigRevision)
	require.EqualValues(t, 1, stored.ProjectionRevision)
	snapshot, err := repo.ProjectionSnapshot(ctx, platform.ID)
	require.NoError(t, err)
	require.Len(t, snapshot.Channels, 2)
	require.Len(t, snapshot.Nodes, 2)
	require.Len(t, snapshot.Bindings, 2)
}

func TestReplaceBusinessGroupWithResourceChangesRejectsMissingDirectoryButAllowsUnownedSharedChannels(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		nodes     []BusinessNodeInput
		bindings  []BusinessBindingInput
		wantError bool
	}{
		{name: "missing tree", wantError: true},
		{
			name: "unbound shared channel is retained",
			nodes: []BusinessNodeInput{
				{Code: "34020000002150000001", Name: "业务根", Kind: "group"},
				{Code: "34020000002160000001", Name: "东区", Kind: "organization", ParentCode: "34020000002150000001"},
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			repo := newTestRepository(t)
			require.NoError(t, repo.db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}))
			ctx := context.Background()
			platform := newPlatform("directory-invalid", "34020000002000000001")
			require.NoError(t, repo.CreatePlatform(ctx, platform))
			_, channels := createBusinessGroupSource(t, repo, "34020000001320000011")
			publish := true
			err := repo.ReplaceBusinessGroupWithOptions(ctx, platform.ID, 0, testCase.nodes, testCase.bindings, nil,
				BusinessGroupResourceChanges{ShareChannelIDs: []uint64{uint64(channels[0].ID)}},
				BusinessGroupPublicationUpdate{PublishGroup: &publish, ExpectedConfigRevision: platform.ConfigRevision}, nil, func() error { return nil })
			if !testCase.wantError {
				require.NoError(t, err)
				stored, findErr := repo.FindPlatform(ctx, platform.ID)
				require.NoError(t, findErr)
				require.True(t, stored.PublishGroup)
				snapshot, snapshotErr := repo.ProjectionSnapshot(ctx, platform.ID)
				require.NoError(t, snapshotErr)
				require.Len(t, snapshot.Channels, 1)
				require.Empty(t, snapshot.Bindings)
				return
			}
			require.ErrorIs(t, err, ErrInvalidProjection)
			stored, findErr := repo.FindPlatform(ctx, platform.ID)
			require.NoError(t, findErr)
			require.False(t, stored.PublishGroup)
			require.EqualValues(t, platform.ConfigRevision, stored.ConfigRevision)
			require.Zero(t, stored.ProjectionRevision)
			snapshot, snapshotErr := repo.ProjectionSnapshot(ctx, platform.ID)
			require.NoError(t, snapshotErr)
			require.Empty(t, snapshot.Channels)
			require.Empty(t, snapshot.Nodes)
			require.Empty(t, snapshot.Bindings)
		})
	}
}

func TestPublishedBusinessGroupRemovingBindingsRetainsSharedChannels(t *testing.T) {
	for _, removeCount := range []int{1, 10} {
		t.Run(fmt.Sprintf("remove_%d", removeCount), func(t *testing.T) {
			repo := newTestRepository(t)
			require.NoError(t, repo.db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}))
			ctx := context.Background()
			platform := newPlatform("directory-move-out", "34020000002000000001")
			require.NoError(t, repo.CreatePlatform(ctx, platform))
			codes := make([]string, 10)
			for i := range codes {
				codes[i] = fmt.Sprintf("340200000013200000%02d", 11+i)
			}
			_, channels := createBusinessGroupSource(t, repo, codes...)
			root := "34020000002150000001"
			organization := "34020000002160000001"
			nodes := []BusinessNodeInput{
				{Code: root, Name: "业务根", Kind: "group"},
				{Code: organization, Name: "组织", Kind: "organization", ParentCode: root},
			}
			ids := make([]uint64, len(channels))
			bindings := make([]BusinessBindingInput, len(channels))
			for i, channel := range channels {
				ids[i] = uint64(channel.ID)
				bindings[i] = BusinessBindingInput{SourceChannelID: ids[i], NodeCode: organization}
			}
			publish := true
			require.NoError(t, repo.ReplaceBusinessGroupWithOptions(ctx, platform.ID, 0, nodes, bindings, nil,
				BusinessGroupResourceChanges{ShareChannelIDs: ids},
				BusinessGroupPublicationUpdate{PublishGroup: &publish, ExpectedConfigRevision: platform.ConfigRevision}, nil, nil))
			before, err := repo.ProjectionSnapshot(ctx, platform.ID)
			require.NoError(t, err)
			require.NoError(t, repo.ReplaceBusinessGroupWithOptions(ctx, platform.ID, before.Revision, nodes, bindings[removeCount:], nil,
				BusinessGroupResourceChanges{}, BusinessGroupPublicationUpdate{PublishGroup: &publish}, nil, nil))
			after, err := repo.ProjectionSnapshot(ctx, platform.ID)
			require.NoError(t, err)
			require.True(t, after.Platform.PublishGroup)
			require.Equal(t, before.Platform.ConfigRevision, after.Platform.ConfigRevision)
			require.Equal(t, before.Revision+1, after.Revision)
			require.Equal(t, before.Channels, after.Channels)
			require.Len(t, after.Nodes, len(before.Nodes))
			for i := range before.Nodes {
				expected := before.Nodes[i]
				expected.ID = after.Nodes[i].ID
				require.Equal(t, expected, after.Nodes[i])
			}
			require.Len(t, after.Bindings, len(channels)-removeCount)
			for _, binding := range after.Bindings {
				require.Contains(t, ids[removeCount:], binding.SourceChannelID)
				require.Equal(t, organization, binding.NodeCode)
			}
		})
	}
}

func TestReplaceBusinessGroupModeCASAndSameModeDoNotNeedConfigRevision(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	platform := newPlatform("directory-mode-cas", "34020000002000000001")
	require.NoError(t, repo.CreatePlatform(ctx, platform))
	root := "34020000002150000001"
	organization := "34020000002160000001"
	nodes := []BusinessNodeInput{
		{Code: root, Name: "业务根", Kind: "group"},
		{Code: organization, Name: "东区", Kind: "organization", ParentCode: root},
	}
	publish := true
	var deniedCalls int
	err := repo.ReplaceBusinessGroupWithOptions(ctx, platform.ID, 0, nodes, nil, nil,
		BusinessGroupResourceChanges{}, BusinessGroupPublicationUpdate{PublishGroup: &publish, ExpectedConfigRevision: platform.ConfigRevision + 1}, nil,
		func() error { deniedCalls++; return nil })
	require.ErrorIs(t, err, ErrRevisionConflict)
	require.Zero(t, deniedCalls, "stale mode revision must fail before authorization or writes")

	err = repo.ReplaceBusinessGroupWithOptions(ctx, platform.ID, 0, nodes, nil, nil,
		BusinessGroupResourceChanges{}, BusinessGroupPublicationUpdate{PublishGroup: &publish, ExpectedConfigRevision: platform.ConfigRevision}, nil,
		func() error { deniedCalls++; return nil })
	require.NoError(t, err)
	require.Equal(t, 1, deniedCalls)

	unchanged := true
	var unexpectedAuthorization int
	err = repo.ReplaceBusinessGroupWithOptions(ctx, platform.ID, 1, nodes, nil, nil,
		BusinessGroupResourceChanges{}, BusinessGroupPublicationUpdate{PublishGroup: &unchanged, ExpectedConfigRevision: 1}, nil,
		func() error { unexpectedAuthorization++; return nil })
	require.NoError(t, err, "same mode must not require a fresh config revision")
	require.Zero(t, unexpectedAuthorization)
	stored, err := repo.FindPlatform(ctx, platform.ID)
	require.NoError(t, err)
	require.True(t, stored.PublishGroup)
	require.EqualValues(t, 2, stored.ConfigRevision)
	require.EqualValues(t, 2, stored.ProjectionRevision)
}

func TestSwitchingToChannelModeRetainsBusinessTreeAndBindings(t *testing.T) {
	repo := newTestRepository(t)
	require.NoError(t, repo.db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}))
	ctx := context.Background()
	platform := newPlatform("directory-to-channel", "34020000002000000001")
	require.NoError(t, repo.CreatePlatform(ctx, platform))
	device, channels := createBusinessGroupSource(t, repo, "34020000001320000011")
	require.NoError(t, repo.ReplaceProjection(ctx, platform.ID, 0,
		[]DeviceProjectionInput{{SourceDeviceID: uint64(device.ID), PublishedDeviceID: device.DeviceID}},
		[]ChannelProjectionInput{{SourceDeviceID: uint64(device.ID), SourceChannelID: uint64(channels[0].ID), PublishedChannelID: channels[0].ChannelID}},
	))
	root := "34020000002150000001"
	organization := "34020000002160000001"
	nodes := []BusinessNodeInput{
		{Code: root, Name: "业务根", Kind: "group"},
		{Code: organization, Name: "东区", Kind: "organization", ParentCode: root},
	}
	bindings := []BusinessBindingInput{{SourceChannelID: uint64(channels[0].ID), NodeCode: organization}}
	require.NoError(t, repo.ReplaceBusinessGroup(ctx, platform.ID, 1, nodes, bindings))
	platform, err := repo.SetPublishGroup(ctx, platform.ID, platform.ConfigRevision, true)
	require.NoError(t, err)
	unpublish := false
	require.NoError(t, repo.ReplaceBusinessGroupWithOptions(ctx, platform.ID, platform.ProjectionRevision, nodes, bindings, nil,
		BusinessGroupResourceChanges{}, BusinessGroupPublicationUpdate{PublishGroup: &unpublish, ExpectedConfigRevision: platform.ConfigRevision}, nil,
		func() error { return nil }))

	stored, err := repo.FindPlatform(ctx, platform.ID)
	require.NoError(t, err)
	require.False(t, stored.PublishGroup)
	require.EqualValues(t, 3, stored.ConfigRevision)
	snapshot, err := repo.ProjectionSnapshot(ctx, platform.ID)
	require.NoError(t, err)
	require.EqualValues(t, 4, snapshot.Revision)
	require.Len(t, snapshot.Nodes, 2)
	require.Len(t, snapshot.Bindings, 1)
}

func TestReplaceProjectionAllowsUnassignedSharesWhileRetainingPublishedDirectory(t *testing.T) {
	repo := newTestRepository(t)
	require.NoError(t, repo.db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}))
	ctx := context.Background()
	platform := newPlatform("directory-projection-guard", "34020000002000000001")
	require.NoError(t, repo.CreatePlatform(ctx, platform))
	device, channels := createBusinessGroupSource(t, repo, "34020000001320000011", "34020000001320000012")
	require.NoError(t, repo.ReplaceProjection(ctx, platform.ID, 0,
		[]DeviceProjectionInput{{SourceDeviceID: uint64(device.ID), PublishedDeviceID: device.DeviceID}},
		[]ChannelProjectionInput{{SourceDeviceID: uint64(device.ID), SourceChannelID: uint64(channels[0].ID), PublishedChannelID: channels[0].ChannelID}},
	))
	root := "34020000002150000001"
	organization := "34020000002160000001"
	require.NoError(t, repo.ReplaceBusinessGroup(ctx, platform.ID, 1,
		[]BusinessNodeInput{{Code: root, Name: "业务根", Kind: "group"}, {Code: organization, Name: "东区", Kind: "organization", ParentCode: root}},
		[]BusinessBindingInput{{SourceChannelID: uint64(channels[0].ID), NodeCode: organization}},
	))
	platform, err := repo.SetPublishGroup(ctx, platform.ID, platform.ConfigRevision, true)
	require.NoError(t, err)

	err = repo.ReplaceProjection(ctx, platform.ID, platform.ProjectionRevision,
		[]DeviceProjectionInput{{SourceDeviceID: uint64(device.ID), PublishedDeviceID: device.DeviceID}},
		[]ChannelProjectionInput{{SourceDeviceID: uint64(device.ID), SourceChannelID: uint64(channels[1].ID), PublishedChannelID: channels[1].ChannelID}},
	)
	require.NoError(t, err)
	snapshot, err := repo.ProjectionSnapshot(ctx, platform.ID)
	require.NoError(t, err)
	require.EqualValues(t, platform.ProjectionRevision+1, snapshot.Revision)
	require.Len(t, snapshot.Channels, 1)
	require.Equal(t, uint64(channels[1].ID), snapshot.Channels[0].SourceChannelID)
	require.True(t, snapshot.Platform.PublishGroup)
	require.Len(t, snapshot.Nodes, 2)
	require.Empty(t, snapshot.Bindings)
}
