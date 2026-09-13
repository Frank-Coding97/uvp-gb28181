package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/model"
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

// 名称与接入关系分别被两条不同的遗留行占着时无解:复活任意一条,另一个键位仍然
// 冲突。这种情况必须报明确的错误,而不是随便挑一条把冲突推到下一次。
func TestCreatePlatformReportsWhenTwoRetiredRowsSplitTheIdentity(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	// retired-a 占着 name=upstream-a,retired-b 占着接入关系 B。
	retiredA := newPlatform("upstream-a", "34020000001320000001")
	require.NoError(t, repo.CreatePlatform(ctx, retiredA))
	retiredB := newPlatform("upstream-b", "34020000001320000002")
	retiredB.Host = "198.51.100.9"
	require.NoError(t, repo.CreatePlatform(ctx, retiredB))
	require.NoError(t, repo.SoftDeletePlatform(ctx, retiredA.ID))
	require.NoError(t, repo.SoftDeletePlatform(ctx, retiredB.ID))

	// 想要的这行:名称取自 retired-a,接入关系取自 retired-b。
	merged := newPlatform("upstream-a", "34020000001320000002")
	merged.Host = "198.51.100.9"
	err := repo.CreatePlatform(ctx, merged)
	require.ErrorIs(t, err, ErrRetiredPlatformConflict)
	require.Zero(t, merged.ID, "接手失败时不得回填一个不存在的 id")
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

// 改名撞上一条已删除的平台时,要说清是"被已删除的平台占用"。否则接口只能回
// 1062 那句"已存在",用户去列表里怎么翻都找不到那个平台 —— 它已经被删了。
func TestUpdatePlatformConfigReportsTheRetiredPlatformHoldingTheTargetName(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	retired := newPlatform("upstream-a", "34020000001320000001")
	require.NoError(t, repo.CreatePlatform(ctx, retired))
	require.NoError(t, repo.SoftDeletePlatform(ctx, retired.ID))

	live := newPlatform("upstream-b", "34020000001320000002")
	require.NoError(t, repo.CreatePlatform(ctx, live))

	live.Name = "upstream-a"
	_, err := repo.UpdatePlatformConfig(ctx, live, live.ConfigRevision)
	require.ErrorIs(t, err, ErrRetiredPlatformConflict)

	stored, err := repo.FindPlatform(ctx, live.ID)
	require.NoError(t, err)
	require.Equal(t, "upstream-b", stored.Name, "被拦下时不得改动任何字段")
	require.EqualValues(t, live.ConfigRevision, stored.ConfigRevision)
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
