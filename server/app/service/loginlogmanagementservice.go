package service

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/models"
)

const maxLoginLogDeleteCount = 100

var (
	ErrLoginLogUnlockNotAllowed = errors.New("login log is not an account lock event")
	ErrLoginLogUnlockUser       = errors.New("login log user is unavailable")
	ErrLoginLogCacheUnavailable = errors.New("login log cache is unavailable")
)

type LoginLogManagementService struct {
	db    *gorm.DB
	cache app.CacheInterf
}

func NewLoginLogManagementService(db *gorm.DB, cache app.CacheInterf) *LoginLogManagementService {
	return &LoginLogManagementService{db: db, cache: cache}
}

func (s *LoginLogManagementService) Delete(ctx context.Context, ids []uint) (int64, error) {
	if s == nil || s.db == nil {
		return 0, errors.New("login log database is unavailable")
	}
	if len(ids) == 0 || len(ids) > maxLoginLogDeleteCount {
		return 0, fmt.Errorf("login log delete count must be between 1 and %d", maxLoginLogDeleteCount)
	}
	result := s.db.WithContext(ctx).Unscoped().Where("id IN ?", ids).Delete(&models.SysLoginLog{})
	return result.RowsAffected, result.Error
}

func (s *LoginLogManagementService) Clear(ctx context.Context) (int64, error) {
	if s == nil || s.db == nil {
		return 0, errors.New("login log database is unavailable")
	}
	var maxID uint
	if err := s.db.WithContext(ctx).Unscoped().Model(&models.SysLoginLog{}).
		Select("COALESCE(MAX(id), 0)").Scan(&maxID).Error; err != nil {
		return 0, err
	}
	if maxID == 0 {
		return 0, nil
	}
	result := s.db.WithContext(ctx).Unscoped().Where("id <= ?", maxID).Delete(&models.SysLoginLog{})
	return result.RowsAffected, result.Error
}

func (s *LoginLogManagementService) Unlock(ctx context.Context, logID uint) error {
	if s == nil || s.db == nil {
		return errors.New("login log database is unavailable")
	}
	if logID == 0 {
		return ErrLoginLogNotFound
	}
	var loginLog models.SysLoginLog
	loginLogResult := s.db.WithContext(ctx).Where("id = ?", logID).First(&loginLog)
	if loginLogResult.Error != nil {
		if errors.Is(loginLogResult.Error, gorm.ErrRecordNotFound) {
			return ErrLoginLogNotFound
		}
		return loginLogResult.Error
	}
	if loginLogResult.RowsAffected != 1 {
		return ErrLoginLogNotFound
	}
	if loginLog.Result != LoginResultFailure || loginLog.FailureReason != LoginFailureAccountLocked {
		return ErrLoginLogUnlockNotAllowed
	}
	if loginLog.UserID == nil || *loginLog.UserID == 0 {
		return ErrLoginLogUnlockUser
	}
	var user models.User
	userResult := s.db.WithContext(ctx).Where("id = ?", *loginLog.UserID).First(&user)
	if userResult.Error != nil {
		if errors.Is(userResult.Error, gorm.ErrRecordNotFound) {
			return ErrLoginLogUnlockUser
		}
		return userResult.Error
	}
	if userResult.RowsAffected != 1 {
		return ErrLoginLogUnlockUser
	}
	if s.cache == nil {
		return ErrLoginLogCacheUnavailable
	}
	if err := s.cache.Del(ctx, "account_locked:"+user.Username, "login_fail_count:"+user.Username); err != nil {
		return fmt.Errorf("%w: %v", ErrLoginLogCacheUnavailable, err)
	}
	return nil
}
