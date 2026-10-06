package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	gbfirmware "uvplatform.cn/uvp-gb28181/app/gb28181/firmware"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	"uvplatform.cn/uvp-gb28181/app/models"
)

// ⛔⛔ 固件下载链路的 ID 语义：路由 `:id` 是**数字主键**（gb_firmware_repository.id），
// 而 token 快照里必须绑**业务固件号**（firmware_id，形如 fw_<hash16>）。
//
// 起因（2026-10-06 线上"点下载 → 403 无权下载此固件"）：
//   GenerateDownloadLink 错把 c.Param("id") 当 firmware_id 写进快照，前端传的又正是
//   数字主键 ⇒ 快照 FirmwareID="1" ⇒ Download 按 `firmware_id='1'` 查不到记录 ⇒
//   最终撞上部门比较、回报 403"无权下载此固件"。
//   ⛔ 最坑的是**报错指错方向**：真实原因是 ID 语义错配，用户（和排查的人）却拿到
//   一句"权限问题"，于是去查权限、查部门，永远查不到真因。
//
// 这组用例锁死：传数字主键能正常生成链接，且快照里是业务固件号。

// memCache 是 app.CacheInterf 的内存实现。
// 只实现本链路真正用到的 Set/GetDel/Get —— 其余方法内嵌 nil 接口占位，
// 一旦被调用会 panic，正好暴露"用例依赖了没实现的行为"。
type memCache struct {
	app.CacheInterf
	store map[string]string
}

func (m *memCache) Set(_ context.Context, key, value string, _ time.Duration) error {
	m.store[key] = value
	return nil
}

func (m *memCache) Get(_ context.Context, key string) (string, error) {
	v, ok := m.store[key]
	if !ok {
		return "", app.ErrKeyNotFound
	}
	return v, nil
}

func (m *memCache) GetDel(_ context.Context, key string) (string, error) {
	v, ok := m.store[key]
	if !ok {
		return "", app.ErrKeyNotFound
	}
	delete(m.store, key)
	return v, nil
}

func setupFirmwareDownloadEnv(t *testing.T) (*gorm.DB, *memCache, *FirmwareRepositoryController) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbFirmwareRepository{}, &models.User{}))

	require.NoError(t, db.Create(&models.User{
		BaseModel: models.BaseModel{ID: 1},
		DeptID:    1,
		Username:  "admin",
		Password:  "x",
	}).Error)
	require.NoError(t, db.Create(&gbmodels.GbFirmwareRepository{
		FirmwareID:   "fw_d635fe0401b2a7b3",
		Version:      "v1.2.3",
		Manufacturer: "厂商",
		FileName:     "a.jpg",
		FileSize:     1,
		StoragePath:  "uploads/firmware/x_a.jpg",
		Status:       gbmodels.FirmwareStatusPublished,
		UploadedBy:   1,
		DeptID:       1,
	}).Error)

	cache := &memCache{store: map[string]string{}}
	ctrl := NewFirmwareRepositoryController(db,
		gbfirmware.NewRepositoryService(db),
		gbfirmware.NewDownloadTokenService(cache),
		"uploads")
	return db, cache, ctrl
}

func TestGenerateDownloadLink_BindsBusinessFirmwareIDNotNumericPK(t *testing.T) {
	_, cache, ctrl := setupFirmwareDownloadEnv(t)

	// ⛔ 传**数字主键**（前端实际传的就是它）
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/x", nil)
	c.Params = gin.Params{{Key: "id", Value: "1"}}
	c.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 1, Username: "admin"}})
	ctrl.GenerateDownloadLink(c)

	body := w.Body.String()
	require.NotContains(t, body, "固件不存在",
		"传数字主键必须能查到固件；返回=%s（说明又把它当 firmware_id 查了）", body)
	require.Contains(t, body, `"code":0`, "应成功生成下载链接；返回=%s", body)

	// 从缓存里取出 token 快照，断言绑的是**业务固件号**
	require.Len(t, cache.store, 1, "应写入一份 token 快照")
	var snapshot gbfirmware.DownloadTokenSnapshot
	for _, raw := range cache.store {
		require.NoError(t, json.Unmarshal([]byte(raw), &snapshot))
	}
	require.Equal(t, "fw_d635fe0401b2a7b3", snapshot.FirmwareID,
		"token 快照必须绑业务固件号(firmware_id)；绑数字主键会让 Download 查不到记录、最终误报 403")
	require.Equal(t, uint64(1), snapshot.DeptID)
}

func TestGenerateDownloadLink_RejectsNonNumericID(t *testing.T) {
	_, _, ctrl := setupFirmwareDownloadEnv(t)

	// 非数字主键必须被明确拒绝，而不是被静默当成 firmware_id 去查
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/x", nil)
	c.Params = gin.Params{{Key: "id", Value: "fw_d635fe0401b2a7b3"}}
	c.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 1, Username: "admin"}})
	ctrl.GenerateDownloadLink(c)

	require.Contains(t, w.Body.String(), "参数错误",
		"非数字主键应报参数错误；返回=%s", w.Body.String())
}

func TestDownloadRoute_PathShapeMatchesGenerateSide(t *testing.T) {
	// 端到端形状：路由模板必须与生成端引用的常量一致
	require.Equal(t,
		"/api/gb28181/device-mgmt/firmware-repository/download/:token",
		gbfirmware.DownloadRoutePath+":token")
}
