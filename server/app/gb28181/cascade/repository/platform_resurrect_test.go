package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
)

// 设备列表里"删了再建"那条路径的级联版本:平台被删掉之后,底下那条软删行仍然
// 占着 uk_cascade_platform_name / uk_cascade_platform_connection 两个键位,
// 于是重建同名平台直接撞 1062。CreatePlatform 应当接手遗留行。
func TestCreatePlatformReclaimsTheRowLeftByADeletedPlatform(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	first := newPlatform("upstream-a", "34020000001320000001")
	require.NoError(t, repo.CreatePlatform(ctx, first))
	registeredAt := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	require.NoError(t, repo.RecordRegistrationSuccess(ctx, first.ID, registeredAt, registeredAt.Add(time.Hour)))
	require.NoError(t, repo.RecordHeartbeatFailure(ctx, first.ID, "timeout", "keepalive timeout", registeredAt))
	require.NoError(t, repo.SoftDeletePlatform(ctx, first.ID))

	// 同名、同接入关系重建 —— 用户视角就是"我把它删了,重新建一个"。
	second := newPlatform("upstream-a", "34020000001320000001")
	second.Host = "198.51.100.77"
	require.NoError(t, repo.CreatePlatform(ctx, second),
		"软删的平台行不该把同名同接入关系的新建挡在门外")
	require.Equal(t, first.ID, second.ID, "应当接手遗留行,而不是新增一行")

	stored, err := repo.FindPlatform(ctx, second.ID)
	require.NoError(t, err, "接手后这行必须不再是软删状态")
	require.Equal(t, "198.51.100.77", stored.Host, "配置字段用新值覆盖")
	require.Equal(t, second.ConfigRevision, stored.ConfigRevision)
	require.True(t, second.CreatedAt.Equal(stored.CreatedAt), "创建时间用这一次新建的时间")

	require.Nil(t, stored.RegisterAt, "运行时事实属于已被删掉的平台,必须清空")
	require.Nil(t, stored.RegisterExpiresAt)
	require.Nil(t, stored.HeartbeatAt)
	require.Empty(t, stored.LastErrorCode)
	require.Empty(t, stored.LastErrorMessage)
	require.Nil(t, stored.LastErrorAt)

	platforms, err := repo.ListPlatforms(ctx)
	require.NoError(t, err)
	require.Len(t, platforms, 1, "接手而不是新增,列表仍然只有一条")

	var retired int64
	require.NoError(t, repo.db.Unscoped().Model(&model.GbCascadePlatform{}).
		Where("deleted_at IS NOT NULL").Count(&retired).Error)
	require.Zero(t, retired, "deleted_at 必须被清回 NULL")
}

// 名称键位被占、接入关系键位空缺时,也应当按接入关系接手。
func TestCreatePlatformReclaimsTheConnectionLeftByADeletedPlatform(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	first := newPlatform("upstream-a", "34020000001320000001")
	require.NoError(t, repo.CreatePlatform(ctx, first))
	require.NoError(t, repo.SoftDeletePlatform(ctx, first.ID))

	// 接入关系完全相同、只是换了个名字:约束 uk_cascade_platform_connection 会拦。
	second := newPlatform("upstream-a-renamed", "34020000001320000001")
	require.NoError(t, repo.CreatePlatform(ctx, second),
		"同接入关系的软删平台行同样要被接手")
	require.Equal(t, first.ID, second.ID)

	stored, err := repo.FindPlatform(ctx, second.ID)
	require.NoError(t, err)
	require.Equal(t, "upstream-a-renamed", stored.Name, "名称用新值覆盖")
}

