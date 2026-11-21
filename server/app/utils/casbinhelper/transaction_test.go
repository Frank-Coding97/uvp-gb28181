package casbinhelper

import (
	"context"
	"errors"
	"testing"
	"time"

	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

const transactionPolicyTable = "test_policy_rules"

func TestTransactionalHelperPreservesQueryDeadline(t *testing.T) {
	db := transactionPolicyDB(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	queries := 0
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:policy_deadline", func(query *gorm.DB) {
		if query.Statement.Table == transactionPolicyTable {
			queries++
			_, ok := query.Statement.Context.Deadline()
			require.True(t, ok, "policy query must retain the caller's deadline")
		}
	}))
	require.NoError(t, db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_, err := NewTransactionalHelper(tx, testModelConfig, "test_", "policy_rules")
		return err
	}))
	require.Equal(t, 1, queries)
}

func transactionPolicyDB(t *testing.T, migrate bool) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	if migrate {
		require.NoError(t, db.Table(transactionPolicyTable).AutoMigrate(&gormadapter.CasbinRule{}))
	}
	return db
}

func requireTransactionAccess(t *testing.T, helper *CasbinHelper, userID uint, resource, domain string, allowed bool) {
	t.Helper()
	actual, err := helper.Enforce(helper.PrefixUser(userID), resource, "GET", domain)
	require.NoError(t, err)
	require.Equal(t, allowed, actual, "resource=%s domain=%s", resource, domain)
}

func TestTransactionalHelperSharesPolicyWritesAndRollsBackIndependently(t *testing.T) {
	db := transactionPolicyDB(t, true)
	require.NoError(t, db.Table(transactionPolicyTable).Create(&[]gormadapter.CasbinRule{
		{Ptype: "g", V0: "user_7", V1: "role_10", V2: "*"},
		{Ptype: "p", V0: "role_10", V1: "/retained", V2: "GET", V3: "*"},
	}).Error)
	global := setupTestCasbin(t)
	require.NoError(t, global.AddRolesForUserByID(7, []uint{10}))
	require.NoError(t, global.AddPolicyForRole(10, "/retained", "GET"))
	previous := app.CasbinV2
	app.CasbinV2 = global
	t.Cleanup(func() { app.CasbinV2 = previous })
	rollback := errors.New("rollback policy transaction")
	err := db.Transaction(func(tx *gorm.DB) error {
		helper, err := NewTransactionalHelper(tx, testModelConfig, "test_", "policy_rules")
		require.NoError(t, err)
		requireTransactionAccess(t, helper, 7, "/retained", "*", true)
		require.NoError(t, helper.RemovePolicyForRole(10, "/retained", "GET"))
		require.NoError(t, helper.AddPolicyForRole(10, "/new", "GET"))
		requireTransactionAccess(t, helper, 7, "/retained", "*", false)
		requireTransactionAccess(t, helper, 7, "/new", "*", true)
		fresh, err := NewTransactionalHelper(tx, testModelConfig, "test_", "policy_rules")
		require.NoError(t, err)
		requireTransactionAccess(t, fresh, 7, "/retained", "*", false)
		requireTransactionAccess(t, fresh, 7, "/new", "*", true)
		require.Same(t, global, app.CasbinV2)
		requireTransactionAccess(t, global, 7, "/retained", "*", true)
		requireTransactionAccess(t, global, 7, "/new", "*", false)
		require.Nil(t, helper.reloadState, "a transaction must not start a reload goroutine")
		return rollback
	})
	require.ErrorIs(t, err, rollback)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		helper, err := NewTransactionalHelper(tx, testModelConfig, "test_", "policy_rules")
		require.NoError(t, err)
		requireTransactionAccess(t, helper, 7, "/retained", "*", true)
		requireTransactionAccess(t, helper, 7, "/new", "*", false)
		return nil
	}))
	requireTransactionAccess(t, global, 7, "/retained", "*", true)
	requireTransactionAccess(t, global, 7, "/new", "*", false)
}

