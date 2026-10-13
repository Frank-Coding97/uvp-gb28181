package controllers

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/gb28181/firmware"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/utils/common"
	"uvplatform.com/uvp-gb28181/app/utils/response"
)

const (
	maxFirmwareFileSize = 2 * 1024 * 1024 * 1024 // 2GB
	firmwareUploadPath  = "firmware"
)

type FirmwareRepositoryController struct {
	db             *gorm.DB
	repoService    *firmware.RepositoryService
	tokenService   *firmware.DownloadTokenService
	uploadBasePath string
	maxFileSize    int64
}

func NewFirmwareRepositoryController(db *gorm.DB, repoService *firmware.RepositoryService, tokenService *firmware.DownloadTokenService, uploadBasePath string) *FirmwareRepositoryController {
	if uploadBasePath == "" {
		uploadBasePath = "uploads"
	}
	return &FirmwareRepositoryController{
		db:             db,
		repoService:    repoService,
		tokenService:   tokenService,
		uploadBasePath: uploadBasePath,
		maxFileSize:    maxFirmwareFileSize,
	}
}

// Upload 上传固件文件
func (ctrl *FirmwareRepositoryController) Upload(c *gin.Context) {
	claims := common.GetClaims(c)
	if claims == nil {
		response.Fail(c, "未授权")
		return
	}

	// 获取用户部门信息
	var user struct {
		DeptID uint `gorm:"column:dept_id"`
	}
	if err := ctrl.db.Table("sys_users").Select("dept_id").Where("id = ?", claims.UserID).First(&user).Error; err != nil {
		response.Fail(c, "用户信息查询失败")
		return
	}

	// 获取上传文件
	file, err := c.FormFile("file")
	if err != nil {
		response.Fail(c, "文件上传失败: "+err.Error())
		return
	}

	// 文件大小检查
	if file.Size > ctrl.maxFileSize {
		response.Fail(c, fmt.Sprintf("文件大小超过 %d MB", ctrl.maxFileSize/(1024*1024)))
		return
	}

	// 读取表单字段
	version := strings.TrimSpace(c.PostForm("version"))
	manufacturer := strings.TrimSpace(c.PostForm("manufacturer"))
	modelPattern := strings.TrimSpace(c.PostForm("modelPattern"))
	remark := strings.TrimSpace(c.PostForm("remark"))

	if version == "" || manufacturer == "" {
		response.Fail(c, "版本号和厂商不能为空")
		return
	}

	// 计算文件 hash
	src, err := file.Open()
	if err != nil {
		response.Fail(c, "文件读取失败")
		return
	}
	defer src.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, src); err != nil {
		response.Fail(c, "文件哈希计算失败")
		return
	}
	fileHash := hex.EncodeToString(hash.Sum(nil))

	// 重新打开文件用于保存
	src, err = file.Open()
	if err != nil {
		response.Fail(c, "文件读取失败")
		return
	}
	defer src.Close()

	// 保存文件到磁盘
	targetDir := filepath.Join(ctrl.uploadBasePath, firmwareUploadPath)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		response.Fail(c, "创建上传目录失败")
		return
	}

	// 生成唯一文件名: hash[:16] + 原文件名
	filename := fmt.Sprintf("%s_%s", fileHash[:16], filepath.Base(file.Filename))
	storagePath := filepath.Join(targetDir, filename)

	dst, err := os.Create(storagePath)
	if err != nil {
		response.Fail(c, "文件保存失败")
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		os.Remove(storagePath)
		response.Fail(c, "文件写入失败")
		return
	}

	// 生成 firmwareID
	firmwareID := fmt.Sprintf("fw_%s", fileHash[:16])

	// 保存到数据库
	record, err := ctrl.repoService.Create(c, firmware.CreateFirmwareRequest{
		FirmwareID:   firmwareID,
		Version:      version,
		Manufacturer: manufacturer,
		ModelPattern: modelPattern,
		FileName:     file.Filename,
		FileSize:     file.Size,
		FileHash:     fileHash,
		StoragePath:  storagePath,
		StorageKey:   filename,
		Status:       gbmodels.FirmwareStatusDraft,
		UploadedBy:   uint64(claims.UserID),
		DeptID:       uint64(user.DeptID),
		Remark:       remark,
	})

	if err != nil {
		os.Remove(storagePath)
		response.Fail(c, "数据库保存失败: "+err.Error())
		return
	}

	response.Success(c, gin.H{
		"firmwareId":   record.FirmwareID,
		"version":      record.Version,
		"manufacturer": record.Manufacturer,
		"fileName":     record.FileName,
		"fileSize":     record.FileSize,
		"fileHash":     record.FileHash,
		"status":       record.Status,
	})
}

// List 查询固件列表
func (ctrl *FirmwareRepositoryController) List(c *gin.Context) {
	if ctrl.repoService == nil {
		app.ZapLog.Error("固件仓库 repoService 为 nil", zap.String("event", "firmware.list_nil_service"))
		response.Fail(c, "服务未初始化")
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "10"))
	manufacturer := c.Query("manufacturer")
	status := c.Query("status")

	records, total, err := ctrl.repoService.List(c, firmware.ListFirmwareRequest{
		Page:         page,
		PageSize:     pageSize,
		Manufacturer: manufacturer,
		Status:       gbmodels.FirmwareStatus(status),
	})

	if err != nil {
		response.Fail(c, "查询失败: "+err.Error())
		return
	}

	response.Success(c, gin.H{
		"list":     records,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

// GetByID 查询固件详情
func (ctrl *FirmwareRepositoryController) GetByID(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, "参数错误")
		return
	}

	record, err := ctrl.repoService.GetByID(c, id)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			response.Fail(c, "固件不存在")
			return
		}
		response.Fail(c, "查询失败: "+err.Error())
		return
	}

	response.Success(c, record)
}

