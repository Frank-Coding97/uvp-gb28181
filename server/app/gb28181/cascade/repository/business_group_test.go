package repository

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

func TestReplaceBusinessGroupWithSharesUsesSourceRowsAndPreservesExistingProjection(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	require.NoError(t, repo.db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}))
	platform := newPlatform("upstream-a", "34020000002000000001")
	platform.PTZEnabled = true
	require.NoError(t, repo.CreatePlatform(ctx, platform))
	device, channels := createBusinessGroupSource(t, repo, "34020000001320000011", "34020000001320000012")
	first, second := channels[0], channels[1]

	existingDevice := model.GbCascadeDeviceProjection{
		PlatformID: platform.ID, SourceDeviceID: uint64(device.ID), PublishedDeviceID: "34020000001320000101",
		Name: "保留设备名称", Manufacturer: "保留厂商", Model: "保留型号", Active: true, Revision: 7,
	}
	require.NoError(t, repo.db.Create(&existingDevice).Error)
	existingChannel := model.GbCascadeChannelProjection{
		PlatformID: platform.ID, DeviceProjectionID: existingDevice.ID, SourceChannelID: uint64(first.ID),
		PublishedChannelID: "34020000001320000111", Name: "保留通道名称", ParentOverride: "34020000002000000009",
		PTZAllowed: false, Active: true, Revision: 9,
	}
	require.NoError(t, repo.db.Create(&existingChannel).Error)

	root := "34020000002150000001"
	organization := "34020000002160000001"
	nodes := []BusinessNodeInput{
		{Code: root, Name: "业务根", Kind: "group"},
		{Code: organization, Name: "组织", Kind: "organization", ParentCode: root},
	}
	bindings := []BusinessBindingInput{
		{SourceChannelID: uint64(first.ID), NodeCode: organization},
		{SourceChannelID: uint64(second.ID), NodeCode: organization},
	}
	require.NoError(t, repo.ReplaceBusinessGroupWithShares(ctx, platform.ID, 0, nodes, bindings, []uint64{uint64(second.ID)}, nil))

	snapshot, err := repo.ProjectionSnapshot(ctx, platform.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, snapshot.Revision)
	require.Len(t, snapshot.Devices, 1)
	require.Len(t, snapshot.Channels, 2)
	require.Len(t, snapshot.Bindings, 2)
	require.Equal(t, existingDevice.ID, snapshot.Devices[0].ID)
	require.Equal(t, existingDevice.PublishedDeviceID, snapshot.Devices[0].PublishedDeviceID)
	require.Equal(t, existingDevice.Name, snapshot.Devices[0].Name)
	require.Equal(t, existingDevice.Manufacturer, snapshot.Devices[0].Manufacturer)
	require.Equal(t, existingDevice.Model, snapshot.Devices[0].Model)
	require.Equal(t, existingDevice.Revision, snapshot.Devices[0].Revision, "adding a channel on an already shared device must preserve its projection")

	var preserved model.GbCascadeChannelProjection
	require.NoError(t, repo.db.First(&preserved, existingChannel.ID).Error)
	require.Equal(t, existingChannel.DeviceProjectionID, preserved.DeviceProjectionID)
	require.Equal(t, existingChannel.PublishedChannelID, preserved.PublishedChannelID)
	require.Equal(t, existingChannel.Name, preserved.Name)
	require.Equal(t, existingChannel.ParentOverride, preserved.ParentOverride)
	require.Equal(t, existingChannel.PTZAllowed, preserved.PTZAllowed)
	require.Equal(t, existingChannel.Revision, preserved.Revision, "an already shared channel may only change its business binding")
	var added model.GbCascadeChannelProjection
	require.NoError(t, repo.db.Where("platform_id = ? AND source_channel_id = ?", platform.ID, second.ID).First(&added).Error)
	require.Equal(t, uint64(device.ID), added.DeviceProjectionID)
	require.Equal(t, second.ChannelID, added.PublishedChannelID)
	require.Equal(t, second.Name, added.Name)
	require.True(t, added.PTZAllowed)
	require.True(t, added.Active, "offline source rows remain eligible for sharing")
}

