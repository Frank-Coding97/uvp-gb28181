package workrecording

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

const (
	OwnerWork       = "work_job"
	OwnerContinuous = "continuous"
	OwnerPlan       = "plan"
	OwnerManagement = "management"
)

type Owner struct {
	Kind string
	ID   string
}

func (o Owner) valid() bool {
	switch o.Kind {
	case OwnerWork, OwnerContinuous, OwnerPlan, OwnerManagement:
	default:
		return false
	}
	return o.ID != "" && len(o.ID) <= 128 && strings.TrimSpace(o.ID) == o.ID
}

type Claims struct{ db *gorm.DB }

func NewClaims(db *gorm.DB) *Claims { return &Claims{db: db} }

func resource(parts ...any) string {
	data, _ := json.Marshal(parts)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func ChannelResource(channelID uint) string { return resource("channel", channelID, "mp4") }
func MediaResource(nodeID int64, vhost, app, stream string) string {
	return resource("media", nodeID, vhost, app, stream, "mp4")
}

// Acquire is idempotent only for the same owner and stream generation. Unknown
// or stale claims never expire into someone else's ownership.
func (s *Claims) Acquire(ctx context.Context, key string, owner Owner, generation uint64) (*models.GbRecorderClaim, error) {
	if key == "" || len(key) > 64 || !owner.valid() {
		return nil, ErrInvalidRequest
	}
	row := models.GbRecorderClaim{ResourceKey: key, OwnerKind: owner.Kind, OwnerID: owner.ID, Generation: generation, State: StateStarting, Version: 1}
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "resource_key"}}, DoNothing: true}).Create(&row).Error; err != nil {
		return nil, err
	}
	// Retain released rows so versions cannot restart and let a delayed
	// request from an earlier run match the next run of the same owner.
	reused := s.db.WithContext(ctx).Model(&models.GbRecorderClaim{}).
		Where("resource_key = ? AND state = ?", key, StateIdle).
		Updates(map[string]any{"owner_kind": owner.Kind, "owner_id": owner.ID, "generation": generation, "state": StateStarting, "version": gorm.Expr("version + 1")})
	if reused.Error != nil {
		return nil, reused.Error
	}
	current, err := s.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	if current.OwnerKind != owner.Kind || current.OwnerID != owner.ID {
		return nil, ErrOwnerConflict
	}
	if current.Generation != generation {
		return nil, ErrVersionConflict
	}
	return current, nil
}

func (s *Claims) Get(ctx context.Context, key string) (*models.GbRecorderClaim, error) {
	var row models.GbRecorderClaim
	if err := s.db.WithContext(ctx).First(&row, "resource_key = ?", key).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Claims) owned(ctx context.Context, key string, owner Owner, version uint64) (*models.GbRecorderClaim, error) {
	if !owner.valid() {
		return nil, ErrInvalidRequest
	}
	row, err := s.Get(ctx, key)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrOwnerConflict
	}
	if err != nil {
		return nil, err
	}
	if row.OwnerKind != owner.Kind || row.OwnerID != owner.ID {
		return nil, ErrOwnerConflict
	}
	if row.Version != version {
		return nil, ErrVersionConflict
	}
	return row, nil
}

func claimTransition(from, to string) bool {
	switch from {
	case StateStarting:
		return to == StateRecording || to == StateUnknown || to == StateStopping
	case StateRecording:
		return to == StateStopping || to == StateUnknown
	case StateUnknown:
		return to == StateRecording || to == StateStopping || to == StateStopped
	case StateStopping:
		return to == StateUnknown || to == StateStopped
	}
	return false
}

func (s *Claims) Transition(ctx context.Context, key string, owner Owner, version uint64, state string) (*models.GbRecorderClaim, error) {
	row, err := s.owned(ctx, key, owner, version)
	if err != nil {
		return nil, err
	}
	if !claimTransition(row.State, state) {
		return nil, ErrInvalidRequest
	}
	result := s.db.WithContext(ctx).Model(&models.GbRecorderClaim{}).Where("resource_key = ? AND owner_kind = ? AND owner_id = ? AND version = ?", key, owner.Kind, owner.ID, version).Updates(map[string]any{"state": state, "version": version + 1})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, ErrVersionConflict
	}
	return s.Get(ctx, key)
}

// Release requires an explicitly resolved stopped claim. The caller must also
// resolve file finalization before advancing the claim to stopped.
func (s *Claims) Release(ctx context.Context, key string, owner Owner, version uint64) error {
	row, err := s.owned(ctx, key, owner, version)
	if err != nil {
		return err
	}
	if row.State != StateStopped {
		return ErrVersionConflict
	}
	result := s.db.WithContext(ctx).Model(&models.GbRecorderClaim{}).Where("resource_key = ? AND owner_kind = ? AND owner_id = ? AND version = ? AND state = ?", key, owner.Kind, owner.ID, version, StateStopped).Updates(map[string]any{"state": StateIdle, "owner_kind": "", "owner_id": "", "version": version + 1})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrVersionConflict
	}
	return nil
}