func TestTransactionalHelperPreservesInheritanceAndDomainMatching(t *testing.T) {
	db := transactionPolicyDB(t, true)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		helper, err := NewTransactionalHelper(tx, testModelConfig, "test_", "policy_rules")
		require.NoError(t, err)
		require.NoError(t, helper.AddRolesForUserByID(9, []uint{8}, "/tenants/:tenant"))
		require.NoError(t, helper.AddRoleInheritance(8, 7, "/tenants/:tenant"))
		require.NoError(t, helper.AddPoliciesForRole(7, [][]string{{"/items/:id", "GET"}}))
		requireTransactionAccess(t, helper, 9, "/items/42", "/tenants/alpha", true)
		requireTransactionAccess(t, helper, 9, "/items/42", "/other/alpha", false)
		require.NoError(t, helper.DeleteRoleInheritance(8, 7, "/tenants/:tenant"))
		requireTransactionAccess(t, helper, 9, "/items/42", "/tenants/alpha", false)
		require.NoError(t, helper.AddRoleInheritance(8, 7, "/tenants/:tenant"))
		require.NoError(t, helper.DeleteRolesForUserByID(9, []uint{8}, "/tenants/:tenant"))
		requireTransactionAccess(t, helper, 9, "/items/42", "/tenants/alpha", false)
		require.NoError(t, helper.AddRolesForUserByID(9, []uint{8}, "/tenants/:tenant"))
		require.NoError(t, helper.RemoveAllPoliciesForRole(7))
		requireTransactionAccess(t, helper, 9, "/items/42", "/tenants/alpha", false)
		require.NoError(t, helper.AddPolicyForRole(7, "/saved", "GET"))
		return nil
	}))
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		helper, err := NewTransactionalHelper(tx, testModelConfig, "test_", "policy_rules")
		require.NoError(t, err)
		requireTransactionAccess(t, helper, 9, "/saved", "/tenants/alpha", true)
		return nil
	}))
}

func TestTransactionalHelperDoesNotCreateMissingPolicySchema(t *testing.T) {
	db := transactionPolicyDB(t, false)
	tx := db.Begin()
	require.NoError(t, tx.Error)
	helper, err := NewTransactionalHelper(tx, testModelConfig, "test_", "policy_rules")
	require.Error(t, err)
	require.Nil(t, helper)
	require.NoError(t, tx.Rollback().Error)
	require.False(t, db.Migrator().HasTable(transactionPolicyTable))
}

func TestTransactionalHelperRejectsInvalidTransactionAndModel(t *testing.T) {
	db := transactionPolicyDB(t, true)
	failed := db.Session(&gorm.Session{})
	failed.AddError(errors.New("transaction failed"))
	for _, candidate := range []*gorm.DB{nil, db, failed} {
		helper, err := NewTransactionalHelper(candidate, testModelConfig, "test_", "policy_rules")
		require.Error(t, err)
		require.Nil(t, helper)
	}
	tx := db.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()
	helper, err := NewTransactionalHelper(tx, "", "test_", "policy_rules")
	require.Error(t, err)
	require.Nil(t, helper)
}

type transactionNoPingPool struct {
	gorm.ConnPool
	gorm.TxCommitter
	pings int
}

func (p *transactionNoPingPool) Ping() error {
	p.pings++
	return errors.New("a transaction must not ping another connection")
}

func TestTransactionalHelperUsesTransactionWithoutPing(t *testing.T) {
	db := transactionPolicyDB(t, true)
	tx := db.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()
	pool := &transactionNoPingPool{ConnPool: tx.Statement.ConnPool, TxCommitter: tx.Statement.ConnPool.(gorm.TxCommitter)}
	tx.Statement.ConnPool = pool
	helper, err := NewTransactionalHelper(tx, testModelConfig, "test_", "policy_rules")
	require.NoError(t, err)
	require.NotNil(t, helper)
	require.Equal(t, 0, pool.pings)
	require.Nil(t, helper.reloadState)
}