func TestReplaceBusinessGroupWithSharesRollsBackOnInvalidTreeAndAuthorization(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		authorize   bool
		invalidTree bool
	}{
		{name: "authorization denied"},
		{name: "invalid tree rolls back staged share", authorize: true, invalidTree: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			repo := newTestRepository(t)
			ctx := context.Background()
			require.NoError(t, repo.db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}))
			platform := newPlatform("upstream-a", "34020000002000000001")
			require.NoError(t, repo.CreatePlatform(ctx, platform))
			_, channels := createBusinessGroupSource(t, repo, "34020000001320000011", "34020000001320000012")
			channel := channels[1]
			root := "34020000002150000001"
			organization := "34020000002160000001"
			nodes := []BusinessNodeInput{{Code: root, Name: "业务根", Kind: "group"}, {Code: organization, Name: "组织", Kind: "organization", ParentCode: root}}
			if testCase.invalidTree {
				nodes = append(nodes, BusinessNodeInput{Code: root, Name: "重复根", Kind: "group"})
			}
			authorizer := func(_ *gorm.DB, _, _ []uint64) error {
				if testCase.authorize {
					return nil
				}
				return errors.New("source access denied")
			}
			err := repo.ReplaceBusinessGroupWithShares(ctx, platform.ID, 0, nodes,
				[]BusinessBindingInput{{SourceChannelID: uint64(channel.ID), NodeCode: organization}}, []uint64{uint64(channel.ID)}, authorizer)
			require.Error(t, err)
			var deviceCount, channelCount, nodeCount, bindingCount int64
			require.NoError(t, repo.db.Model(&model.GbCascadeDeviceProjection{}).Where("platform_id = ?", platform.ID).Count(&deviceCount).Error)
			require.NoError(t, repo.db.Model(&model.GbCascadeChannelProjection{}).Where("platform_id = ?", platform.ID).Count(&channelCount).Error)
			require.NoError(t, repo.db.Model(&model.GbCascadeBusinessNode{}).Where("platform_id = ?", platform.ID).Count(&nodeCount).Error)
			require.NoError(t, repo.db.Model(&model.GbCascadeBusinessBinding{}).Where("platform_id = ?", platform.ID).Count(&bindingCount).Error)
			require.Zero(t, deviceCount)
			require.Zero(t, channelCount)
			require.Zero(t, nodeCount)
			require.Zero(t, bindingCount)
			stored, findErr := repo.FindPlatform(ctx, platform.ID)
			require.NoError(t, findErr)
			require.Zero(t, stored.ProjectionRevision)
		})
	}
}

func TestReplaceBusinessGroupWithSharesRejectsStaleRevisionAndUnboundSelection(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	require.NoError(t, repo.db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}))
	platform := newPlatform("upstream-a", "34020000002000000001")
	require.NoError(t, repo.CreatePlatform(ctx, platform))
	_, channels := createBusinessGroupSource(t, repo, "34020000001320000011", "34020000001320000012")
	channel := channels[1]
	root := "34020000002150000001"
	organization := "34020000002160000001"
	nodes := []BusinessNodeInput{{Code: root, Name: "业务根", Kind: "group"}, {Code: organization, Name: "组织", Kind: "organization", ParentCode: root}}
	authorizer := func(_ *gorm.DB, _, _ []uint64) error { return nil }

	err := repo.ReplaceBusinessGroupWithShares(ctx, platform.ID, 1, nodes,
		[]BusinessBindingInput{{SourceChannelID: uint64(channel.ID), NodeCode: organization}}, []uint64{uint64(channel.ID)}, authorizer)
	require.ErrorIs(t, err, ErrRevisionConflict)
	err = repo.ReplaceBusinessGroupWithShares(ctx, platform.ID, 0, nodes, nil, []uint64{uint64(channel.ID)}, authorizer)
	require.ErrorIs(t, err, ErrInvalidProjection)
	var deviceCount, channelCount int64
	require.NoError(t, repo.db.Model(&model.GbCascadeDeviceProjection{}).Where("platform_id = ?", platform.ID).Count(&deviceCount).Error)
	require.NoError(t, repo.db.Model(&model.GbCascadeChannelProjection{}).Where("platform_id = ?", platform.ID).Count(&channelCount).Error)
	require.Zero(t, deviceCount)
	require.Zero(t, channelCount)
}

