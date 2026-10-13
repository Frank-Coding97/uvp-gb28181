package zlm

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
	zlmrepo "uvplatform.com/uvp-gb28181/app/gb28181/zlm/repo"
	"uvplatform.com/uvp-gb28181/app/utils/gormhelper"
)

// 用绿色包真实基线 + 生产回调，复现"默认节点 seed 失败"。
//
// 现场（220 绿色包 2026-10-09 19:33:45）：
//
//	ERROR db  operation=insert table=meta_node  error={class=unknown type=*sqlite.Error}
//	WARN  app GB28181 ZLM 默认节点 seed 失败 event=zlm.node.default_seed_failed
//	INFO  app GB28181 ZLM Registry 已装配 nodes=0
//
// logging.Error 把 message 剥掉了，现场读不到真实原因，本地复现取原文。
func TestDefaultNodeSeedOnGreenPackageBaseline(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)
	var baseline string
	for dir := wd; dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, "resource", "database", "sqlitebaseline", "baseline.sql")
		if _, statErr := os.Stat(candidate); statErr == nil {
			baseline = candidate
			break
		}
	}
	require.NotEmpty(t, baseline, "找不到绿色包基线")
	script, err := os.ReadFile(baseline)
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "gp.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		PrepareStmt: false, SkipDefaultTransaction: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.Exec("PRAGMA foreign_keys = OFF").Error)
	require.NoError(t, db.Exec(string(script)).Error)
	require.NoError(t, db.Exec("PRAGMA foreign_keys = ON").Error)
	require.NoError(t, db.Callback().Query().Before("gorm:query").
		Register("disable_raise_record_not_found", gormhelper.MaskNotDataError))
	require.NoError(t, db.Callback().Create().Before("gorm:before_create").
		Register("CreateBeforeHook", gormhelper.CreateBeforeHook))
	require.NoError(t, db.Callback().Update().Before("gorm:before_update").
		Register("UpdateBeforeHook", gormhelper.UpdateBeforeHook))
	require.NoError(t, db.Callback().Delete().Before("gorm:before_delete").
		Register("DeleteBeforeHook", gormhelper.DeleteBeforeHook))

	reg := node.NewRegistry(zlmrepo.NewMetaNodeRepo(db))
	require.NoError(t, reg.LoadAll(context.Background()))
	require.Len(t, reg.List(), 0)

	// 与 bootstrap.go:1722 的调用参数一致（绿色包的实际取值）。
	seeded, err := reg.Add(context.Background(), node.Node{
		Name: "zlm-default", Host: "127.0.0.1", ReceiveHost: "", PlaybackHost: "",
		APIPort: 51100, APISecret: "uvp-9f3c7a1e5d84b206c1a9e4f7b2d6c805",
		MediaServerUUID: "uvp-media-server-0001", Weight: 50,
		State: node.StateActive, RTPPortStart: 30000, RTPPortEnd: 35000,
	})
	if err != nil {
		t.Fatalf("seed 失败，真实报错: %v", err)
	}
	t.Logf("seed 成功 id=%d uuid=%s", seeded.ID, seeded.MediaServerUUID)
}