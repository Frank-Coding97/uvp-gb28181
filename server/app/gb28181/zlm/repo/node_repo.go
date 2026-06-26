package repo

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// MetaNode gorm 模型,对应 meta_node 表
// 字段保持跟 meta_node.sql 同名(snake_case via gorm column tag)。
type MetaNode struct {
	ID              int64     `gorm:"primaryKey;column:id"`
	Name            string    `gorm:"column:name;size:64;not null;default:''"`
	Host            string    `gorm:"column:host;size:64;not null;default:''"`
	APIPort         int       `gorm:"column:api_port;not null;default:18080"`
	APISecret       string    `gorm:"column:api_secret;size:128;not null;default:''"`
	MediaServerUUID string    `gorm:"column:media_server_uuid;size:64;not null;default:'';uniqueIndex:uk_media_server_uuid"`
	Weight          int       `gorm:"column:weight;not null;default:50"`
	TagsJSON        string    `gorm:"column:tags_json;type:text"`
	State           string    `gorm:"column:state;size:16;not null;default:'active';index:idx_state"`
	RTPPortStart    int       `gorm:"column:rtp_port_start;not null;default:30000"`
	RTPPortEnd      int       `gorm:"column:rtp_port_end;not null;default:35000"`
	CreatedAt       time.Time `gorm:"column:created_at"`
	UpdatedAt       time.Time `gorm:"column:updated_at"`
}

// TableName 显式表名(不走 gorm 的 pluralize 复数化)
func (MetaNode) TableName() string { return "meta_node" }

// ToDomain MetaNode 行 → 业务侧 node.Node
func (m MetaNode) ToDomain() node.Node {
	tags := map[string]string{}
	if m.TagsJSON != "" {
		_ = json.Unmarshal([]byte(m.TagsJSON), &tags)
	}
	return node.Node{
		ID:              m.ID,
		Name:            m.Name,
		Host:            m.Host,
		APIPort:         m.APIPort,
		APISecret:       m.APISecret,
		MediaServerUUID: m.MediaServerUUID,
		Weight:          m.Weight,
		Tags:            tags,
		State:           node.State(m.State),
		RTPPortStart:    m.RTPPortStart,
		RTPPortEnd:      m.RTPPortEnd,
		CreatedAt:       m.CreatedAt,
		UpdatedAt:       m.UpdatedAt,
	}
}

// fromDomain node.Node → MetaNode 行(不带 ID,Create 用)
func fromDomain(n node.Node) MetaNode {
	var tagsJSON string
	if len(n.Tags) > 0 {
		b, _ := json.Marshal(n.Tags)
		tagsJSON = string(b)
	}
	return MetaNode{
		ID:              n.ID,
		Name:            n.Name,
		Host:            n.Host,
		APIPort:         n.APIPort,
		APISecret:       n.APISecret,
		MediaServerUUID: n.MediaServerUUID,
		Weight:          n.Weight,
		TagsJSON:        tagsJSON,
		State:           string(n.State),
		RTPPortStart:    n.RTPPortStart,
		RTPPortEnd:      n.RTPPortEnd,
		CreatedAt:       n.CreatedAt,
		UpdatedAt:       n.UpdatedAt,
	}
}

// MetaNodeRepo gorm 实现 node.Repo 接口
type MetaNodeRepo struct {
	db *gorm.DB
}

// NewMetaNodeRepo 构造
func NewMetaNodeRepo(db *gorm.DB) *MetaNodeRepo {
	return &MetaNodeRepo{db: db}
}

// List 所有节点按 ID 升序
func (r *MetaNodeRepo) List(ctx context.Context) ([]node.Node, error) {
	var rows []MetaNode
	if err := r.db.WithContext(ctx).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]node.Node, 0, len(rows))
	for _, m := range rows {
		out = append(out, m.ToDomain())
	}
	return out, nil
}

// Get 按 ID 取,未找到返回 (nil, nil)
func (r *MetaNodeRepo) Get(ctx context.Context, id int64) (*node.Node, error) {
	var m MetaNode
	err := r.db.WithContext(ctx).Where("id = ?", id).Limit(1).Find(&m).Error
	if err != nil {
		return nil, err
	}
	if m.ID == 0 {
		return nil, nil
	}
	d := m.ToDomain()
	return &d, nil
}

// Create 入库,返回自增 ID
func (r *MetaNodeRepo) Create(ctx context.Context, n node.Node) (int64, error) {
	row := fromDomain(n)
	if row.CreatedAt.IsZero() {
		row.CreatedAt = time.Now()
	}
	row.UpdatedAt = time.Now()
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return 0, err
	}
	return row.ID, nil
}

// Update 全字段更新(ID 必须)
func (r *MetaNodeRepo) Update(ctx context.Context, n node.Node) error {
	row := fromDomain(n)
	row.UpdatedAt = time.Now()
	return r.db.WithContext(ctx).Save(&row).Error
}

// Delete 硬删
func (r *MetaNodeRepo) Delete(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).Delete(&MetaNode{}, id).Error
}
