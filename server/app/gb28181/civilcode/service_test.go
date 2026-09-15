package civilcode_test

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/civilcode"
)

func newCivilTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&civilcode.SysCivilCode{}))
	return db
}

// TestSeedIfEmpty_HappyPath A2.1 RED-1:空表 seed → 表非空 count > 3000
func TestSeedIfEmpty_HappyPath(t *testing.T) {
	db := newCivilTestDB(t)

	n, err := civilcode.SeedIfEmpty(db)
	require.NoError(t, err)
	assert.Greater(t, n, 3000, "GB/T 2260 6 位级应有 3000+ 条")

	var count int64
	require.NoError(t, db.Model(&civilcode.SysCivilCode{}).Count(&count).Error)
	assert.EqualValues(t, n, count)
}

// TestSeedIfEmpty_Idempotent A2.1 RED-2:再次 seed → count 不变(幂等)
func TestSeedIfEmpty_Idempotent(t *testing.T) {
	db := newCivilTestDB(t)

	n1, err := civilcode.SeedIfEmpty(db)
	require.NoError(t, err)
	require.Greater(t, n1, 0)

	// 二次 seed 应直接返回 0(已 seed)
	n2, err := civilcode.SeedIfEmpty(db)
	require.NoError(t, err)
	assert.Equal(t, 0, n2, "二次 seed 应跳过")

	var count int64
	require.NoError(t, db.Model(&civilcode.SysCivilCode{}).Count(&count).Error)
	assert.EqualValues(t, n1, count, "记录数不应翻倍")
}

// TestService_Lookup A2.1 RED-3:Lookup("370112") → 含历城区
func TestService_Lookup(t *testing.T) {
	db := newCivilTestDB(t)
	_, err := civilcode.SeedIfEmpty(db)
	require.NoError(t, err)

	svc := civilcode.NewService(db)
	require.NoError(t, svc.WarmCache(context.Background()))

	// 注:历城区当前 GB/T 2260 code 是 370112(370105 是天桥区,2019 后调整)
	got := svc.Lookup("370112")
	require.NotNil(t, got, "济南历城区 370112 应能查到")
	assert.Contains(t, got.Name, "历城区")
	assert.Equal(t, "370100", got.ParentCode, "上级应为济南市 370100")

	// 不存在 code 返回 nil
	assert.Nil(t, svc.Lookup("999999"))
}

// TestService_Children A2.1 RED-4:Children("370100") 返回济南各区/县
func TestService_Children(t *testing.T) {
	db := newCivilTestDB(t)
	_, err := civilcode.SeedIfEmpty(db)
	require.NoError(t, err)

	svc := civilcode.NewService(db)
	require.NoError(t, svc.WarmCache(context.Background()))

	kids := svc.Children("370100")
	assert.GreaterOrEqual(t, len(kids), 5, "济南市下应有 ≥ 5 个区/县")

	hasLicheng := false
	for _, k := range kids {
		if k.Code == "370112" {
			hasLicheng = true
		}
	}
	assert.True(t, hasLicheng, "历城区 370112 应在济南市子集中")
}

// TestService_SearchByName 名称搜索(本期 Phase 1 用名称匹配,拼音 Phase 2)
func TestService_SearchByName(t *testing.T) {
	db := newCivilTestDB(t)
	_, err := civilcode.SeedIfEmpty(db)
	require.NoError(t, err)

	svc := civilcode.NewService(db)
	require.NoError(t, svc.WarmCache(context.Background()))

	got := svc.SearchByName("历城区", 5)
	require.NotEmpty(t, got, "搜历城区应至少命中 1 条")
	hasLicheng := false
	for _, r := range got {
		if r.Code == "370112" {
			hasLicheng = true
		}
	}
	assert.True(t, hasLicheng)
}

// TestService_WarmCache_Count A2.1 RED-X:warm 后 AllCount 跟表行数一致
func TestService_WarmCache_Count(t *testing.T) {
	db := newCivilTestDB(t)
	n, err := civilcode.SeedIfEmpty(db)
	require.NoError(t, err)

	svc := civilcode.NewService(db)
	require.NoError(t, svc.WarmCache(context.Background()))
	assert.Equal(t, n, svc.AllCount())
}