func createBusinessGroupSource(t *testing.T, repo *GormRepository, channelIDs ...string) (gbmodels.GbDevice, []gbmodels.GbChannel) {
	t.Helper()
	device := gbmodels.GbDevice{DeviceID: "34020000001320000001", Name: "源设备", Manufacturer: "源厂商", Model: "源型号", Status: 0, OwnerDeptID: 10}
	require.NoError(t, repo.db.Create(&device).Error)
	channels := make([]gbmodels.GbChannel, 0, len(channelIDs))
	for index, channelID := range channelIDs {
		channel := gbmodels.GbChannel{DeviceID: device.DeviceID, ChannelID: channelID, Name: "源通道" + string(rune('A'+index)), Status: 0, OwnerDeptID: 10}
		require.NoError(t, repo.db.Create(&channel).Error)
		channels = append(channels, channel)
	}
	return device, channels
}

func TestReplaceBusinessGroupStoresPlatformScopedTreeAndBindings(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	platformA := newPlatform("upstream-a", "34020000001320000001")
	platformB := newPlatform("upstream-b", "34020000001320000002")
	require.NoError(t, repo.CreatePlatform(ctx, platformA))
	require.NoError(t, repo.CreatePlatform(ctx, platformB))
	require.NoError(t, repo.ReplaceProjection(ctx, platformA.ID, 0,
		[]DeviceProjectionInput{{SourceDeviceID: 1, PublishedDeviceID: "34020000001180000001"}},
		[]ChannelProjectionInput{{SourceDeviceID: 1, SourceChannelID: 11, PublishedChannelID: "34020000001320000011"}},
	))

	nodes := []BusinessNodeInput{
		{Code: "34020000002150000001", Name: " 业务根 ", Kind: "group", Sort: 1},
		{Code: "34020000002160000001", Name: "组织一", Kind: "organization", ParentCode: "34020000002150000001", Sort: 2},
		{Code: "34020000002160000002", Name: "子组织", Kind: "organization", ParentCode: "34020000002160000001", Sort: 3},
	}
	bindings := []BusinessBindingInput{{SourceChannelID: 11, NodeCode: "34020000002160000002"}}
	require.NoError(t, repo.ReplaceBusinessGroup(ctx, platformA.ID, 1, nodes, bindings))

	snapshot, err := repo.ProjectionSnapshot(ctx, platformA.ID)
	require.NoError(t, err)
	require.EqualValues(t, 2, snapshot.Revision)
	require.Len(t, snapshot.Nodes, 3)
	require.Equal(t, "业务根", snapshot.Nodes[0].Name, "存储名称应清除首尾空格")
	require.Equal(t, "34020000002160000002", snapshot.Bindings[0].NodeCode)
	require.EqualValues(t, 11, snapshot.Bindings[0].SourceChannelID)

	other, err := repo.ProjectionSnapshot(ctx, platformB.ID)
	require.NoError(t, err)
	require.Empty(t, other.Nodes)
	require.Empty(t, other.Bindings)
	require.Zero(t, other.Revision)
}

