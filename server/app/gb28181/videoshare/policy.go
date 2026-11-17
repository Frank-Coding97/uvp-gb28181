package videoshare

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/utils/casbinhelper"
)

// TransactionalPermissionChecker reads policy from the same authoritative
// transaction as the user, data scope, and share. It never uses a process cache.
func TransactionalPermissionChecker(tx *gorm.DB) (PermissionChecker, error) {
	helper, err := NewPolicyHelper(tx)
	if err != nil {
		return nil, err
	}
	return ActivePolicyChecker(tx, helper)
}

func NewPolicyHelper(tx *gorm.DB) (*casbinhelper.CasbinHelper, error) {
	if app.ConfigYml == nil {
		return nil, errors.New("authority configuration unavailable")
	}
	return casbinhelper.NewTransactionalHelper(tx, app.ConfigYml.GetString("casbin.modelconfig"), app.ConfigYml.GetString("casbin.tableprefix"), app.ConfigYml.GetString("casbin.tablename"))
}

func PermissionCheckerFor(helper app.CasbinInterf) PermissionChecker {
	return func(_ context.Context, userID uint, path, method string) (bool, error) {
		return helper.Enforce(fmt.Sprintf("user_%d", userID), path, method, "*")
	}
}

func NewTransactionalAuthorizer() Authorizer {
	return func(ctx context.Context, db *gorm.DB, actorID, channelPK uint) (AuthorizationSnapshot, error) {
		var result AuthorizationSnapshot
		check := func(tx *gorm.DB) error {
			permission, err := TransactionalPermissionChecker(tx)
			if err != nil {
				return err
			}
			result, err = NewDatabaseAuthorizer(permission)(ctx, tx, actorID, channelPK)
			return err
		}
		if _, active := db.Statement.ConnPool.(gorm.TxCommitter); active {
			err := check(db)
			return result, err
		}
		err := db.WithContext(ctx).Transaction(check)
		return result, err
	}
}
