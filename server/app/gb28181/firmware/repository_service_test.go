package firmware

import (
	"net/http"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/global/consts"
	"uvplatform.com/uvp-gb28181/app/models"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	// ⛔ 必须一并建 gb_device_firmware_upgrade：Delete 删前要查"有没有升级记录引用这个固件"，
	//   少这张表会报 "no such table" 而把断言打偏到表结构上，看不出真实意图。
	require.NoError(t, db.AutoMigrate(&gbmodels.GbFirmwareRepository{}, &models.User{}, &gbmodels.GbDeviceFirmwareUpgrade{}))
	return db
}

func newTestContext(db *gorm.DB, userID, deptID uint) *gin.Context {
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = &http.Request{}
	ctx.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: userID}})

	// 插入测试用户记录（每次用不同的 username 避免冲突）
	username := "test-" + string(rune('0'+userID))
	db.Create(&models.User{
		BaseModel: models.BaseModel{ID: userID},
		DeptID:    deptID,
		Username:  username,
		Password:  "test",
	})

	return ctx
}

// TC2.1: 创建固件记录
func TestRepositoryService_Create(t *testing.T) {
	db := newTestDB(t)
	svc := NewRepositoryService(db)
	ctx := newTestContext(db, 1, 100)

	req := CreateFirmwareRequest{
		FirmwareID:   "fw-test-001",
		Version:      "v1.0.0",
		Manufacturer: "Hikvision",
		ModelPattern: "DS-.*",
		FileName:     "firmware.bin",
		FileSize:     1048576,
		FileHash:     "abc123",
		StoragePath:  "/storage/fw001",
		Status:       gbmodels.FirmwareStatusPublished,
		UploadedBy:   1,
		DeptID:       100,
	}

	result, err := svc.Create(ctx, req)
	require.NoError(t, err)
	require.NotZero(t, result.ID)
	require.Equal(t, "fw-test-001", result.FirmwareID)
	require.Equal(t, "v1.0.0", result.Version)
	require.Equal(t, gbmodels.FirmwareStatusPublished, result.Status)

	// 验证存储路径不对外暴露
	var persisted gbmodels.GbFirmwareRepository
	require.NoError(t, db.First(&persisted, result.ID).Error)
	require.Equal(t, "/storage/fw001", persisted.StoragePath)
}

