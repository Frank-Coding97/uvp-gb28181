package directory

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/civilcode"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func setupCivilCodeTestDB(t *testing.T) (*gorm.DB, *civilcode.Service) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	err = db.AutoMigrate(&models.GbChannel{}, &civilcode.SysCivilCode{})
	assert.NoError(t, err)

	// 灌几个字典数据(GB/T 2260)
	db.Create(&civilcode.SysCivilCode{Code: "370000", Name: "山东省", ParentCode: "", Level: 1})
	db.Create(&civilcode.SysCivilCode{Code: "370100", Name: "济南市", ParentCode: "370000", Level: 2})
	db.Create(&civilcode.SysCivilCode{Code: "370112", Name: "历城区", ParentCode: "370100", Level: 3})
	db.Create(&civilcode.SysCivilCode{Code: "110000", Name: "北京市", ParentCode: "", Level: 1})
	db.Create(&civilcode.SysCivilCode{Code: "110100", Name: "市辖区", ParentCode: "110000", Level: 2})
	db.Create(&civilcode.SysCivilCode{Code: "110108", Name: "海淀区", ParentCode: "110100", Level: 3})

	svc := civilcode.NewService(db)
	_ = svc.WarmCache(context.Background())
	return db, svc
}

func TestCivilCodeDimension_GetRoots_Provinces(t *testing.T) {
	db, svc := setupCivilCodeTestDB(t)

	// 3 个通道: 山东济南历城, 北京海淀, 未分配
	db.Create(&models.GbChannel{OwnerDeptID: 1, ChannelID: "CH001", DeviceID: "DEV001", CivilCode: "370112"})
	db.Create(&models.GbChannel{OwnerDeptID: 1, ChannelID: "CH002", DeviceID: "DEV002", CivilCode: "110108"})
	db.Create(&models.GbChannel{OwnerDeptID: 1, ChannelID: "CH003", DeviceID: "DEV003", CivilCode: "000000"})

	dim := NewCivilCodeDimension(db, svc, nil)
	roots, err := dim.GetRoots(false)
	assert.NoError(t, err)

	// 期望:2 个省 + 1 个未分配桶 = 3 条
	assert.Len(t, roots, 3)

	names := []string{roots[0].Name, roots[1].Name, roots[2].Name}
	assert.Contains(t, names, "山东省")
	assert.Contains(t, names, "北京市")
	assert.Contains(t, names, "未分配行政区")

	// 未分配桶置末尾
	assert.Equal(t, "未分配行政区", roots[2].Name)
	assert.Equal(t, NodeTypeUnassigned, roots[2].NodeType)
}

func TestCivilCodeDimension_GetChildren_ProvinceToCity(t *testing.T) {
	db, svc := setupCivilCodeTestDB(t)

	db.Create(&models.GbChannel{OwnerDeptID: 1, ChannelID: "CH001", DeviceID: "DEV001", CivilCode: "370112"})

	dim := NewCivilCodeDimension(db, svc, nil)
	// 山东省 → 济南市
	children, err := dim.GetChildren("province:37", false)
	assert.NoError(t, err)
	assert.Len(t, children, 1)
	assert.Equal(t, "济南市", children[0].Name)
	assert.Equal(t, "3701", children[0].CivilCode)
}

func TestCivilCodeDimension_GetChildren_CityToDistrict(t *testing.T) {
	db, svc := setupCivilCodeTestDB(t)

	db.Create(&models.GbChannel{OwnerDeptID: 1, ChannelID: "CH001", DeviceID: "DEV001", CivilCode: "370112"})

	dim := NewCivilCodeDimension(db, svc, nil)
	// 济南市 → 历城区
	children, err := dim.GetChildren("city:3701", false)
	assert.NoError(t, err)
	assert.Len(t, children, 1)
	assert.Equal(t, "历城区", children[0].Name)
	assert.Equal(t, "370112", children[0].CivilCode)
}

func TestCivilCodeDimension_GetChildren_DistrictToChannels(t *testing.T) {
	db, svc := setupCivilCodeTestDB(t)

	db.Create(&models.GbChannel{OwnerDeptID: 1, ChannelID: "CH001", DeviceID: "DEV001", Name: "摄像头1", CivilCode: "370112"})
	db.Create(&models.GbChannel{OwnerDeptID: 1, ChannelID: "CH002", DeviceID: "DEV002", Name: "摄像头2", CivilCode: "370112"})
	db.Create(&models.GbChannel{OwnerDeptID: 1, ChannelID: "CH003", DeviceID: "DEV003", Name: "别地", CivilCode: "110108"})

	dim := NewCivilCodeDimension(db, svc, nil)
	// 历城区 → 2 个通道
	children, err := dim.GetChildren("district:370112", false)
	assert.NoError(t, err)
	assert.Len(t, children, 2)
	for _, c := range children {
		assert.Equal(t, NodeTypeChannel, c.NodeType)
		assert.True(t, c.IsLeaf)
	}
}

func TestCivilCodeDimension_GetChildren_UnassignedBucket(t *testing.T) {
	db, svc := setupCivilCodeTestDB(t)

	// 有未分配通道
	db.Create(&models.GbChannel{OwnerDeptID: 1, ChannelID: "CH001", DeviceID: "DEV001", Name: "未分配1", CivilCode: "000000"})
	db.Create(&models.GbChannel{OwnerDeptID: 1, ChannelID: "CH002", DeviceID: "DEV002", Name: "未分配2", CivilCode: ""})

	dim := NewCivilCodeDimension(db, svc, nil)
	children, err := dim.GetChildren("unassigned", false)
	assert.NoError(t, err)
	// 000000 和 空串都归到未分配桶
	assert.Len(t, children, 2)
}

func TestCivilCodeDimension_NoUnassignedBucketIfEmpty(t *testing.T) {
	db, svc := setupCivilCodeTestDB(t)

	// 只有有效行政区通道,没有未分配
	db.Create(&models.GbChannel{OwnerDeptID: 1, ChannelID: "CH001", DeviceID: "DEV001", CivilCode: "370112"})

	dim := NewCivilCodeDimension(db, svc, nil)
	roots, err := dim.GetRoots(false)
	assert.NoError(t, err)
	// 只有 1 个省,没有未分配桶
	assert.Len(t, roots, 1)
	assert.Equal(t, "山东省", roots[0].Name)
}
