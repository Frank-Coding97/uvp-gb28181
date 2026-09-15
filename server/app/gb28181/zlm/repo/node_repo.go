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
	ID                  int64     `gorm:"primaryKey;column:id"`
	Revision            uint64    `gorm:"column:revision;not null;default:1"`
	Name                string    `gorm:"column:name;size:64;not null;default:''"`
	Host                string    `gorm:"column:host;size:64;not null;default:''"`
	ReceiveHost         string    `gorm:"column:receive_host;size:255;not null;default:''"`
	PlaybackHost        string    `gorm:"column:playback_host;size:255;not null;default:''"`
	APIPort             int       `gorm:"column:api_port;not null;default:18080"`
	APISecret           string    `gorm:"column:api_secret;size:128;not null;default:''"`
	MediaServerUUID     string    `gorm:"column:media_server_uuid;size:64;not null;default:'';uniqueIndex:uk_media_server_uuid"`
	Weight              int       `gorm:"column:weight;not null;default:50"`
	TagsJSON            string    `gorm:"column:tags_json;type:text"`
	State               string    `gorm:"column:state;size:16;not null;default:'active';index:idx_state"`
	RecoveryRequired    bool      `gorm:"column:recovery_required;not null;default:false;index:idx_recovery_required"`
	RecoveryReason      string    `gorm:"column:recovery_reason;size:255;not null;default:''"`
	RecoveryFingerprint string    `gorm:"column:recovery_fingerprint;size:64;not null;default:''"`
	RTPPortStart        int       `gorm:"column:rtp_port_start;not null;default:30000"`
	RTPPortEnd          int       `gorm:"column:rtp_port_end;not null;default:35000"`
	CreatedAt           time.Time `gorm:"column:created_at"`
	UpdatedAt           time.Time `gorm:"column:updated_at"`
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
		ID:                  m.ID,
		Revision:            m.Revision,
		Name:                m.Name,
		Host:                m.Host,
		ReceiveHost:         m.ReceiveHost,
		PlaybackHost:        m.PlaybackHost,
		APIPort:             m.APIPort,
		APISecret:           m.APISecret,
		MediaServerUUID:     m.MediaServerUUID,
		Weight:              m.Weight,
		Tags:                tags,
		State:               node.State(m.State),
		RecoveryRequired:    m.RecoveryRequired,
		RecoveryReason:      m.RecoveryReason,
		RecoveryFingerprint: m.RecoveryFingerprint,
		RTPPortStart:        m.RTPPortStart,
		RTPPortEnd:          m.RTPPortEnd,
		CreatedAt:           m.CreatedAt,
		UpdatedAt:           m.UpdatedAt,
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
		ID:                  n.ID,
		Revision:            n.Revision,
		Name:                n.Name,
		Host:                n.Host,
		ReceiveHost:         n.ReceiveHost,
		PlaybackHost:        n.PlaybackHost,
		APIPort:             n.APIPort,
		APISecret:           n.APISecret,
		MediaServerUUID:     n.MediaServerUUID,
		Weight:              n.Weight,
		TagsJSON:            tagsJSON,
		State:               string(n.State),
		RecoveryRequired:    n.RecoveryRequired,
		RecoveryReason:      n.RecoveryReason,
		RecoveryFingerprint: n.RecoveryFingerprint,
		RTPPortStart:        n.RTPPortStart,
		RTPPortEnd:          n.RTPPortEnd,
		CreatedAt:           n.CreatedAt,
		UpdatedAt:           n.UpdatedAt,
	}
}

// MetaNodeRepo gorm 实现 node.Repo 接口
type MetaNodeRepo struct {
	db *gorm.DB
}

func nextRevision(revision uint64) uint64 {
	if revision == ^uint64(0) {
		return revision
	}
	return revision + 1
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
	if row.Revision == 0 {
		row.Revision = 1
	}
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

// UpdateCAS updates a complete meta_node row only when its persisted revision
// still equals expectedRevision. A successful write advances revision by one;
// callers therefore get a durable linearization point shared with heartbeat
// state transitions and service updates.
func (r *MetaNodeRepo) UpdateCAS(ctx context.Context, n node.Node, expectedRevision uint64) (bool, error) {
	row := fromDomain(n)
	row.Revision = nextRevision(expectedRevision)
	row.UpdatedAt = time.Now()
	updates := map[string]any{
		"revision":             row.Revision,
		"name":                 row.Name,
		"host":                 row.Host,
		"receive_host":         row.ReceiveHost,
		"playback_host":        row.PlaybackHost,
		"api_port":             row.APIPort,
		"api_secret":           row.APISecret,
		"media_server_uuid":    row.MediaServerUUID,
		"weight":               row.Weight,
		"tags_json":            row.TagsJSON,
		"state":                row.State,
		"recovery_required":    row.RecoveryRequired,
		"recovery_reason":      row.RecoveryReason,
		"recovery_fingerprint": row.RecoveryFingerprint,
		"rtp_port_start":       row.RTPPortStart,
		"rtp_port_end":         row.RTPPortEnd,
		"created_at":           row.CreatedAt,
		"updated_at":           row.UpdatedAt,
	}
	result := r.db.WithContext(ctx).Model(&MetaNode{}).
		Where("id = ? AND revision = ?", n.ID, expectedRevision).
		Updates(updates)
	return result.RowsAffected == 1, result.Error
}

// Delete 硬删
func (r *MetaNodeRepo) Delete(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).Delete(&MetaNode{}, id).Error
}
