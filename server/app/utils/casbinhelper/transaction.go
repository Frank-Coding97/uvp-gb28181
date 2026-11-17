package casbinhelper

import (
	"context"
	"fmt"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/util"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"gorm.io/gorm"
)

type transactionAdapter struct {
	*gormadapter.Adapter
	ctx context.Context
}

func (a *transactionAdapter) LoadPolicy(m model.Model) error {
	return a.Adapter.LoadPolicyCtx(a.ctx, m)
}

// NewTransactionalHelper loads a private policy model from the caller's active
// transaction. The caller owns commit/rollback and must discard the helper when
// the transaction ends; it never publishes policy changes to a global helper.
func NewTransactionalHelper(tx *gorm.DB, modelConfig, tablePrefix, tableName string) (*CasbinHelper, error) {
	if tx == nil || tx.Error != nil || tx.Statement == nil {
		return nil, fmt.Errorf("casbin requires an active transaction")
	}
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return nil, fmt.Errorf("casbin requires an active transaction")
	}
	m, err := model.NewModelFromString(modelConfig)
	if err != nil {
		return nil, fmt.Errorf("create transactional casbin model: %w", err)
	}
	privateTx := tx.Session(&gorm.Session{})
	gormadapter.TurnOffAutoMigrate(privateTx)
	adapter, err := gormadapter.NewAdapterByDBUseTableName(privateTx, tablePrefix, tableName)
	if err != nil {
		return nil, fmt.Errorf("create transactional casbin adapter: %w", err)
	}
	enforcer, err := casbin.NewEnforcer(m, &transactionAdapter{Adapter: adapter, ctx: tx.Statement.Context})
	if err != nil {
		return nil, fmt.Errorf("load transactional casbin policy: %w", err)
	}
	enforcer.AddNamedDomainMatchingFunc("g", "KeyMatch2", util.KeyMatch2)
	return &CasbinHelper{enforcer: enforcer}, nil
}
