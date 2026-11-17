package videoshare

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"gorm.io/gorm"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/global/app"
	basemodels "uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/utils/datascope"
)

type PermissionChecker func(context.Context, uint, string, string) (bool, error)

// NewDatabaseAuthorizer returns a fail-closed authorizer for management and
// anonymous redemption. The callback must check the caller's current Casbin
// policy for the supplied route and method.
func NewDatabaseAuthorizer(check PermissionChecker) Authorizer {
	return func(ctx context.Context, tx *gorm.DB, actorID, channelPK uint) (AuthorizationSnapshot, error) {
		if actorID == 0 || tx == nil || check == nil {
			return AuthorizationSnapshot{}, ErrExpired
		}
		var user basemodels.User
		if err := tx.WithContext(ctx).Where("id = ? AND status = ?", actorID, 1).Take(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return AuthorizationSnapshot{}, ErrExpired
			}
			return AuthorizationSnapshot{}, err
		}
		if user.ID == 0 {
			return AuthorizationSnapshot{}, ErrExpired
		}
		visibility, err := videoShareVisibilityScope(ctx, user, tx)
		if err != nil {
			return AuthorizationSnapshot{}, err
		}
		var channel gbmodels.GbChannel
		var device struct {
			ID          uint
			DeviceID    string
			AccessEpoch uint64
			OwnerDeptID uint
		}
		if err := tx.WithContext(ctx).Model(&gbmodels.GbChannel{}).
			Select("gb_channel.*").
			Joins("JOIN gb_device ON gb_device.device_id = gb_channel.device_id AND gb_device.deleted_at IS NULL").
			Scopes(visibility).
			Where("gb_channel.id = ? AND gb_channel.deleted_at IS NULL", channelPK).
			Take(&channel).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return AuthorizationSnapshot{}, ErrExpired
			}
			return AuthorizationSnapshot{}, err
		}
		if channel.ID == 0 {
			return AuthorizationSnapshot{}, ErrExpired
		}
		if err := tx.WithContext(ctx).Table("gb_device").Select("id, device_id, access_epoch, owner_dept_id").Where("device_id = ? AND deleted_at IS NULL", channel.DeviceID).Take(&device).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return AuthorizationSnapshot{}, ErrExpired
			}
			return AuthorizationSnapshot{}, err
		}
		if device.ID == 0 || device.AccessEpoch == 0 || device.OwnerDeptID != channel.OwnerDeptID {
			return AuthorizationSnapshot{}, ErrExpired
		}
		sharePath := "/api/gb28181/channel-shares/" + strconv.FormatUint(uint64(channelPK), 10)
		playPath := "/api/gb28181/play/" + channel.DeviceID + "/" + channel.ChannelID
		for _, permission := range []struct{ path, method string }{{sharePath, http.MethodPut}, {playPath, http.MethodPost}} {
			allowed, err := check(ctx, actorID, permission.path, permission.method)
			if err != nil {
				return AuthorizationSnapshot{}, err
			}
			if !allowed {
				return AuthorizationSnapshot{}, ErrExpired
			}
		}
		return AuthorizationSnapshot{DevicePK: device.ID, DeviceID: device.DeviceID, DeviceEpoch: device.AccessEpoch}, nil
	}
}

func videoShareVisibilityScope(ctx context.Context, user basemodels.User, tx *gorm.DB) (func(*gorm.DB) *gorm.DB, error) {
	all := func(query *gorm.DB) *gorm.DB { return query }
	if app.ConfigYml != nil {
		for _, userID := range app.ConfigYml.GetUintSlice("server.notcheckuser") {
			if user.ID == userID {
				return all, nil
			}
		}
	}
	access, err := datascope.ResolveOwnerDeptAccessByUserID(ctx, tx, user.ID)
	if err != nil && !errors.Is(err, datascope.ErrOwnerDeptAccessDenied) {
		return nil, err
	}
	if access.FullAccess {
		return all, nil
	}
	// An absent or unusable active role grants no owner visibility. Explicit
	// grants remain valid for this trusted user or the user's actual department.
	grants := tx.WithContext(ctx).Model(&gbmodels.GbDeviceGrant{}).Select("device_id")
	if user.DeptID == 0 {
		grants = grants.Where("target_type = ? AND target_id = ?", gbmodels.GrantTargetTypeUser, user.ID)
	} else {
		grants = grants.Where("(target_type = ? AND target_id = ?) OR (target_type = ? AND target_id = ?)",
			gbmodels.GrantTargetTypeUser, user.ID, gbmodels.GrantTargetTypeDept, user.DeptID)
	}
	return func(query *gorm.DB) *gorm.DB {
		if len(access.DeptIDs) == 0 {
			return query.Where("gb_device.id IN (?)", grants)
		}
		return query.Where("gb_channel.owner_dept_id IN ? OR gb_device.id IN (?)", access.DeptIDs, grants)
	}, nil
}

// CasbinPermissionChecker adapts the application's current in-memory policy.
func CasbinPermissionChecker(ctx context.Context, userID uint, path, method string) (bool, error) {
	if app.CasbinV2 == nil {
		return false, fmt.Errorf("casbin permission service unavailable")
	}
	return app.CasbinV2.Enforce(fmt.Sprintf("user_%d", userID), path, method, "*")
}