// 名称与接入关系分别被两条不同的遗留行占着时(名称来自 A 行、接入关系来自 B 行,
// 两条都已删),必须接手接入关系那条、并把名称那条连残留子表一起真删 ——
// 键位腾不干净,用户就永远建不出他想要的那条平台(这两条行在列表里都看不见)。
func TestCreatePlatformTakesOverTheConnectionRowWhenTwoRetiredRowsSplitTheIdentity(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	// retired-a 占着 name=upstream-a,retired-b 占着接入关系 B。
	retiredA := newPlatform("upstream-a", "34020000001320000001")
	require.NoError(t, repo.CreatePlatform(ctx, retiredA))
	// retired-a 名下留一份共享(软删后会被置为 inactive),用来断言真删连子表一起清。
	require.NoError(t, repo.ReplaceProjection(ctx, retiredA.ID, 0,
		[]DeviceProjectionInput{{SourceDeviceID: 7001, PublishedDeviceID: "34020000001320007001"}},
		[]ChannelProjectionInput{{SourceDeviceID: 7001, SourceChannelID: 7101, PublishedChannelID: "34020000001320007101"}},
	))
	retiredB := newPlatform("upstream-b", "34020000001320000002")
	retiredB.Host = "198.51.100.9"
	require.NoError(t, repo.CreatePlatform(ctx, retiredB))
	require.NoError(t, repo.SoftDeletePlatform(ctx, retiredA.ID))
	require.NoError(t, repo.SoftDeletePlatform(ctx, retiredB.ID))

	// 想要的这行:名称取自 retired-a,接入关系取自 retired-b。
	merged := newPlatform("upstream-a", "34020000001320000002")
	merged.Host = "198.51.100.9"
	require.NoError(t, repo.CreatePlatform(ctx, merged),
		"两个键位各被一条遗留行占着时,必须把键位腾干净让用户建出来")
	require.Equal(t, retiredB.ID, merged.ID,
		"接手的是接入关系那条:复用它的主键才保住它遗留的投影行(上级看到的 published id 不抖动)")

	stored, err := repo.FindPlatform(ctx, merged.ID)
	require.NoError(t, err, "接手后这行必须不再是软删状态")
	require.Equal(t, "upstream-a", stored.Name)
	require.Equal(t, "198.51.100.9", stored.Host)

	var remaining int64
	require.NoError(t, repo.db.Unscoped().Model(&model.GbCascadePlatform{}).
		Where("id = ?", retiredA.ID).Count(&remaining).Error)
	require.Zero(t, remaining, "占着名称键位的遗留行必须被物理删除,否则这个名字永远腾不出来")
	for _, table := range []string{"gb_cascade_device_projection", "gb_cascade_channel_projection"} {
		require.NoError(t, repo.db.Unscoped().Table(table).
			Where("platform_id = ?", retiredA.ID).Count(&remaining).Error)
		require.Zero(t, remaining, "真删遗留平台要连它名下的残留投影一起清: "+table)
	}
	require.NoError(t, repo.db.Unscoped().Model(&model.GbCascadePlatform{}).
		Where("deleted_at IS NOT NULL").Count(&remaining).Error)
	require.Zero(t, remaining, "处理完之后不该再留下任何软删行")
}

// ⛔ 硬约束:清理只认软删行。拿一条活着的平台去清必须被拒绝,而且那行一点都不能动 ——
// 这是"腾键位"这条路径唯一可能造成不可逆破坏的地方。
func TestPurgeRetiredPlatformRefusesToTouchALivePlatform(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	live := newPlatform("upstream-live", "34020000001320000001")
	require.NoError(t, repo.CreatePlatform(ctx, live))

	err := repo.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return purgeRetiredPlatform(tx, live)
	})
	require.Error(t, err, "活着的平台不是遗留行,清理必须失败而不是把它删掉")

	stored, findErr := repo.FindPlatform(ctx, live.ID)
	require.NoError(t, findErr, "活着的平台必须原样留在库里")
	require.Equal(t, "upstream-live", stored.Name)
}