func TestReplaceBusinessGroupRejectsInvalidTreesWithoutPartialWrites(t *testing.T) {
	testCases := []struct {
		name     string
		nodes    []BusinessNodeInput
		bindings []BusinessBindingInput
		conflict string
	}{
		{name: "bad root code", nodes: []BusinessNodeInput{{Code: "34020000002160000001", Name: "根", Kind: "group"}}},
		{name: "wrong organization code", nodes: []BusinessNodeInput{{Code: "34020000002150000001", Name: "根", Kind: "group"}, {Code: "34020000002150000002", Name: "组织", Kind: "organization", ParentCode: "34020000002150000001"}}},
		{name: "duplicate roots", nodes: []BusinessNodeInput{{Code: "34020000002150000001", Name: "根一", Kind: "group"}, {Code: "34020000002150000002", Name: "根二", Kind: "group"}}},
		{name: "orphan", nodes: []BusinessNodeInput{{Code: "34020000002150000001", Name: "根", Kind: "group"}, {Code: "34020000002160000001", Name: "组织", Kind: "organization", ParentCode: "34020000002160000099"}}},
		{name: "self cycle", nodes: []BusinessNodeInput{{Code: "34020000002150000001", Name: "根", Kind: "group"}, {Code: "34020000002160000001", Name: "组织", Kind: "organization", ParentCode: "34020000002160000001"}}},
		{name: "cycle", nodes: []BusinessNodeInput{{Code: "34020000002150000001", Name: "根", Kind: "group"}, {Code: "34020000002160000001", Name: "组织一", Kind: "organization", ParentCode: "34020000002160000002"}, {Code: "34020000002160000002", Name: "组织二", Kind: "organization", ParentCode: "34020000002160000001"}}},
		{name: "blank name", nodes: []BusinessNodeInput{{Code: "34020000002150000001", Name: "  \t", Kind: "group"}}},
		{name: "long name", nodes: []BusinessNodeInput{{Code: "34020000002150000001", Name: strings.Repeat("界", 129), Kind: "group"}}},
		{name: "reserved platform id", nodes: []BusinessNodeInput{{Code: "34020000002150000001", Name: "根", Kind: "group"}}, conflict: "platform"},
		{name: "active device collision", nodes: []BusinessNodeInput{{Code: "34020000002150000001", Name: "根", Kind: "group"}}, conflict: "device"},
		{name: "active channel collision", nodes: []BusinessNodeInput{{Code: "34020000002150000001", Name: "根", Kind: "group"}}, conflict: "channel"},
		{name: "bind root", nodes: []BusinessNodeInput{{Code: "34020000002150000001", Name: "根", Kind: "group"}}, bindings: []BusinessBindingInput{{SourceChannelID: 11, NodeCode: "34020000002150000001"}}},
		{name: "bind unshared channel", nodes: []BusinessNodeInput{{Code: "34020000002150000001", Name: "根", Kind: "group"}, {Code: "34020000002160000001", Name: "组织", Kind: "organization", ParentCode: "34020000002150000001"}}, bindings: []BusinessBindingInput{{SourceChannelID: 99, NodeCode: "34020000002160000001"}}},
		{name: "duplicate channel binding", nodes: []BusinessNodeInput{{Code: "34020000002150000001", Name: "根", Kind: "group"}, {Code: "34020000002160000001", Name: "组织一", Kind: "organization", ParentCode: "34020000002150000001"}, {Code: "34020000002160000002", Name: "组织二", Kind: "organization", ParentCode: "34020000002150000001"}}, bindings: []BusinessBindingInput{{SourceChannelID: 11, NodeCode: "34020000002160000001"}, {SourceChannelID: 11, NodeCode: "34020000002160000002"}}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			repo := newTestRepository(t)
			ctx := context.Background()
			platform := newPlatform("upstream-a", "34020000001320000001")
			deviceCode := "34020000001180000001"
			channelCode := "34020000001320000011"
			switch testCase.conflict {
			case "platform":
				platform.UpstreamServerID = "34020000002150000001"
			case "device":
				deviceCode = "34020000002150000001"
			case "channel":
				channelCode = "34020000002150000001"
			}
			require.NoError(t, repo.CreatePlatform(ctx, platform))
			require.NoError(t, repo.ReplaceProjection(ctx, platform.ID, 0,
				[]DeviceProjectionInput{{SourceDeviceID: 1, PublishedDeviceID: deviceCode}},
				[]ChannelProjectionInput{{SourceDeviceID: 1, SourceChannelID: 11, PublishedChannelID: channelCode}},
			))
			err := repo.ReplaceBusinessGroup(ctx, platform.ID, 1, testCase.nodes, testCase.bindings)
			require.ErrorIs(t, err, ErrInvalidProjection)
			require.NotEqual(t, ErrInvalidProjection.Error(), err.Error(), "校验错误必须附带可读原因")
			snapshot, snapshotErr := repo.ProjectionSnapshot(ctx, platform.ID)
			require.NoError(t, snapshotErr)
			require.Empty(t, snapshot.Nodes)
			require.Empty(t, snapshot.Bindings)
			require.EqualValues(t, 1, snapshot.Revision, "校验失败不得推进共享修订号")
		})
	}
}

