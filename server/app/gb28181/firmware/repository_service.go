package firmware

import (
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
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

func (s *RepositoryService) Delete(c *gin.Context, id uint64) error {
	// 软删除: 状态改为 archived
	result := s.db.WithContext(c.Request.Context()).
		Model(&gbmodels.GbFirmwareRepository{}).
		Scopes(datascope.OwnerDeptScopeWithDB(c, s.db, "dept_id")).
		Where("id = ?", id).
		Update("status", gbmodels.FirmwareStatusArchived)

	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
