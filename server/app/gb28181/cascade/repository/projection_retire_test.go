package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/model"
)

// 设备从设备列表删掉、之后再重新接入时,gb_device 拿到的是一条新的自增 id,
// 而 published_device_id 仍然是同一个国标号。只要旧那条投影还在(哪怕 active=false),
// 新设备共享给上级时就会撞 uk_cascade_device_published 报 1062,接口只能回 409。
// 删除事务里回收投影之后,这个号位才会真正空出来。
func TestRetireDeletedSourcesFreesPublishedIDForTheReconnectedDevice(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	platform := newPlatform("upstream-a", "34020000001320000001")
	require.NoError(t, repo.CreatePlatform(ctx, platform))

	const deviceCode = "37010301021180000007"
	const channelCode = "37010301021320000007"

	// 设备第一次接入:gb_device 自增 id = 1,共享给上级。
	require.NoError(t, repo.ReplaceProjection(ctx, platform.ID, 0,
		[]DeviceProjectionInput{{SourceDeviceID: 1, PublishedDeviceID: deviceCode}},
		[]ChannelProjectionInput{{SourceDeviceID: 1, SourceChannelID: 11, PublishedChannelID: channelCode}},
	))

	// 运维把它从设备列表删了 -> 删除事务里回收共享投影。
	retired, err := RetireDeletedSources(repo.db, []uint64{1}, []uint64{11})
	require.NoError(t, err)
	require.EqualValues(t, 2, retired, "设备投影一行 + 通道投影一行")

	snapshot, err := repo.ProjectionSnapshot(ctx, platform.ID)
	require.NoError(t, err)
	require.Empty(t, snapshot.Devices)
	require.Empty(t, snapshot.Channels)
	require.EqualValues(t, 2, snapshot.Revision, "回收要推进修订号:旧快照的提交应当拿到冲突而不是静默重建授权")

	// 设备重新接入:自增 id 变成 9,国标号还是同一个。
	require.NoError(t, repo.ReplaceProjection(ctx, platform.ID, 2,
		[]DeviceProjectionInput{{SourceDeviceID: 9, PublishedDeviceID: deviceCode}},
		[]ChannelProjectionInput{{SourceDeviceID: 9, SourceChannelID: 91, PublishedChannelID: channelCode}},
	))
	snapshot, err = repo.ProjectionSnapshot(ctx, platform.ID)
	require.NoError(t, err)
	require.Len(t, snapshot.Devices, 1)
	require.EqualValues(t, 9, snapshot.Devices[0].SourceDeviceID)
	require.Equal(t, deviceCode, snapshot.Devices[0].PublishedDeviceID)
	require.Len(t, snapshot.Channels, 1)
	require.Equal(t, channelCode, snapshot.Channels[0].PublishedChannelID)
}

// 回收只针对被删的那一份源:同平台其它设备/通道的投影一行都不能少。
func TestRetireDeletedSourcesRemovesOnlyTheDeletedSourcesProjections(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	platform := newPlatform("upstream-a", "34020000001320000001")
	require.NoError(t, repo.CreatePlatform(ctx, platform))
	require.NoError(t, repo.ReplaceProjection(ctx, platform.ID, 0,
		[]DeviceProjectionInput{
			{SourceDeviceID: 1, PublishedDeviceID: "34020000001180000001"},
			{SourceDeviceID: 2, PublishedDeviceID: "34020000001180000002"},
		},
		[]ChannelProjectionInput{
			{SourceDeviceID: 1, SourceChannelID: 11, PublishedChannelID: "34020000001320000011"},
			{SourceDeviceID: 1, SourceChannelID: 12, PublishedChannelID: "34020000001320000012"},
			{SourceDeviceID: 2, SourceChannelID: 21, PublishedChannelID: "34020000001320000021"},
		},
	))

	retired, err := RetireDeletedSources(repo.db, []uint64{1}, []uint64{11, 12})
	require.NoError(t, err)
	require.EqualValues(t, 3, retired)

	snapshot, err := repo.ProjectionSnapshot(ctx, platform.ID)
	require.NoError(t, err)
	require.Len(t, snapshot.Devices, 1)
	require.EqualValues(t, 2, snapshot.Devices[0].SourceDeviceID)
	require.Len(t, snapshot.Channels, 1)
	require.EqualValues(t, 21, snapshot.Channels[0].SourceChannelID)
	require.EqualValues(t, 2, snapshot.Revision)

	// 必须是物理删除:软删的行仍然占着 (platform_id, published_*) 唯一键位,等于没回收。
	var devices, channels int64
	require.NoError(t, repo.db.Unscoped().Model(&model.GbCascadeDeviceProjection{}).
		Where("source_device_id = ?", 1).Count(&devices).Error)
	require.Zero(t, devices)
	require.NoError(t, repo.db.Unscoped().Model(&model.GbCascadeChannelProjection{}).
		Where("source_channel_id IN ?", []uint64{11, 12}).Count(&channels).Error)
	require.Zero(t, channels)
}