func TestReplaceProjectionRejectsCodesThatConflictWithBusinessNodes(t *testing.T) {
	testCases := []struct {
		name        string
		deviceCode  string
		channelID   uint64
		channelCode string
	}{
		{name: "device", deviceCode: "34020000002150000001", channelID: 11, channelCode: "34020000001320000011"},
		{name: "channel", deviceCode: "34020000001180000001", channelID: 12, channelCode: "34020000002160000001"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			repo := newTestRepository(t)
			ctx := context.Background()
			platform := newPlatform("upstream-a", "34020000001320000001")
			require.NoError(t, repo.CreatePlatform(ctx, platform))
			require.NoError(t, repo.ReplaceProjection(ctx, platform.ID, 0,
				[]DeviceProjectionInput{{SourceDeviceID: 1, PublishedDeviceID: "34020000001180000001"}},
				[]ChannelProjectionInput{{SourceDeviceID: 1, SourceChannelID: 11, PublishedChannelID: "34020000001320000011"}},
			))
			require.NoError(t, repo.ReplaceBusinessGroup(ctx, platform.ID, 1, []BusinessNodeInput{
				{Code: "34020000002150000001", Name: "根", Kind: "group"},
				{Code: "34020000002160000001", Name: "组织", Kind: "organization", ParentCode: "34020000002150000001"},
			}, nil))

			err := repo.ReplaceProjection(ctx, platform.ID, 2,
				[]DeviceProjectionInput{{SourceDeviceID: 1, PublishedDeviceID: testCase.deviceCode}},
				[]ChannelProjectionInput{{SourceDeviceID: 1, SourceChannelID: testCase.channelID, PublishedChannelID: testCase.channelCode}},
			)
			require.ErrorIs(t, err, ErrInvalidProjection)
			require.Contains(t, err.Error(), "业务分组")

			snapshot, snapshotErr := repo.ProjectionSnapshot(ctx, platform.ID)
			require.NoError(t, snapshotErr)
			require.EqualValues(t, 2, snapshot.Revision, "编号冲突不得推进共享修订号")
			require.Len(t, snapshot.Nodes, 2)
			require.Equal(t, "34020000001180000001", snapshot.Devices[0].PublishedDeviceID)
			require.Equal(t, "34020000001320000011", snapshot.Channels[0].PublishedChannelID)
		})
	}
}

func TestReplaceBusinessGroupUsesProjectionRevisionCASAndRejectsStaleSnapshots(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	platform := newPlatform("upstream-a", "34020000001320000001")
	require.NoError(t, repo.CreatePlatform(ctx, platform))
	require.NoError(t, repo.ReplaceProjection(ctx, platform.ID, 0, nil, nil))
	nodes := []BusinessNodeInput{{Code: "34020000002150000001", Name: "根", Kind: "group"}}
	require.NoError(t, repo.ReplaceBusinessGroup(ctx, platform.ID, 1, nodes, nil))
	require.ErrorIs(t, repo.ReplaceProjection(ctx, platform.ID, 1, nil, nil), ErrRevisionConflict)
	require.ErrorIs(t, repo.ReplaceBusinessGroup(ctx, platform.ID, 1, nodes, nil), ErrRevisionConflict)

	snapshot, err := repo.ProjectionSnapshot(ctx, platform.ID)
	require.NoError(t, err)
	require.EqualValues(t, 2, snapshot.Revision)
	require.Len(t, snapshot.Nodes, 1)
}

