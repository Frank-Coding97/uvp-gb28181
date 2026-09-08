package casbinhelper

import (
	"context"
	"errors"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/util"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"gorm.io/gorm"
)

// ValidatePolicyReadOnly checks that the supplied Casbin policy can be loaded
// without allowing the adapter to migrate the caller's database.
func ValidatePolicyReadOnly(ctx context.Context, db *gorm.DB, modelText, tablePrefix, tableName string) error {
	if db == nil {
		return errors.New("casbin read-only health: database unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	m, err := model.NewModelFromString(modelText)
	if err != nil {
		return errors.New("casbin read-only health: invalid model")
	}

	// TurnOffAutoMigrate mutates the *gorm.DB passed to it. Clone first so the
	// caller's DB and its context remain untouched.
	probe := db.Session(&gorm.Session{NewDB: true, Context: ctx})
	gormadapter.TurnOffAutoMigrate(probe)

	adapter, err := gormadapter.NewAdapterByDBUseTableName(probe, tablePrefix, tableName)
	if err != nil {
		return errors.New("casbin read-only health: policy adapter unavailable")
	}

	enforcer, err := casbin.NewEnforcer(m, adapter)
	if err != nil {
		return errors.New("casbin read-only health: policy enforcer unavailable")
	}
	_ = enforcer.AddNamedDomainMatchingFunc("g", "KeyMatch2", util.KeyMatch2)
	if err := enforcer.LoadPolicy(); err != nil {
		return errors.New("casbin read-only health: policy read failed")
	}
	return nil
}
