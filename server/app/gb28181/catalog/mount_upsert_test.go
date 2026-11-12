package catalog

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// TestEnsurePrimaryMountUpsertSemantics 锁住 ensurePrimaryMount 的 upsert 语义。
//
// ⛔ 为什么这个函数要被单独测：它原本是「先 SELECT 这一行在不在，不在就 INSERT」，
// 2026-11-09 万级压测证明那种写法在 MySQL REPEATABLE-READ 下会因间隙锁死锁
// （实测 6311 条死锁、通道覆盖率只有 8.4%），改成原子 upsert 后必须保证行为不变：
//
//  1. 首次调用建一行，且因为没有主挂载所以 is_primary=true；
//  2. 重复调用不新增行、不报错、也不把 is_primary 抖掉；
//  3. 同一 channel 换一个父节点再调，是新增第二行，但**不抢主挂载**
//     （主挂载必须唯一，DAG 多挂载场景下这一条决定页面显示哪个位置）。
//
// 并发不在这里验：SQLite 是单写者，测出来的"并发"只是排队，证明不了 MySQL 的
// 间隙锁行为。并发安全性由 220 上的万级压测验证，测试只守语义。
func TestEnsurePrimaryMountUpsertSemantics(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbChannelMount{}))

	ctx := context.Background()
	const (
		ownerDept uint = 1
		channelID uint = 100
		parentA   uint = 10
		parentB   uint = 20
	)

	// 1) 首次：建一行并标为主挂载
	require.NoError(t, ensurePrimaryMount(ctx, db, ownerDept, channelID, parentA, "Camera-1"))

	var mounts []gbmodels.GbChannelMount
	require.NoError(t, db.Find(&mounts).Error)
	require.Len(t, mounts, 1)
	require.True(t, mounts[0].IsPrimary, "首个挂载必须是主挂载")
	require.Equal(t, parentA, mounts[0].ParentNodeID)

	// 2) 重复上报：不新增、不报错、is_primary 不被抖掉
	require.NoError(t, ensurePrimaryMount(ctx, db, ownerDept, channelID, parentA, "Camera-1"))
	require.NoError(t, ensurePrimaryMount(ctx, db, ownerDept, channelID, parentA, "Camera-1 改名"))

	mounts = nil
	require.NoError(t, db.Find(&mounts).Error)
	require.Len(t, mounts, 1, "重复上报不得新增挂载行")
	require.True(t, mounts[0].IsPrimary, "重复上报不得把主挂载抖成非主")
	require.Equal(t, "Camera-1", mounts[0].DisplayName, "已存在的挂载按原语义不更新显示名")

	// 3) 换父节点：新增第二行，但不是主挂载
	require.NoError(t, ensurePrimaryMount(ctx, db, ownerDept, channelID, parentB, "Camera-1"))

	mounts = nil
	require.NoError(t, db.Find(&mounts).Error)
	require.Len(t, mounts, 2, "换父节点应新增一行（N:N 多挂载）")

	var primary int
	for _, m := range mounts {
		if m.IsPrimary {
			primary++
			require.Equal(t, parentA, m.ParentNodeID, "主挂载应仍是最早那个")
		}
	}
	require.Equal(t, 1, primary, "主挂载必须唯一")
}
