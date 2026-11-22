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

// BatchAuthorizer is the whole-page form of Authorizer: same per-channel
// fail-closed verdict, but the caller's identity and visibility scope are
// resolved once instead of once per channel.
type BatchAuthorizer func(context.Context, *gorm.DB, uint, []uint) (map[uint]AuthorizationSnapshot, error)

// NewDatabaseBatchAuthorizer mirrors NewDatabaseAuthorizer for a page of
// channels.
//
// ⛔ Why this exists: the single-channel authorizer re-reads sys_users and the
// datascope for *every* channel. One page of 200 channels therefore costs
// 400+ round trips, and on a slow link (dev machine → 220 over OpenVPN, ~90ms
// RTT per leg) that blows straight through BatchStatus' 2s budget — the whole
// channel list answers 503 because a decorative share badge could not be read.
// The verdict for each channel is unchanged: same visibility scope, same
// device-epoch / owner-department checks, same two Casbin probes.
func NewDatabaseBatchAuthorizer(check PermissionChecker) BatchAuthorizer {
	return func(ctx context.Context, tx *gorm.DB, actorID uint, channelPKs []uint) (map[uint]AuthorizationSnapshot, error) {
		result := make(map[uint]AuthorizationSnapshot, len(channelPKs))
		if actorID == 0 || tx == nil || check == nil || len(channelPKs) == 0 {
			return result, nil
		}
		var user basemodels.User
		if err := tx.WithContext(ctx).Where("id = ? AND status = ?", actorID, 1).Take(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return result, nil
			}
			return nil, err
		}
		if user.ID == 0 {
			return result, nil
		}
		visibility, err := videoShareVisibilityScope(ctx, user, tx)
		if err != nil {
			return nil, err
		}
		ids := make([]uint, 0, len(channelPKs))
		seen := make(map[uint]bool, len(channelPKs))
		for _, id := range channelPKs {
			if id == 0 || seen[id] {
				continue
			}
			seen[id] = true
			ids = append(ids, id)
		}
		if len(ids) == 0 {
			return result, nil
		}
		var rows []batchAuthorizedChannel
		if err := tx.WithContext(ctx).Table("gb_channel").
			Select("gb_channel.id AS id, gb_channel.device_id AS device_id, gb_channel.channel_id AS channel_id, "+
				"gb_channel.owner_dept_id AS owner_dept_id, gb_device.id AS device_pk, "+
				"gb_device.access_epoch AS device_epoch, gb_device.owner_dept_id AS device_owner_dept_id").
			Joins("JOIN gb_device ON gb_device.device_id = gb_channel.device_id AND gb_device.deleted_at IS NULL").
			Scopes(visibility).
			Where("gb_channel.id IN ? AND gb_channel.deleted_at IS NULL", ids).
			Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			if _, done := result[row.ID]; done {
				continue
			}
			// 与单通道版逐条对齐：设备归属部门必须与通道一致，设备轮次不能为 0。
			if row.DevicePK == 0 || row.DeviceEpoch == 0 || row.DeviceOwnerDeptID != row.OwnerDeptID {
				continue
			}
			sharePath := "/api/gb28181/channel-shares/" + strconv.FormatUint(uint64(row.ID), 10)
			playPath := "/api/gb28181/play/" + row.DeviceID + "/" + row.ChannelID
			allowed := true
			for _, permission := range []struct{ path, method string }{{sharePath, http.MethodPut}, {playPath, http.MethodPost}} {
				ok, err := check(ctx, actorID, permission.path, permission.method)
				if err != nil {
					return nil, err
				}
				if !ok {
					allowed = false
					break
				}
			}
			if !allowed {
				continue
			}
			result[row.ID] = AuthorizationSnapshot{DevicePK: row.DevicePK, DeviceID: row.DeviceID, DeviceEpoch: row.DeviceEpoch}
		}
		return result, nil
	}
}

// batchAuthorizedChannel 是批量授权一次性取回的通道 + 设备快照。
// 单独定义而不是复用 GbChannel：需要把 gb_device 的列以别名映射进来。
type batchAuthorizedChannel struct {
	ID                uint
	DeviceID          string
	ChannelID         string
	OwnerDeptID       uint
	DevicePK          uint   `gorm:"column:device_pk"`
	DeviceEpoch       uint64 `gorm:"column:device_epoch"`
	DeviceOwnerDeptID uint   `gorm:"column:device_owner_dept_id"`
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
