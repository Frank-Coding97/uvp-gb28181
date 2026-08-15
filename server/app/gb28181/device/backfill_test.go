package device

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func newBackfillTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}))
	return db
}

// T0 RED-1: owner_dept_id=0 的设备回填到默认部门,非 0 的不动
func TestBackfillZeroOwnerDept_BackfillsOnlyZero(t *testing.T) {
	db := newBackfillTestDB(t)

	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: "d1", OwnerDeptID: 0}).Error)
	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: "d2", OwnerDeptID: 0}).Error)
	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: "d3", OwnerDeptID: 5}).Error)

	affected, err := BackfillZeroOwnerDept(context.Background(), db, 9)
	require.NoError(t, err)
	assert.EqualValues(t, 2, affected, "只回填 owner=0 的两台")

	var counts []struct {
		OwnerDeptID uint
		Cnt         int64
	}
	require.NoError(t, db.Model(&gbmodels.GbDevice{}).
		Select("owner_dept_id", "count(*) as cnt").
		Group("owner_dept_id").Scan(&counts).Error)
	want := map[uint]int64{9: 2, 5: 1}
	got := make(map[uint]int64, len(counts))
	for _, c := range counts {
		got[c.OwnerDeptID] = c.Cnt
	}
	assert.Equal(t, want, got)
}

// T0 RED-2: 回填幂等——再次执行影响 0 行
func TestBackfillZeroOwnerDept_Idempotent(t *testing.T) {
	db := newBackfillTestDB(t)

	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: "d1", OwnerDeptID: 0}).Error)

	first, err := BackfillZeroOwnerDept(context.Background(), db, 9)
	require.NoError(t, err)
	assert.EqualValues(t, 1, first)

	second, err := BackfillZeroOwnerDept(context.Background(), db, 9)
	require.NoError(t, err)
	assert.EqualValues(t, 0, second, "二次回填应无影响")
}
