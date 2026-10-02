package controllers

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"uvplatform.cn/uvp-gb28181/app/gb28181/firmware"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"gorm.io/gorm"
)

// TC4.1: 上传固件成功
func TestFirmwareRepositoryController_Upload_Success(t *testing.T) {
	t.Skip("Integration test - requires full setup")
}

// TC4.2: 上传超大文件拒绝
func TestFirmwareRepositoryController_Upload_FileTooLarge(t *testing.T) {
	t.Skip("Integration test - requires full setup")
}

// TC4.3: 列表查询（租户隔离）
func TestFirmwareRepositoryController_List_TenantIsolation(t *testing.T) {
	t.Skip("Integration test - requires full setup")
}

// TC4.4: 下载固件（token 校验 + 租户二次校验）
func TestFirmwareRepositoryController_Download_TenantCheck(t *testing.T) {
	t.Skip("Integration test - requires full setup")
}

// Basic smoke test: controller can be instantiated
func TestFirmwareRepositoryController_Instantiation(t *testing.T) {
	var db *gorm.DB
	var cache app.CacheInterf
	repoService := firmware.NewRepositoryService(db)
	tokenService := firmware.NewDownloadTokenService(cache)

	controller := NewFirmwareRepositoryController(db, repoService, tokenService, "/tmp")
	assert.NotNil(t, controller)
	assert.Equal(t, "/tmp", controller.uploadBasePath)
	assert.Equal(t, int64(2*1024*1024*1024), controller.maxFileSize)
}

