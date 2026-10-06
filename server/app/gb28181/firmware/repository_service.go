package firmware

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/datascope"
)

type CreateFirmwareRequest struct {
	FirmwareID   string
	Version      string
	Manufacturer string
	ModelPattern string
	FileName     string
	FileSize     int64
	FileHash     string
	StoragePath  string
	StorageKey   string
	ReleaseDate  *time.Time
	Status       gbmodels.FirmwareStatus
	UploadedBy   uint64
	DeptID       uint64
	Remark       string
}

type ListFirmwareRequest struct {
	Page         int
	PageSize     int
	Manufacturer string
	Status       gbmodels.FirmwareStatus
}

type RepositoryService struct {
	db *gorm.DB
}

func NewRepositoryService(db *gorm.DB) *RepositoryService {
	return &RepositoryService{db: db}
}

func (s *RepositoryService) Create(c *gin.Context, req CreateFirmwareRequest) (*gbmodels.GbFirmwareRepository, error) {
	record := &gbmodels.GbFirmwareRepository{
		FirmwareID:   req.FirmwareID,
		Version:      req.Version,
		Manufacturer: req.Manufacturer,
		ModelPattern: req.ModelPattern,
		FileName:     req.FileName,
		FileSize:     req.FileSize,
		FileHash:     req.FileHash,
		StoragePath:  req.StoragePath,
		StorageKey:   req.StorageKey,
		ReleaseDate:  req.ReleaseDate,
		Status:       req.Status,
		UploadedBy:   req.UploadedBy,
		DeptID:       req.DeptID,
		Remark:       req.Remark,
	}
	if err := s.db.WithContext(c.Request.Context()).Create(record).Error; err != nil {
		return nil, err
	}
	return record, nil
}

func (s *RepositoryService) List(c *gin.Context, req ListFirmwareRequest) ([]gbmodels.GbFirmwareRepository, int64, error) {
	query := s.db.WithContext(c.Request.Context()).
		Model(&gbmodels.GbFirmwareRepository{}).
		Scopes(datascope.OwnerDeptScopeWithDB(c, s.db, "dept_id"))

	// 过滤条件
	if req.Manufacturer != "" {
		query = query.Where("manufacturer = ?", req.Manufacturer)
	}
	if req.Status != "" {
		query = query.Where("status = ?", req.Status)
	}

	// 计数
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页
	if req.Page < 1 {
		req.Page = 1
	}
	if req.PageSize < 1 {
		req.PageSize = 10
	}
	offset := (req.Page - 1) * req.PageSize

	var records []gbmodels.GbFirmwareRepository
	if err := query.Order("created_at DESC").Offset(offset).Limit(req.PageSize).Find(&records).Error; err != nil {
		return nil, 0, err
	}

	return records, total, nil
}

func (s *RepositoryService) GetByID(c *gin.Context, id uint64) (*gbmodels.GbFirmwareRepository, error) {
	var record gbmodels.GbFirmwareRepository
	result := s.db.WithContext(c.Request.Context()).
		Scopes(datascope.OwnerDeptScopeWithDB(c, s.db, "dept_id")).
		Where("id = ?", id).
		First(&record)
	if result.Error != nil {
		return nil, result.Error
	}
	return &record, nil
}

func (s *RepositoryService) GetByFirmwareID(c *gin.Context, firmwareID string) (*gbmodels.GbFirmwareRepository, error) {
	var record gbmodels.GbFirmwareRepository
	result := s.db.WithContext(c.Request.Context()).
		Scopes(datascope.OwnerDeptScopeWithDB(c, s.db, "dept_id")).
		Where("firmware_id = ?", firmwareID).
		First(&record)
	if result.Error != nil {
		return nil, result.Error
	}
	return &record, nil
}

// GetByFirmwareIDWithDept 通过固件 ID 和部门 ID 查询固件（用于非 HTTP 上下文）
func (s *RepositoryService) GetByFirmwareIDWithDept(ctx context.Context, firmwareID string, deptID uint64) (*gbmodels.GbFirmwareRepository, error) {
	var record gbmodels.GbFirmwareRepository
	result := s.db.WithContext(ctx).
		Where("firmware_id = ? AND dept_id = ?", firmwareID, deptID).
		First(&record)
	if result.Error != nil {
		return nil, result.Error
	}
	return &record, nil
}

// UpdateStatus 更新固件状态
func (s *RepositoryService) UpdateStatus(c *gin.Context, id uint64, status gbmodels.FirmwareStatus) error {
	result := s.db.WithContext(c).
		Model(&gbmodels.GbFirmwareRepository{}).
		Scopes(datascope.OwnerDeptScopeWithDB(c, s.db, "dept_id")).
		Where("id = ?", id).
		Update("status", status)

	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// ErrFirmwareInUse 固件已被设备升级记录引用，不允许删除
var ErrFirmwareInUse = errors.New("firmware: 已被设备升级记录引用，无法删除")

// CountUpgradeReferences 统计引用该固件的设备升级记录数（按 firmware_id 关联）
func (s *RepositoryService) CountUpgradeReferences(c *gin.Context, firmwareID string) (int64, error) {
	var count int64
	err := s.db.WithContext(c.Request.Context()).
		Model(&gbmodels.GbDeviceFirmwareUpgrade{}).
		Where("firmware_id = ?", firmwareID).
		Count(&count).Error
	return count, err
}

// Delete 物理删除固件记录与其磁盘文件。
//
// ⛔⛔ 这里曾经是「软删除」——只把 status 改成 archived 就返回成功。列表默认不过滤状态，
//   于是用户点删除看到"删除成功"，刷新后那条记录还好端端躺在台账里（问题 2）。
//   归档是另一个独立动作（POST .../status { status: "archived" }），两者不能混为一谈：
//   归档保留文件供历史追溯，删除必须真的消失。
//
// ⛔ 被设备升级记录引用（gb_device_firmware_upgrade.firmware_id）时**明确拒绝**，
//   而不是级联删掉历史记录 —— 升级历史是审计凭证，不能因为清理仓库被抹掉。
func (s *RepositoryService) Delete(c *gin.Context, id uint64) error {
	db := s.db.WithContext(c.Request.Context())

	// 1) 先取记录（需要 firmware_id 与 storage_path）
	var record gbmodels.GbFirmwareRepository
	if err := db.Scopes(datascope.OwnerDeptScopeWithDB(c, s.db, "dept_id")).
		Where("id = ?", id).
		First(&record).Error; err != nil {
		return err
	}

	// 2) 有升级记录引用 ⇒ 拒绝
	refs, err := s.CountUpgradeReferences(c, record.FirmwareID)
	if err != nil {
		return err
	}
	if refs > 0 {
		return fmt.Errorf("%w（%d 条设备升级记录）", ErrFirmwareInUse, refs)
	}

	// 3) 删数据库记录（确认过 RowsAffected，避免"以为删了其实没删"）
	result := db.Where("id = ?", id).Delete(&gbmodels.GbFirmwareRepository{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}

	// 4) 记录已消失后清理磁盘文件（失败只记日志，不回滚 —— 库里已无引用，
	//    残留文件是运维问题，不该让用户以为删除失败）
	if record.StoragePath != "" {
		if err := os.Remove(record.StoragePath); err != nil && !os.IsNotExist(err) {
			app.ZapLog.Warn("固件记录已删除但文件清理失败",
				zap.String("storagePath", record.StoragePath),
				zap.Error(err))
		}
	}
	return nil
}
