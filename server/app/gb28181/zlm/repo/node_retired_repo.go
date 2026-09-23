package repo

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// MetaNodeRetired gorm 模型,对应 meta_node_retired 表。
//
// 这张表只装一件事:**已删除节点的"撤销对端 hook"凭据**。
// `meta_node` 的行在删除时被物理删除(见 Registry.Delete),而撤销对端的 hook 需要
// uuid + host + api_port + api_secret —— 缺了凭据,平台就再也关不掉那个仍在回调的实例。
// 语义与设计动机见迁移 `2026-09-21-retired-media-node-credentials.sql`。
type MetaNodeRetired struct {
	ID                  int64      `gorm:"primaryKey;column:id"`
	MediaServerUUID     string     `gorm:"column:media_server_uuid;size:64;not null;default:''"`
	Name                string     `gorm:"column:name;size:64;not null;default:''"`
	Host                string     `gorm:"column:host;size:64;not null;default:''"`
	APIPort             int        `gorm:"column:api_port;not null;default:18080"`
	APISecret           string     `gorm:"column:api_secret;size:128;not null;default:''"`
	RetireReason        string     `gorm:"column:retire_reason;size:255;not null;default:''"`
	UnprovisionState    string     `gorm:"column:unprovision_state;size:16;not null;default:'pending'"`
	UnprovisionAttempts int        `gorm:"column:unprovision_attempts;not null;default:0"`
	LastAttemptAt       *time.Time `gorm:"column:last_attempt_at"`
	RetiredAt           time.Time  `gorm:"column:retired_at;not null"`
	UpdatedAt           time.Time  `gorm:"column:updated_at;not null"`
}

// TableName 显式表名(不走 gorm 的 pluralize 复数化)
func (MetaNodeRetired) TableName() string { return "meta_node_retired" }

// MetaNodeRetiredRepo 实现 node.RetirementRepo。
//
// 三个方法都刻意保持"方言中立":不依赖 upsert / RETURNING 等各库形态不同的语法
// (本仓要跑 MySQL / PostgreSQL / SQL Server / SQLite),读后写两个语句足够 ——
// 退役是"删一个节点才发生一次"的低频动作,不值得为它换方言兼容性。
type MetaNodeRetiredRepo struct {
	db *gorm.DB
}

func NewMetaNodeRetiredRepo(db *gorm.DB) *MetaNodeRetiredRepo {
	return &MetaNodeRetiredRepo{db: db}
}

// ListRetired 全量(启动时装载索引用);按 uuid 升序保证行为稳定。
func (r *MetaNodeRetiredRepo) ListRetired(ctx context.Context) ([]node.RetiredCredential, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("retired node repo unavailable")
	}
	var rows []MetaNodeRetired
	if err := r.db.WithContext(ctx).Order("media_server_uuid").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]node.RetiredCredential, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toCredential())
	}
	return out, nil
}

// SaveRetired 按 uuid 幂等写入。
func (r *MetaNodeRetiredRepo) SaveRetired(ctx context.Context, cred node.RetiredCredential) error {
	if r == nil || r.db == nil {
		return errors.New("retired node repo unavailable")
	}
	if cred.MediaServerUUID == "" {
		return errors.New("retired node uuid required")
	}
	if cred.UnprovisionState == "" {
		cred.UnprovisionState = node.RetireUnprovisionPending
	}
	now := time.Now()
	if cred.RetiredAt.IsZero() {
		cred.RetiredAt = now
	}
	existing, err := r.findByUUID(ctx, cred.MediaServerUUID)
	if err != nil {
		return err
	}
	if existing == nil {
		return r.db.WithContext(ctx).Create(&MetaNodeRetired{
			MediaServerUUID:     cred.MediaServerUUID,
			Name:                cred.Name,
			Host:                cred.Host,
			APIPort:             cred.APIPort,
			APISecret:           cred.APISecret,
			RetireReason:        cred.RetireReason,
			UnprovisionState:    cred.UnprovisionState,
			UnprovisionAttempts: cred.UnprovisionAttempts,
			LastAttemptAt:       cred.LastAttemptAt,
			RetiredAt:           cred.RetiredAt,
			UpdatedAt:           now,
		}).Error
	}
	// 同一个 uuid 被重新加入 → 再被移除，是一次**新的退役事件**：刷凭据与理由，
	// 并把解约状态推回 pending（否则会继承上一次的 done，从此不再尝试撤销，
	// 而重新加入时 ApplyConfigForNode 可能已经把 hook 又写回去了）。
	return r.db.WithContext(ctx).Model(&MetaNodeRetired{}).
		Where("id = ?", existing.ID).
		Updates(map[string]any{
			"name":                 cred.Name,
			"host":                 cred.Host,
			"api_port":             cred.APIPort,
			"api_secret":           cred.APISecret,
			"retire_reason":        cred.RetireReason,
			"unprovision_state":    cred.UnprovisionState,
			"unprovision_attempts": cred.UnprovisionAttempts,
			"last_attempt_at":      cred.LastAttemptAt,
			"updated_at":           now,
		}).Error
}

// UpdateRetiredUnprovision 记一次解约尝试的结果。
func (r *MetaNodeRetiredRepo) UpdateRetiredUnprovision(ctx context.Context, uuid, state string, attempts int, at time.Time) error {
	if r == nil || r.db == nil {
		return errors.New("retired node repo unavailable")
	}
	return r.db.WithContext(ctx).Model(&MetaNodeRetired{}).
		Where("media_server_uuid = ?", uuid).
		Updates(map[string]any{
			"unprovision_state":    state,
			"unprovision_attempts": attempts,
			"last_attempt_at":      at,
			"updated_at":           at,
		}).Error
}

func (r *MetaNodeRetiredRepo) findByUUID(ctx context.Context, uuid string) (*MetaNodeRetired, error) {
	var row MetaNodeRetired
	err := r.db.WithContext(ctx).Where("media_server_uuid = ?", uuid).Limit(1).Find(&row).Error
	if err != nil {
		return nil, err
	}
	if row.ID == 0 {
		return nil, nil
	}
	return &row, nil
}

func (row MetaNodeRetired) toCredential() node.RetiredCredential {
	return node.RetiredCredential{
		MediaServerUUID:     row.MediaServerUUID,
		Name:                row.Name,
		Host:                row.Host,
		APIPort:             row.APIPort,
		APISecret:           row.APISecret,
		RetireReason:        row.RetireReason,
		UnprovisionState:    row.UnprovisionState,
		UnprovisionAttempts: row.UnprovisionAttempts,
		LastAttemptAt:       row.LastAttemptAt,
		RetiredAt:           row.RetiredAt,
	}
}

// 编译期断言：repo 必须满足 node 包声明的持久化契约。
var _ node.RetirementRepo = (*MetaNodeRetiredRepo)(nil)
