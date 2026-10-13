package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
)

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
