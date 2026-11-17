package videoshare

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// RevokeCreatorTx permanently retires shares owned by actorID. Bounded reads
// avoid retaining every creator share in memory; all batches share the caller's
// authority transaction, so an audit failure rolls back the entire revocation.
func RevokeCreatorTx(ctx context.Context, tx *gorm.DB, actorID uint, reason string, now time.Time) error {
	if ctx == nil || tx == nil || tx.Statement == nil || actorID == 0 {
		return errors.New("video share creator revocation requires context, transaction, and actor")
	}
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return errors.New("video share creator revocation requires an explicit transaction")
	}
	if reason == "" {
		reason = "creator_access_changed"
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	for {
		var shares []gbmodels.GbChannelVideoShare
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("created_by = ? AND enabled = ? AND revoked_at IS NULL", actorID, true).
			Order("id ASC").Limit(100).Find(&shares).Error; err != nil {
			return err
		}
		if len(shares) == 0 {
			return nil
		}
		for i := range shares {
			if err := revokeShareWithAuditTx(ctx, tx, &shares[i], reason, now); err != nil {
				return err
			}
		}
	}
}

// No operator is supplied by these system revocation paths. created_by is the
// affected creator, not the administrator making the authority change.
func revokeShareWithAuditTx(ctx context.Context, tx *gorm.DB, share *gbmodels.GbChannelVideoShare, reason string, now time.Time) error {
	updated := tx.WithContext(ctx).Model(&gbmodels.GbChannelVideoShare{}).
		Where("id = ? AND revision = ? AND generation = ? AND enabled = ? AND revoked_at IS NULL", share.ID, share.Revision, share.Generation, true).
		Updates(map[string]any{"enabled": false, "revoked_at": now, "revoke_reason": reason, "revision": share.Revision + 1, "updated_at": now})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return ErrConflict
	}
	share.Enabled, share.RevokedAt, share.RevokeReason, share.Revision = false, &now, reason, share.Revision+1
	return RecordAuditTx(ctx, tx, "video_share.revoked", 0, share)
}

// RoleCreatorIDsTx returns users assigned to roleID or any descendant role.
// A parent role policy affects each descendant through role inheritance.
func RoleCreatorIDsTx(ctx context.Context, tx *gorm.DB, roleID uint) ([]uint, error) {
	if ctx == nil || tx == nil || roleID == 0 {
		return nil, errors.New("video share role lookup requires context, transaction, and role")
	}
	var roles []struct {
		ID       uint
		ParentID uint `gorm:"column:parent_id"`
	}
	if err := tx.WithContext(ctx).Table("sys_role").Select("id, parent_id").Where("deleted_at IS NULL").Find(&roles).Error; err != nil {
		return nil, err
	}
	roleSet := map[uint]struct{}{roleID: {}}
	for changed := true; changed; {
		changed = false
		for _, role := range roles {
			if _, included := roleSet[role.ParentID]; !included {
				continue
			}
			if _, exists := roleSet[role.ID]; exists {
				continue
			}
			roleSet[role.ID] = struct{}{}
			changed = true
		}
	}
	roleIDs := make([]uint, 0, len(roleSet))
	for id := range roleSet {
		roleIDs = append(roleIDs, id)
	}
	var userIDs []uint
	if err := tx.WithContext(ctx).Table("sys_user_role").Distinct("user_id").Where("role_id IN ?", roleIDs).Pluck("user_id", &userIDs).Error; err != nil {
		return nil, err
	}
	return userIDs, nil
}

// RevokeUnauthorizedSharesTx permanently retires only shares whose creators
// no longer pass the current share, playback, and visibility authorization.
func RevokeUnauthorizedSharesTx(ctx context.Context, tx *gorm.DB, check PermissionChecker, actorIDs []uint, reason string, now time.Time) error {
	if ctx == nil || tx == nil || tx.Statement == nil || check == nil {
		return errors.New("video share permission revocation requires context, transaction, and checker")
	}
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return errors.New("video share permission revocation requires an explicit transaction")
	}
	if len(actorIDs) == 0 {
		return nil
	}
	if reason == "" {
		reason = "creator_authority_changed"
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	authorize := NewDatabaseAuthorizer(check)
	var shares []gbmodels.GbChannelVideoShare
	if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("created_by IN ? AND enabled = ? AND revoked_at IS NULL", actorIDs, true).Find(&shares).Error; err != nil {
		return err
	}
	for _, share := range shares {
		if share.CreatedBy == nil {
			continue
		}
		_, err := authorize(ctx, tx, *share.CreatedBy, share.ChannelPK)
		if err == nil {
			continue
		}
		if !errors.Is(err, ErrExpired) && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := revokeShareWithAuditTx(ctx, tx, &share, reason, now); err != nil {
			return err
		}
	}
	return nil
}

func RevokeAllUnauthorizedSharesTx(ctx context.Context, tx *gorm.DB, check PermissionChecker, reason string, now time.Time) error {
	if ctx == nil || tx == nil || check == nil {
		return errors.New("video share permission revocation requires context, transaction, and checker")
	}
	var actorIDs []uint
	if err := tx.WithContext(ctx).Model(&gbmodels.GbChannelVideoShare{}).
		Where("enabled = ? AND revoked_at IS NULL AND created_by IS NOT NULL", true).
		Distinct().Pluck("created_by", &actorIDs).Error; err != nil {
		return err
	}
	return RevokeUnauthorizedSharesTx(ctx, tx, check, actorIDs, reason, now)
}