func TestReplaceBusinessGroupRequiresContentsMovedBeforeDeletingANode(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	platform := newPlatform("upstream-a", "34020000001320000001")
	require.NoError(t, repo.CreatePlatform(ctx, platform))
	require.NoError(t, repo.ReplaceProjection(ctx, platform.ID, 0,
		[]DeviceProjectionInput{{SourceDeviceID: 1, PublishedDeviceID: "34020000001180000001"}},
		[]ChannelProjectionInput{{SourceDeviceID: 1, SourceChannelID: 11, PublishedChannelID: "34020000001320000011"}},
	))
	root := "34020000002150000001"
	oldOrg := "34020000002160000001"
	newOrg := "34020000002160000002"
	childOrg := "34020000002160000003"
	initialNodes := []BusinessNodeInput{
		{Code: root, Name: "根", Kind: "group"},
		{Code: oldOrg, Name: "旧组织", Kind: "organization", ParentCode: root},
		{Code: childOrg, Name: "子组织", Kind: "organization", ParentCode: oldOrg},
	}
	require.NoError(t, repo.ReplaceBusinessGroup(ctx, platform.ID, 1, initialNodes,
		[]BusinessBindingInput{{SourceChannelID: 11, NodeCode: oldOrg}}))

	err := repo.ReplaceBusinessGroup(ctx, platform.ID, 2, []BusinessNodeInput{{Code: root, Name: "根", Kind: "group"}}, nil)
	require.ErrorIs(t, err, ErrInvalidProjection)
	require.Contains(t, err.Error(), "移动或删除")

	movedNodes := []BusinessNodeInput{
		{Code: root, Name: "根", Kind: "group"},
		{Code: newOrg, Name: "新组织", Kind: "organization", ParentCode: root},
		{Code: childOrg, Name: "子组织", Kind: "organization", ParentCode: newOrg},
	}
	require.NoError(t, repo.ReplaceBusinessGroup(ctx, platform.ID, 2, movedNodes,
		[]BusinessBindingInput{{SourceChannelID: 11, NodeCode: newOrg}}))
}

func TestPublishGroupRequiresSystemIdentityAndBusinessRootAndCannotClearTreeWhileEnabled(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	platform := newPlatform("upstream-a", "34020000001320000001")
	require.NoError(t, repo.CreatePlatform(ctx, platform))

	platform.PublishGroup = true
	_, err := repo.UpdatePlatformConfig(ctx, platform, platform.ConfigRevision)
	require.ErrorIs(t, err, ErrInvalidProjection)
	require.Contains(t, err.Error(), "200")
	stored, err := repo.FindPlatform(ctx, platform.ID)
	require.NoError(t, err)
	require.False(t, stored.PublishGroup)

	platform.LocalDeviceID = "34020000002000000001"
	platform.PublishGroup = false
	platform, err = repo.UpdatePlatformConfig(ctx, platform, platform.ConfigRevision)
	require.NoError(t, err)
	platform.PublishGroup = true
	_, err = repo.UpdatePlatformConfig(ctx, platform, platform.ConfigRevision)
	require.ErrorIs(t, err, ErrInvalidProjection)
	require.Contains(t, err.Error(), "根节点")

	require.NoError(t, repo.ReplaceBusinessGroup(ctx, platform.ID, platform.ProjectionRevision,
		[]BusinessNodeInput{{Code: "34020000002150000001", Name: "根", Kind: "group"}}, nil))
	updated, err := repo.FindPlatform(ctx, platform.ID)
	require.NoError(t, err)
	require.EqualValues(t, 2, updated.ProjectionRevision)
	updated.PublishGroup = true
	updated, err = repo.UpdatePlatformConfig(ctx, updated, updated.ConfigRevision)
	require.NoError(t, err)
	require.True(t, updated.PublishGroup)
	require.EqualValues(t, 3, updated.ProjectionRevision, "发布开关变更与目录保存共用投影修订号")

	err = repo.ReplaceBusinessGroup(ctx, platform.ID, updated.ProjectionRevision, nil, nil)
	require.ErrorIs(t, err, ErrInvalidProjection)
	require.Contains(t, err.Error(), "先关闭发布")

	updated.PublishGroup = false
	updated, err = repo.UpdatePlatformConfig(ctx, updated, updated.ConfigRevision)
	require.NoError(t, err)
	require.False(t, updated.PublishGroup)
	require.EqualValues(t, 4, updated.ProjectionRevision)
	require.NoError(t, repo.ReplaceBusinessGroup(ctx, platform.ID, updated.ProjectionRevision, nil, nil))
}

