package models

import (
	"context"
	"uvplatform.com/uvp-gb28181/app/global/app"

	"gorm.io/gorm"
)

// SysAffix 文件附件模型
type SysAffix struct {
	BaseModel
	Name          string `gorm:"type:varchar(255);comment:文件名" json:"name"`
	Path          string `gorm:"type:varchar(255);comment:文件路径" json:"path"`
	Size          int    `gorm:"type:int(10);comment:文件大小(字节)" json:"size"`
	Ftype         string `gorm:"type:varchar(100);comment:文件类型" json:"ftype"`
	CreatedBy     uint   `gorm:"type:int(11);comment:创建者ID" json:"createdBy"`
	Suffix        string `gorm:"type:varchar(100);comment:文件后缀" json:"suffix"`
	Url           string `gorm:"type:varchar(255);comment:文件访问URL" json:"url"`
	FileMd5       string `gorm:"type:varchar(32);index;comment:文件MD5(秒传检测)" json:"fileMd5,omitempty"`
	ThumbnailPath string `gorm:"type:varchar(255);comment:缩略图路径" json:"thumbnailPath,omitempty"`
	ThumbnailName string `gorm:"type:varchar(255);comment:缩略图名称" json:"thumbnailName,omitempty"`
	ThumbnailUrl  string `gorm:"type:varchar(255);comment:缩略图URL" json:"thumbnailUrl,omitempty"`
	// 添加与User模型的关联
	User User `gorm:"foreignKey:id;references:created_by" json:"user"`
}

// SysAffixList 文件附件列表
type SysAffixList []SysAffix

// NewSysAffix 创建文件附件实例
func NewSysAffix() *SysAffix {
	return &SysAffix{}
}

// NewSysAffixList 创建文件附件列表实例
func NewSysAffixList() *SysAffixList {
	return &SysAffixList{}
}

// TableName 指定表名
func (SysAffix) TableName() string {
	return "sys_affix"
}

// GetByID 根据ID获取文件附件（**不套数据权限**，仅供内部/后台任务使用）
//
// ⛔⛔ 面向用户的按 ID 接口一律用 GetByIDScoped：
//
//	列表接口走 datascope、按 ID 接口却直查 —— 这个不对称就是越权本身
//	（枚举 ID 读别人附件的 path/url，甚至改名、删除）。
func (m *SysAffix) GetByID(c context.Context, id uint) error {
	return app.DB().WithContext(c).First(m, id).Error
}

// GetByIDScoped 按 ID 查询，并强制套用调用方给出的数据权限范围。
//
// scope 为 nil 时等价于不加范围（调用方通常是没拿到 claims 的兜底分支）；
// 面向用户的控制器必须传 datascope.GetDataScope(c)。
func (m *SysAffix) GetByIDScoped(c context.Context, id uint, scope func(*gorm.DB) *gorm.DB) error {
	return m.getByIDScoped(app.DB().WithContext(c), id, scope)
}

// getByIDScoped 是查询本身（与全局连接解耦，便于单测）。
func (m *SysAffix) getByIDScoped(db *gorm.DB, id uint, scope func(*gorm.DB) *gorm.DB) error {
	if scope != nil {
		db = db.Scopes(scope)
	}
	if err := db.First(m, id).Error; err != nil {
		return err
	}
	// ⛔ 全仓的 GORM 回调可能把 not-found 掩成「零值 + nil error」，
	//   这里显式再判一次主键，避免"查不到"被当成"查到了 ID=0 的记录"。
	if m.ID == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// Create 创建文件附件记录
func (m *SysAffix) Create(c context.Context) error {
	return app.DB().WithContext(c).Create(m).Error
}

// Update 更新文件附件记录
func (m *SysAffix) Update(c context.Context) error {
	return app.DB().WithContext(c).Save(m).Error
}

// Delete 软删除文件附件记录
func (m *SysAffix) Delete(c context.Context) error {
	return app.DB().WithContext(c).Delete(m).Error
}

// IsEmpty 检查模型是否为空
func (m *SysAffix) IsEmpty() bool {
	return m.ID == 0
}

// Find 查询文件附件列表
func (l *SysAffixList) Find(c context.Context, funcs ...func(*gorm.DB) *gorm.DB) error {
	return app.DB().WithContext(c).Model(&SysAffix{}).Scopes(funcs...).Find(l).Error
}

// GetTotal 获取文件附件总数
func (l *SysAffixList) GetTotal(c context.Context, query ...func(*gorm.DB) *gorm.DB) (int64, error) {
	var count int64
	err := app.DB().WithContext(c).Model(&SysAffix{}).Scopes(query...).Count(&count).Error
	return count, err
}