// 单独删一路通道(不动设备)时只回收那一路的投影,设备投影要留着。
func TestRetireDeletedSourcesRetiresASingleChannelOnly(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	platform := newPlatform("upstream-a", "34020000001320000001")
	require.NoError(t, repo.CreatePlatform(ctx, platform))
	require.NoError(t, repo.ReplaceProjection(ctx, platform.ID, 0,
		[]DeviceProjectionInput{{SourceDeviceID: 1, PublishedDeviceID: "34020000001180000001"}},
		[]ChannelProjectionInput{
			{SourceDeviceID: 1, SourceChannelID: 11, PublishedChannelID: "34020000001320000011"},
			{SourceDeviceID: 1, SourceChannelID: 12, PublishedChannelID: "34020000001320000012"},
		},
	))

	retired, err := RetireDeletedSources(repo.db, nil, []uint64{11})
	require.NoError(t, err)
	require.EqualValues(t, 1, retired)

	snapshot, err := repo.ProjectionSnapshot(ctx, platform.ID)
	require.NoError(t, err)
	require.Len(t, snapshot.Devices, 1)
	require.Len(t, snapshot.Channels, 1)
	require.EqualValues(t, 12, snapshot.Channels[0].SourceChannelID)
}

// 传空参数必须是纯 no-op:这个函数被接在设备/通道删除事务里,
// 任何"顺手清理"都会变成误删。
func TestRetireDeletedSourcesIsANoOpWithoutSources(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	platform := newPlatform("upstream-a", "34020000001320000001")
	require.NoError(t, repo.CreatePlatform(ctx, platform))
	require.NoError(t, repo.ReplaceProjection(ctx, platform.ID, 0,
		[]DeviceProjectionInput{{SourceDeviceID: 1, PublishedDeviceID: "34020000001180000001"}},
		[]ChannelProjectionInput{{SourceDeviceID: 1, SourceChannelID: 11, PublishedChannelID: "34020000001320000011"}},
	))

	retired, err := RetireDeletedSources(repo.db, nil, nil)
	require.NoError(t, err)
	require.Zero(t, retired)

	snapshot, err := repo.ProjectionSnapshot(ctx, platform.ID)
	require.NoError(t, err)
	require.Len(t, snapshot.Devices, 1)
	require.Len(t, snapshot.Channels, 1)
	require.EqualValues(t, 1, snapshot.Revision, "no-op 不该推进修订号")
}

// 这次修复之前的库里留下的脏数据:设备被删了,但投影没被回收。
// 设备再用同一个国标号接回来时,upsert 必须认领那条历史行,而不是 INSERT 撞键。
func TestReplaceProjectionClaimsTheProjectionLeftByADeletedDevice(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	platform := newPlatform("upstream-a", "34020000001320000001")
	require.NoError(t, repo.CreatePlatform(ctx, platform))

	const deviceCode = "37010301021180000007"
	// gb_device 自增 id = 1 时的历史共享;此刻设备早已被硬删,投影却还留着。
	require.NoError(t, repo.ReplaceProjection(ctx, platform.ID, 0,
		[]DeviceProjectionInput{{SourceDeviceID: 1, PublishedDeviceID: deviceCode}}, nil))
	var before model.GbCascadeDeviceProjection
	require.NoError(t, repo.db.Unscoped().
		Where("platform_id = ? AND published_device_id = ?", platform.ID, deviceCode).First(&before).Error)

	// 设备重新接入,gb_device 变成新的自增 id = 9,同一个国标号。
	// 没有认领这条路,这里就是线上那个 1062 Duplicate entry -> 409。
	require.NoError(t, repo.ReplaceProjection(ctx, platform.ID, 1,
		[]DeviceProjectionInput{{SourceDeviceID: 9, PublishedDeviceID: deviceCode}}, nil))

	var after []model.GbCascadeDeviceProjection
	require.NoError(t, repo.db.Unscoped().
		Where("platform_id = ? AND published_device_id = ?", platform.ID, deviceCode).Find(&after).Error)
	require.Len(t, after, 1, "认领的是原来那一行,不该再多出一行")
	require.Equal(t, before.ID, after[0].ID)
	require.EqualValues(t, 9, after[0].SourceDeviceID)
	require.True(t, after[0].Active)
	require.EqualValues(t, before.Revision+1, after[0].Revision)

	// 认领后上级看到的还是同一个标识。
	snapshot, err := repo.ProjectionSnapshot(ctx, platform.ID)
	require.NoError(t, err)
	require.Len(t, snapshot.Devices, 1)
	require.Equal(t, deviceCode, snapshot.Devices[0].PublishedDeviceID)
}