// TC2.2: 租户隔离 - 不同部门看不到对方固件
func TestRepositoryService_List_TenantIsolation(t *testing.T) {
	db := newTestDB(t)
	svc := NewRepositoryService(db)

	// 部门100创建2条
	ctx100 := newTestContext(db, 1, 100)
	_, err := svc.Create(ctx100, CreateFirmwareRequest{
		FirmwareID: "fw-100-1", Version: "v1.0", Manufacturer: "Hikvision",
		FileName: "fw1.bin", FileSize: 1024, StoragePath: "/s/1",
		Status: gbmodels.FirmwareStatusPublished, UploadedBy: 1, DeptID: 100,
	})
	require.NoError(t, err)
	_, err = svc.Create(ctx100, CreateFirmwareRequest{
		FirmwareID: "fw-100-2", Version: "v2.0", Manufacturer: "Dahua",
		FileName: "fw2.bin", FileSize: 2048, StoragePath: "/s/2",
		Status: gbmodels.FirmwareStatusPublished, UploadedBy: 1, DeptID: 100,
	})
	require.NoError(t, err)

	// 部门200创建1条
	ctx200 := newTestContext(db, 2, 200)
	_, err = svc.Create(ctx200, CreateFirmwareRequest{
		FirmwareID: "fw-200-1", Version: "v1.0", Manufacturer: "Uniview",
		FileName: "fw3.bin", FileSize: 3072, StoragePath: "/s/3",
		Status: gbmodels.FirmwareStatusPublished, UploadedBy: 2, DeptID: 200,
	})
	require.NoError(t, err)

	// 部门100只能看到自己的2条
	list100, total100, err := svc.List(ctx100, ListFirmwareRequest{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Equal(t, int64(2), total100)
	require.Len(t, list100, 2)
	for _, item := range list100 {
		require.Equal(t, uint64(100), item.DeptID)
	}

	// 部门200只能看到自己的1条
	list200, total200, err := svc.List(ctx200, ListFirmwareRequest{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Equal(t, int64(1), total200)
	require.Len(t, list200, 1)
	require.Equal(t, uint64(200), list200[0].DeptID)
	require.Equal(t, "fw-200-1", list200[0].FirmwareID)
}

// TC2.3: 按厂商过滤
func TestRepositoryService_List_FilterByManufacturer(t *testing.T) {
	db := newTestDB(t)
	svc := NewRepositoryService(db)
	ctx := newTestContext(db, 1, 100)

	// 创建多厂商固件
	for i, manu := range []string{"Hikvision", "Hikvision", "Dahua"} {
		_, err := svc.Create(ctx, CreateFirmwareRequest{
			FirmwareID: "fw-" + manu + "-" + string(rune('1'+i)), Version: "v1.0",
			Manufacturer: manu, FileName: "fw.bin", FileSize: 1024,
			StoragePath: "/s", Status: gbmodels.FirmwareStatusPublished,
			UploadedBy: 1, DeptID: 100,
		})
		require.NoError(t, err)
	}

	// 过滤 Hikvision
	list, total, err := svc.List(ctx, ListFirmwareRequest{
		Page: 1, PageSize: 10, Manufacturer: "Hikvision",
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, list, 2)
	for _, item := range list {
		require.Equal(t, "Hikvision", item.Manufacturer)
	}
}

// TC2.4: 按状态过滤
func TestRepositoryService_List_FilterByStatus(t *testing.T) {
	db := newTestDB(t)
	svc := NewRepositoryService(db)
	ctx := newTestContext(db, 1, 100)

	// 创建不同状态
	for i, status := range []gbmodels.FirmwareStatus{
		gbmodels.FirmwareStatusPublished,
		gbmodels.FirmwareStatusPublished,
		gbmodels.FirmwareStatusArchived,
	} {
		_, err := svc.Create(ctx, CreateFirmwareRequest{
			FirmwareID: "fw-" + string(rune('1'+i)), Version: "v1.0",
			Manufacturer: "Test", FileName: "fw.bin", FileSize: 1024,
			StoragePath: "/s", Status: status, UploadedBy: 1, DeptID: 100,
		})
		require.NoError(t, err)
	}

	// 过滤 published
	list, total, err := svc.List(ctx, ListFirmwareRequest{
		Page: 1, PageSize: 10, Status: gbmodels.FirmwareStatusPublished,
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, list, 2)
	for _, item := range list {
		require.Equal(t, gbmodels.FirmwareStatusPublished, item.Status)
	}
}

// TC2.5: 分页
func TestRepositoryService_List_Pagination(t *testing.T) {
	db := newTestDB(t)
	svc := NewRepositoryService(db)
	ctx := newTestContext(db, 1, 100)

	// 创建5条
	for i := 0; i < 5; i++ {
		_, err := svc.Create(ctx, CreateFirmwareRequest{
			FirmwareID: "fw-" + string(rune('1'+i)), Version: "v1.0",
			Manufacturer: "Test", FileName: "fw.bin", FileSize: 1024,
			StoragePath: "/s", Status: gbmodels.FirmwareStatusPublished,
			UploadedBy: 1, DeptID: 100,
		})
		require.NoError(t, err)
	}

	// 第1页(2条)
	page1, total, err := svc.List(ctx, ListFirmwareRequest{Page: 1, PageSize: 2})
	require.NoError(t, err)
	require.Equal(t, int64(5), total)
	require.Len(t, page1, 2)

	// 第2页(2条)
	page2, total, err := svc.List(ctx, ListFirmwareRequest{Page: 2, PageSize: 2})
	require.NoError(t, err)
	require.Equal(t, int64(5), total)
	require.Len(t, page2, 2)

	// 第3页(1条)
	page3, total, err := svc.List(ctx, ListFirmwareRequest{Page: 3, PageSize: 2})
	require.NoError(t, err)
	require.Equal(t, int64(5), total)
	require.Len(t, page3, 1)
}

// TC2.6: GetByID 租户隔离
func TestRepositoryService_GetByID_TenantIsolation(t *testing.T) {
	db := newTestDB(t) // 独立 DB
	svc := NewRepositoryService(db)

	// 部门100创建
	ctx100 := newTestContext(db, 10, 100) // 用不同的 userID
	created, err := svc.Create(ctx100, CreateFirmwareRequest{
		FirmwareID: "fw-100", Version: "v1.0", Manufacturer: "Test",
		FileName: "fw.bin", FileSize: 1024, StoragePath: "/s",
		Status: gbmodels.FirmwareStatusPublished, UploadedBy: 10, DeptID: 100,
	})
	require.NoError(t, err)

	// 部门100可以查到
	found, err := svc.GetByID(ctx100, created.ID)
	require.NoError(t, err)
	require.Equal(t, created.ID, found.ID)

	// 部门200查不到
	ctx200 := newTestContext(db, 20, 200) // 用不同的 userID
	_, err = svc.GetByID(ctx200, created.ID)
	require.Error(t, err)
	require.Equal(t, gorm.ErrRecordNotFound, err)
}

// TC2.7: Delete 物理删除记录（不是软删除）
//
// ⛔⛔ 本用例**原先断言的是"软删除"**：Delete 只把 status 改成 archived，断言
//   "记录仍存在但状态为 archived"。但那正是 2026-10-06 上报的缺陷 2 本体 ——
//   用户点删除收到"删除成功"，刷新后台账里那条记录还好端端在（列表默认不过滤
//   archived，于是"已删除"的行始终显示）。
//
//   归档是一个**独立动作**（PATCH .../status { status: "archived" }），语义是
//   "下线但保留文件供历史追溯"；删除必须真的消失。两者混为一谈是当初的错，
//   这里的断言随之改掉。
func TestRepositoryService_Delete(t *testing.T) {
	db := newTestDB(t)
	svc := NewRepositoryService(db)
	ctx := newTestContext(db, 1, 100)

	created, err := svc.Create(ctx, CreateFirmwareRequest{
		FirmwareID: "fw-del", Version: "v1.0", Manufacturer: "Test",
		FileName: "fw.bin", FileSize: 1024, StoragePath: "/s",
		Status: gbmodels.FirmwareStatusPublished, UploadedBy: 1, DeptID: 100,
	})
	require.NoError(t, err)

	err = svc.Delete(ctx, created.ID)
	require.NoError(t, err)

	// 契约：记录必须**真的消失**，而不是换个状态继续躺在列表里
	var persisted gbmodels.GbFirmwareRepository
	err = db.First(&persisted, created.ID).Error
	require.Error(t, err, "删除后记录必须不存在；仍存在即回到「提示删除成功但台账还在」的缺陷")
	require.Equal(t, gorm.ErrRecordNotFound, err)
}

// TC2.7b: 被设备升级记录引用的固件不允许删除。
//
// ⛔ 硬删会让历史升级记录指向不存在的固件，审计链断掉。这里选择**明确拒绝**
//   而不是级联删除 —— 升级历史是凭证，不该因为清理仓库被抹掉。
func TestRepositoryService_Delete_ReferencedByUpgradeIsRejected(t *testing.T) {
	db := newTestDB(t)
	svc := NewRepositoryService(db)
	ctx := newTestContext(db, 1, 100)

	created, err := svc.Create(ctx, CreateFirmwareRequest{
		FirmwareID: "fw-used", Version: "v2.0", Manufacturer: "Test",
		FileName: "fw.bin", FileSize: 1024, StoragePath: "/s",
		Status: gbmodels.FirmwareStatusPublished, UploadedBy: 1, DeptID: 100,
	})
	require.NoError(t, err)

	// 造一条引用该固件的升级记录
	require.NoError(t, db.Create(&gbmodels.GbDeviceFirmwareUpgrade{
		OperationID:  "op-fw-used-1",
		IdempotencyKey: "idem-fw-used-1",
		DeviceID:     1,
		DeviceCode:   "34020000001320000001",
		Firmware:     created.Version,
		FileURL:      "http://placeholder/fw.bin",
		Manufacturer: "Test",
		FirmwareID:   created.FirmwareID,
		SessionID:    "session-fw-used-1",
		SN:           1,
		Status:       gbmodels.FirmwareUpgradeSucceeded,
		ActorID:      1,
		ActorDeptID:  100,
	}).Error)

	err = svc.Delete(ctx, created.ID)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrFirmwareInUse)
	// 错误信息要带引用条数，让用户知道为什么删不掉
	require.Contains(t, err.Error(), "1 条设备升级记录")

	// 拒绝之后记录必须还在
	var persisted gbmodels.GbFirmwareRepository
	require.NoError(t, db.First(&persisted, created.ID).Error)
	require.Equal(t, gbmodels.FirmwareStatusPublished, persisted.Status)
}

// TC2.8: Delete 租户隔离
func TestRepositoryService_Delete_TenantIsolation(t *testing.T) {
	db := newTestDB(t) // 独立 DB
	svc := NewRepositoryService(db)

	// 部门100创建
	ctx100 := newTestContext(db, 30, 100) // 用不同的 userID
	created, err := svc.Create(ctx100, CreateFirmwareRequest{
		FirmwareID: "fw-100", Version: "v1.0", Manufacturer: "Test",
		FileName: "fw.bin", FileSize: 1024, StoragePath: "/s",
		Status: gbmodels.FirmwareStatusPublished, UploadedBy: 30, DeptID: 100,
	})
	require.NoError(t, err)

	// 部门200无法删除
	ctx200 := newTestContext(db, 40, 200) // 用不同的 userID
	err = svc.Delete(ctx200, created.ID)
	require.Error(t, err)
	require.Equal(t, gorm.ErrRecordNotFound, err)

	// 记录仍然存在
	found, err := svc.GetByID(ctx100, created.ID)
	require.NoError(t, err)
	require.Equal(t, gbmodels.FirmwareStatusPublished, found.Status)
}
