package models

import (
	"context"
	"uvplatform.com/uvp-gb28181/app/global/app"

	"gorm.io/gorm"
)

// SysAffixChunk 分片上传临时记录
type SysAffixChunk struct {
	BaseModel
	UploadId    string `gorm:"type:varchar(64);index;comment:上传会话ID" json:"uploadId"`
	FileMd5     string `gorm:"type:varchar(32);index;comment:文件MD5" json:"fileMd5"`
	FileName    string `gorm:"type:varchar(255);comment:原始文件名" json:"fileName"`
	FileSize    int64  `gorm:"type:bigint;comment:文件总大小" json:"fileSize"`
	ChunkSize   int    `gorm:"type:int;comment:分片大小" json:"chunkSize"`
	TotalChunks int    `gorm:"type:int;comment:总分片数" json:"totalChunks"`
	ChunkIndex  int    `gorm:"type:int;comment:当前分片序号" json:"chunkIndex"`
	ChunkPath   string `gorm:"type:varchar(255);comment:分片文件路径" json:"chunkPath"`
	Status      int    `gorm:"type:tinyint;default:0;comment:0-上传中 1-已合并 2-已取消" json:"status"`
	CreatedBy   uint   `gorm:"type:int(11);comment:创建者ID" json:"createdBy"`
}

// SysAffixChunkList 分片上传记录列表
type SysAffixChunkList []SysAffixChunk

// NewSysAffixChunk 创建分片记录实例
func NewSysAffixChunk() *SysAffixChunk {
	return &SysAffixChunk{}
}

// NewSysAffixChunkList 创建分片记录列表实例
func NewSysAffixChunkList() *SysAffixChunkList {
	return &SysAffixChunkList{}
}

// TableName 指定表名
func (SysAffixChunk) TableName() string {
	return "sys_affix_chunk"
}

// Create 创建分片记录
func (m *SysAffixChunk) Create(c context.Context) error {
	return app.DB().WithContext(c).Create(m).Error
}

// GetUploadedChunkIndexes 获取指定uploadId已上传的分片序号列表
func GetUploadedChunkIndexes(c context.Context, uploadId string) ([]int, error) {
	var indexes []int
	err := app.DB().WithContext(c).Model(&SysAffixChunk{}).
		Where("upload_id = ? AND status = 0", uploadId).
		Pluck("chunk_index", &indexes).Error
	return indexes, err
}

// GetChunksByUploadId 获取指定 uploadId **属于该用户**的所有分片记录
//
// ⛔⛔ 必须带 created_by：uploadId 虽然是随机串，但它是随请求传回来的，
//
//	任何登录用户都能拿别人的 uploadId 合并出别人的文件（并把附件记到自己名下）。
//	归属过滤放在查询里，而不是"查出来再比对"——后者漏一个分支就是越权。
func GetChunksByUploadId(c context.Context, uploadId string, userID uint) (*SysAffixChunkList, error) {
	return getChunksByUploadId(app.DB().WithContext(c), uploadId, userID)
}

// getChunksByUploadId 是查询本身（与全局连接解耦，便于单测）。
func getChunksByUploadId(db *gorm.DB, uploadId string, userID uint) (*SysAffixChunkList, error) {
	list := NewSysAffixChunkList()
	err := db.Where("upload_id = ? AND status = 0 AND created_by = ?", uploadId, userID).
		Order("chunk_index ASC").
		Find(list).Error
	return list, err
}

// GetChunkOwnerID 返回某个 uploadId 的归属用户。
//
// found=false 表示该会话还没有任何分片记录（首次上传的第一个分片）——
// 此时没有"别人的会话"可被顶替，调用方按允许处理。
func GetChunkOwnerID(c context.Context, uploadId string) (userID uint, found bool, err error) {
	return getChunkOwnerID(app.DB().WithContext(c), uploadId)
}

func getChunkOwnerID(db *gorm.DB, uploadId string) (userID uint, found bool, err error) {
	var owners []uint
	err = db.Model(&SysAffixChunk{}).
		Where("upload_id = ?", uploadId).
		Limit(1).
		Pluck("created_by", &owners).Error
	if err != nil {
		return 0, false, err
	}
	if len(owners) == 0 {
		return 0, false, nil
	}
	return owners[0], true, nil
}

// UpdateChunkStatus 更新指定uploadId的所有分片状态
func UpdateChunkStatus(c context.Context, uploadId string, status int) error {
	return app.DB().WithContext(c).
		Model(&SysAffixChunk{}).
		Where("upload_id = ?", uploadId).
		Update("status", status).Error
}

// DeleteChunksByUploadId 删除指定uploadId的所有分片记录
func DeleteChunksByUploadId(c context.Context, uploadId string) error {
	return app.DB().WithContext(c).
		Where("upload_id = ?", uploadId).
		Delete(&SysAffixChunk{}).Error
}

// GetAffixByMd5 根据MD5查找是否已有完整文件（秒传检测）
//
// ⛔ 秒传必须**按用户**命中：过去只按 file_md5+size 查全表，
//
//	返回值里带 path/url —— 只要知道别人文件的 MD5，就能读到它的存储地址。
//	跨用户去重会牺牲一点存储空间，但把「文件地址」收回了数据权限范围。
func GetAffixByMd5(c context.Context, fileMd5 string, fileSize int64, userID uint) (*SysAffix, error) {
	return getAffixByMd5(app.DB().WithContext(c), fileMd5, fileSize, userID)
}

func getAffixByMd5(db *gorm.DB, fileMd5 string, fileSize int64, userID uint) (*SysAffix, error) {
	affix := NewSysAffix()
	err := db.Where("file_md5 = ? AND size = ? AND created_by = ?", fileMd5, fileSize, userID).
		First(affix).Error
	if err != nil {
		return nil, err
	}
	return affix, nil
}
