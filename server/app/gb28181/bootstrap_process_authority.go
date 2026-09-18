package gb28181

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/openapi/processauthority"
)

// GB borrows main's authority. Neither assembly, reload nor stop may register
// another generation or release the process-lifetime lock. This preflight is
// not dispatch permission; every effect still fences its own transaction.
func authorizedRootIntentStore(db *gorm.DB, authority *processauthority.Authority) (*playauth.DeviceOperationIntentStore, error) {
	store, err := playauth.NewAuthorizedDeviceOperationIntentStore(db, authority)
	if err != nil {
		return nil, fmt.Errorf("进程授权不可用: %w", uac.ErrPlaybackUnavailable)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.WithContext(ctx).Transaction(authority.CheckTx); err != nil {
		return nil, fmt.Errorf("进程授权事务校验失败: %w", uac.ErrPlaybackUnavailable)
	}
	return store, nil
}