func TestReplaceProjectionCancellationRemovesOnlyMatchingBusinessBindings(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	platformA := newPlatform("upstream-a", "34020000001320000001")
	platformB := newPlatform("upstream-b", "34020000001320000002")
	require.NoError(t, repo.CreatePlatform(ctx, platformA))
	require.NoError(t, repo.CreatePlatform(ctx, platformB))
	require.NoError(t, repo.ReplaceProjection(ctx, platformA.ID, 0,
		[]DeviceProjectionInput{{SourceDeviceID: 1, PublishedDeviceID: "34020000001180000001"}},
		[]ChannelProjectionInput{{SourceDeviceID: 1, SourceChannelID: 11, PublishedChannelID: "34020000001320000011"}, {SourceDeviceID: 1, SourceChannelID: 12, PublishedChannelID: "34020000001320000012"}},
	))
	require.NoError(t, repo.ReplaceProjection(ctx, platformB.ID, 0,
		[]DeviceProjectionInput{{SourceDeviceID: 1, PublishedDeviceID: "34020000001180000002"}},
		[]ChannelProjectionInput{{SourceDeviceID: 1, SourceChannelID: 11, PublishedChannelID: "34020000001320000021"}},
	))
	nodesA := []BusinessNodeInput{{Code: "34020000002150000001", Name: "根A", Kind: "group"}, {Code: "34020000002160000001", Name: "组织A", Kind: "organization", ParentCode: "34020000002150000001"}}
	nodesB := []BusinessNodeInput{{Code: "34020000002150000002", Name: "根B", Kind: "group"}, {Code: "34020000002160000002", Name: "组织B", Kind: "organization", ParentCode: "34020000002150000002"}}
	require.NoError(t, repo.ReplaceBusinessGroup(ctx, platformA.ID, 1, nodesA, []BusinessBindingInput{{SourceChannelID: 11, NodeCode: nodesA[1].Code}, {SourceChannelID: 12, NodeCode: nodesA[1].Code}}))
	require.NoError(t, repo.ReplaceBusinessGroup(ctx, platformB.ID, 1, nodesB, []BusinessBindingInput{{SourceChannelID: 11, NodeCode: nodesB[1].Code}}))

	require.NoError(t, repo.ReplaceProjection(ctx, platformA.ID, 2,
		[]DeviceProjectionInput{{SourceDeviceID: 1, PublishedDeviceID: "34020000001180000001"}},
		[]ChannelProjectionInput{{SourceDeviceID: 1, SourceChannelID: 12, PublishedChannelID: "34020000001320000012"}},
	))

	snapshotA, err := repo.ProjectionSnapshot(ctx, platformA.ID)
	require.NoError(t, err)
	require.Len(t, snapshotA.Nodes, 2, "取消共享不应删除业务节点")
	require.Len(t, snapshotA.Bindings, 1)
	require.EqualValues(t, 12, snapshotA.Bindings[0].SourceChannelID)
	snapshotB, err := repo.ProjectionSnapshot(ctx, platformB.ID)
	require.NoError(t, err)
	require.Len(t, snapshotB.Bindings, 1, "另一上级的共享和业务归属不应被影响")
	require.EqualValues(t, 11, snapshotB.Bindings[0].SourceChannelID)
}