// 没有遗留行时,新建仍然是新建:别把正常路径改坏(两条平台必须各自拿到新行),
// 也别去接手一条名称与接入关系都对不上的软删行。
func TestCreatePlatformStillInsertsWhenNoRetiredRowMatches(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	first := newPlatform("upstream-a", "34020000001320000001")
	second := newPlatform("upstream-b", "34020000001320000002")
	require.NoError(t, repo.CreatePlatform(ctx, first))
	require.NoError(t, repo.CreatePlatform(ctx, second))
	require.NotEqual(t, first.ID, second.ID)
	require.NotZero(t, first.ID)

	// first 软删后,一条名称与接入关系都不同的平台仍然是全新的一行。
	require.NoError(t, repo.SoftDeletePlatform(ctx, first.ID))
	unrelated := newPlatform("upstream-c", "34020000001320000003")
	require.NoError(t, repo.CreatePlatform(ctx, unrelated))
	require.NotEqual(t, first.ID, unrelated.ID, "两处键位都不匹配时不得误接手")

	// 而"重新建一个一模一样的"才会走接手。
	reborn := newPlatform("upstream-a", "34020000001320000001")
	require.NoError(t, repo.CreatePlatform(ctx, reborn))
	require.Equal(t, first.ID, reborn.ID)
	require.NotEqual(t, unrelated.ID, reborn.ID)
}

// 改名撞上一条已删除的平台时,把那行连残留子表一起真删掉、把名字腾出来再改 ——
// 而不是把用户挡在门外:那条遗留行在平台列表里怎么翻都看不见,他没有任何入口可点。
func TestUpdatePlatformConfigPurgesTheRetiredPlatformHoldingTheTargetName(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	retired := newPlatform("upstream-a", "34020000001320000001")
	require.NoError(t, repo.CreatePlatform(ctx, retired))
	require.NoError(t, repo.ReplaceProjection(ctx, retired.ID, 0,
		[]DeviceProjectionInput{{SourceDeviceID: 7002, PublishedDeviceID: "34020000001320007002"}},
		[]ChannelProjectionInput{{SourceDeviceID: 7002, SourceChannelID: 7102, PublishedChannelID: "34020000001320007102"}},
	))
	require.NoError(t, repo.SoftDeletePlatform(ctx, retired.ID))

	live := newPlatform("upstream-b", "34020000001320000002")
	require.NoError(t, repo.CreatePlatform(ctx, live))
	liveID := live.ID

	live.Name = "upstream-a"
	updated, err := repo.UpdatePlatformConfig(ctx, live, live.ConfigRevision)
	require.NoError(t, err, "目标名字只是被一条已删平台占着,不该把改名挡在门外")
	require.Equal(t, "upstream-a", updated.Name)
	require.Equal(t, liveID, updated.ID, "改的是这条活着的平台,不是接手遗留行")

	var remaining int64
	require.NoError(t, repo.db.Unscoped().Model(&model.GbCascadePlatform{}).
		Where("id = ?", retired.ID).Count(&remaining).Error)
	require.Zero(t, remaining, "让位的遗留行必须被物理删除")
	for _, table := range []string{"gb_cascade_device_projection", "gb_cascade_channel_projection"} {
		require.NoError(t, repo.db.Unscoped().Table(table).
			Where("platform_id = ?", retired.ID).Count(&remaining).Error)
		require.Zero(t, remaining, "让位的遗留平台要连它名下的残留投影一起清: "+table)
	}
	require.NoError(t, repo.db.Unscoped().Model(&model.GbCascadePlatform{}).
		Where("deleted_at IS NOT NULL").Count(&remaining).Error)
	require.Zero(t, remaining, "活着的平台仍然只有一条,不该留下软删行")
}

// 正常的改名(目标名称没人用)必须照旧成功:前置检查不能把活路也堵上。
func TestUpdatePlatformConfigStillRenamesWhenTheTargetNameIsFree(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	retired := newPlatform("upstream-a", "34020000001320000001")
	require.NoError(t, repo.CreatePlatform(ctx, retired))
	require.NoError(t, repo.SoftDeletePlatform(ctx, retired.ID))

	live := newPlatform("upstream-b", "34020000001320000002")
	require.NoError(t, repo.CreatePlatform(ctx, live))

	live.Name = "upstream-c"
	updated, err := repo.UpdatePlatformConfig(ctx, live, live.ConfigRevision)
	require.NoError(t, err)
	require.Equal(t, "upstream-c", updated.Name)
	require.EqualValues(t, 2, updated.ConfigRevision)
}
