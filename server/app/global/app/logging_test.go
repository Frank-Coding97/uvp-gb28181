package app_test

import (
	"context"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"gorm.io/gorm"
	"sync"
	"testing"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

func TestLoggingDBContext(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	old := app.GormDbMysql
	app.GormDbMysql = db
	defer func() { app.GormDbMysql = old }()
	core, observed := observer.New(zap.DebugLevel)
	root := zap.New(core)
	var wg sync.WaitGroup
	for i := 1; i <= 64; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			claims := &app.Claims{}
			claims.UserID = uint(id)
			ctx := context.WithValue(context.Background(), consts.BindContextKeyName, claims)
			ctx = logging.WithContext(ctx, logging.WithIdentity(root, zap.Int("request_id", id)))
			scoped := app.DBContext(ctx, "mysql").Session(&gorm.Session{DryRun: true})
			child := scoped.WithContext(context.WithValue(scoped.Statement.Context, "child", true))
			if gormhelper.GetCurrentUserIDFromContext(child.Statement.Context) != uint(id) {
				t.Error("Claims crossed requests")
			}
			app.Log(child.Statement.Context).Info("database scope", zap.Int("expected", id))
			if child.Statement.Context == db.Statement.Context {
				t.Error("root context mutated")
			}
		}(i)
	}
	wg.Wait()
	if observed.Len() != 64 {
		t.Fatal("missing scoped logs")
	}
	for _, entry := range observed.All() {
		fields := entry.ContextMap()
		if fields["request_id"] != fields["expected"] {
			t.Error("request IDs crossed")
		}
	}
	if gormhelper.GetCurrentUserIDFromContext(db.Statement.Context) != 0 {
		t.Error("root Claims leaked")
	}
	ctx := context.WithValue(context.Background(), consts.BindContextKeyName, &app.Claims{})
	scoped := app.DBContext(ctx, "mysql")
	sentinel := context.WithValue(ctx, "tx-scope", true)
	err = scoped.Transaction(func(tx *gorm.DB) error {
		derived := tx.WithContext(sentinel)
		if derived.Statement.ConnPool != tx.Statement.ConnPool || derived.Statement.ConnPool == db.Statement.ConnPool {
			t.Error("transaction connection replaced")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