func TestRetireDeletedSourcesRemovesBusinessBindings(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	platformA := newPlatform("upstream-a", "34020000001320000001")
	platformB := newPlatform("upstream-b", "34020000001320000002")
	require.NoError(t, repo.CreatePlatform(ctx, platformA))
	require.NoError(t, repo.CreatePlatform(ctx, platformB))
	for _, platform := range []*model.GbCascadePlatform{platformA, platformB} {
		require.NoError(t, repo.ReplaceProjection(ctx, platform.ID, 0,
			[]DeviceProjectionInput{{SourceDeviceID: 1, PublishedDeviceID: "34020000001180000001"}},
			[]ChannelProjectionInput{{SourceDeviceID: 1, SourceChannelID: 11, PublishedChannelID: "34020000001320000011"}},
		))
		root := "34020000002150000001"
		org := "34020000002160000001"
		require.NoError(t, repo.ReplaceBusinessGroup(ctx, platform.ID, 1,
			[]BusinessNodeInput{{Code: root, Name: "根", Kind: "group"}, {Code: org, Name: "组织", Kind: "organization", ParentCode: root}},
			[]BusinessBindingInput{{SourceChannelID: 11, NodeCode: org}},
		))
	}

	retired, err := RetireDeletedSources(repo.db, nil, []uint64{11})
	require.NoError(t, err)
	require.EqualValues(t, 2, retired)
	for _, platform := range []*model.GbCascadePlatform{platformA, platformB} {
		snapshot, snapshotErr := repo.ProjectionSnapshot(ctx, platform.ID)
		require.NoError(t, snapshotErr)
		require.Empty(t, snapshot.Channels)
		require.Empty(t, snapshot.Bindings)
		require.Len(t, snapshot.Nodes, 2, "源资源删除只解除挂载，不删除平台业务树")
		require.EqualValues(t, 3, snapshot.Revision)
	}
}

func TestSoftDeleteAndResurrectPlatformDoesNotRestoreBusinessTree(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	platform := newPlatform("upstream-a", "34020000001320000001")
	require.NoError(t, repo.CreatePlatform(ctx, platform))
	require.NoError(t, repo.ReplaceProjection(ctx, platform.ID, 0,
		[]DeviceProjectionInput{{SourceDeviceID: 1, PublishedDeviceID: "34020000001180000001"}},
		[]ChannelProjectionInput{{SourceDeviceID: 1, SourceChannelID: 11, PublishedChannelID: "34020000001320000011"}},
	))
	require.NoError(t, repo.ReplaceBusinessGroup(ctx, platform.ID, 1,
		[]BusinessNodeInput{{Code: "34020000002150000001", Name: "根", Kind: "group"}, {Code: "34020000002160000001", Name: "组织", Kind: "organization", ParentCode: "34020000002150000001"}},
		[]BusinessBindingInput{{SourceChannelID: 11, NodeCode: "34020000002160000001"}},
	))
	require.NoError(t, repo.SoftDeletePlatform(ctx, platform.ID))

	var nodeCount, bindingCount int64
	require.NoError(t, repo.db.Model(&model.GbCascadeBusinessNode{}).Where("platform_id = ?", platform.ID).Count(&nodeCount).Error)
	require.NoError(t, repo.db.Model(&model.GbCascadeBusinessBinding{}).Where("platform_id = ?", platform.ID).Count(&bindingCount).Error)
	require.Zero(t, nodeCount)
	require.Zero(t, bindingCount)

	reborn := newPlatform("upstream-a", "34020000001320000001")
	require.NoError(t, repo.CreatePlatform(ctx, reborn))
	require.Equal(t, platform.ID, reborn.ID)
	snapshot, err := repo.ProjectionSnapshot(ctx, reborn.ID)
	require.NoError(t, err)
	require.Empty(t, snapshot.Nodes)
	require.Empty(t, snapshot.Bindings)
}