// Delete 删除固件（物理删除：数据库记录 + 磁盘文件）
func (ctrl *FirmwareRepositoryController) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, "参数错误")
		return
	}

	if err := ctrl.repoService.Delete(c, id); err != nil {
		// 已被设备升级记录引用 ⇒ 明确告知原因，不要笼统报"删除失败"
		if errors.Is(err, firmware.ErrFirmwareInUse) {
			response.Fail(c, err.Error())
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.Fail(c, "固件不存在或无权限删除")
			return
		}
		response.Fail(c, "删除失败: "+err.Error())
		return
	}

	response.Success(c, "删除成功")
}

// UpdateStatus 更新固件状态
func (ctrl *FirmwareRepositoryController) UpdateStatus(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, "参数错误")
		return
	}

	var req struct {
		Status string `json:"status" binding:"required,oneof=draft published archived"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, "参数错误: "+err.Error())
		return
	}

	if err := ctrl.repoService.UpdateStatus(c, id, gbmodels.FirmwareStatus(req.Status)); err != nil {
		if err == gorm.ErrRecordNotFound {
			response.Fail(c, "固件不存在")
			return
		}
		response.Fail(c, "更新状态失败: "+err.Error())
		return
	}

	response.Success(c, "状态更新成功")
}

// GenerateDownloadLink 生成临时下载链接
func (ctrl *FirmwareRepositoryController) GenerateDownloadLink(c *gin.Context) {
	claims := common.GetClaims(c)
	if claims == nil {
		response.Fail(c, "未授权")
		return
	}

	// ⛔⛔ 这里的 `:id` 是**数字主键** gb_firmware_repository.id（全仓 CRUD 路由的统一约定），
	//   **不是** firmware_id。历史上本函数错把 c.Param("id") 当 firmware_id 写进 token 快照，
	//   而前端传的也正是数字主键 ⇒ 快照里 FirmwareID="1"，Download 拿它去查
	//   `firmware_id = '1'` 永远查不到 ⇒ 用户看到的是"无权下载此固件"(403)，
	//   而真正的原因是**两侧 ID 语义错配**，报错信息还完全指错方向，极难归因。
	//   现在统一：先按主键取记录，再把**真正的 firmware_id** 写进快照。
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, "参数错误")
		return
	}

	record, err := ctrl.repoService.GetByID(c, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.Fail(c, "固件不存在")
			return
		}
		response.Fail(c, "查询失败: "+err.Error())
		return
	}

	// 获取用户部门
	var user struct {
		DeptID uint `gorm:"column:dept_id"`
	}
	if err := ctrl.db.Table("sys_users").Select("dept_id").Where("id = ?", claims.UserID).First(&user).Error; err != nil {
		response.Fail(c, "用户信息查询失败")
		return
	}

	// token 快照绑定的是**业务固件号**（Download 端按它查回记录），不是数据库主键
	token, downloadURL, expiresAt, err := ctrl.tokenService.Generate(
		c.Request.Context(), record.FirmwareID, uint64(claims.UserID), uint64(user.DeptID))
	if err != nil {
		response.Fail(c, "生成下载链接失败: "+err.Error())
		return
	}

	response.Success(c, gin.H{
		"token":       token,
		"downloadUrl": downloadURL,
		"expiresAt":   expiresAt,
	})
}

// Download 下载固件文件
func (ctrl *FirmwareRepositoryController) Download(c *gin.Context) {
	token := c.Param("token")
	if token == "" {
		response.Fail(c, "token 不能为空")
		return
	}

	// 消费 token（原子操作）
	snapshot, err := ctrl.tokenService.Consume(c.Request.Context(), token)
	if err != nil {
		if err == firmware.ErrDownloadTokenExpired {
			response.Fail(c, "下载链接已过期")
			return
		}
		response.Fail(c, "token 无效或已使用")
		return
	}

	// 查询固件记录（快照里存的是业务固件号 firmware_id，不是数据库主键）
	var record gbmodels.GbFirmwareRepository
	if err := ctrl.db.Model(&gbmodels.GbFirmwareRepository{}).Where("firmware_id = ?", snapshot.FirmwareID).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// ⛔ 固件已被删除（token 还在有效期内）。必须报"固件不存在"，
			//   不能落到下面的 403 —— 403 会被读成"权限问题"，把真实原因指错方向
			//   （这正是 2026-10-06 那个 403 让人查错方向的原因：真因是 ID 语义错配）。
			response.Fail(c, "固件不存在或已被删除")
			return
		}
		response.Fail(c, "查询失败: "+err.Error())
		return
	}

	// 二次校验：token 绑定的部门必须能访问该固件（租户隔离）
	if record.DeptID != snapshot.DeptID {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    http.StatusForbidden,
			"message": "无权下载此固件（当前固件不属于你的部门）",
		})
		return
	}

	// 读取文件并流式返回
	file, err := os.Open(record.StoragePath)
	if err != nil {
		if os.IsNotExist(err) {
			// 数据库有记录但磁盘文件没了（手工清理过 / 上传中断）—— 说清是哪一种
			response.Fail(c, "固件文件已丢失，请重新上传")
			return
		}
		response.Fail(c, "文件读取失败")
		return
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		response.Fail(c, "文件信息读取失败")
		return
	}

	c.Header("Content-Description", "File Transfer")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", record.FileName))
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Length", strconv.FormatInt(fileInfo.Size(), 10))

	io.Copy(c.Writer, file)
}
