package service

import (
	"fmt"

	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/models"
)

// RevokeAllForRestoreTx invalidates every historical login, including expired
// sessions and refresh material left on already revoked rows. The offline
// restore coordinator owns the transaction and must keep business traffic
// gated until credential rotation and the remaining restore steps succeed.
func (s *AuthSessionService) RevokeAllForRestoreTx(tx *gorm.DB) error {
	if tx == nil {
		return fmt.Errorf("%w: missing restore transaction", ErrSessionStore)
	}
	now := s.now()
	result := tx.Model(&models.SysUserSession{}).Where("1 = 1").Updates(map[string]any{
		"revoke_reason":      gorm.Expr("CASE WHEN revoked_at IS NULL THEN ? ELSE revoke_reason END", "backup_restore"),
		"revoked_at":         gorm.Expr("COALESCE(revoked_at, ?)", now),
		"refresh_token_hash": nil, "refresh_jti": nil, "updated_at": now,
	})
	if result.Error != nil {
		return fmt.Errorf("%w: restore session revocation: %v", ErrSessionStore, result.Error)
	}
	return nil
}
