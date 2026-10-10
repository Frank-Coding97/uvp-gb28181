package bootstrap

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	appmodels "uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/openapi/adapters"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	openapimodels "uvplatform.com/uvp-gb28181/app/openapi/models"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

// publishFixture 建一个覆盖全部发布路径的库。
//
// ⛔⛔ 为什么要专门测这条路径：`EnsureCoreCatalog`（seed）有测试，
//   但 `EnsurePublishedCatalog`（seed + 组 draft + 落 release）只在
//   集成测试里出现过，而集成测试用的是**自建的表**，不是绿色包的 SQLite 基线。
//   真实故障恰恰是"基线建出来的表 + 首次发布"这个组合—— 启动即 panic。
func publishFixture(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&appmodels.SysApi{},
		&openapimodels.CapabilityGroup{},
		&openapimodels.Capability{},
		&openapimodels.Operation{},
		&openapimodels.Release{},
		&openapimodels.ReleaseItem{},
		&openapimodels.RuntimeState{},
		// 发布路径会写 client / client_scope（授予与退役联动），漏掉它们
		// 报的是 "no such table: sys_openapi_client_scope"，与真实现场混淆。
		&openapimodels.Client{},
		&openapimodels.ClientScope{},
		&openapimodels.Audit{},
	))
	return db
}

// newTestRuntime 必须与生产 routes.newCatalogRuntime 同款装配：
// 只注册一部分 adapter 会让发布校验以另一种方式失败，测不到真问题。
func newTestRuntime(t *testing.T, db *gorm.DB) *catalogruntime.CatalogRuntime {
	t.Helper()
	registry := catalogruntime.NewAdapterRegistry()
	reader := resource.New(db)
	registrations := adapters.NewDeviceChannelAdapterRegistrations(reader)
	registrations = append(registrations, adapters.NewDelegatedPlaneRegistrations()...)
	for _, registration := range registrations {
		require.NoError(t, registry.Register(registration))
	}
	return catalogruntime.NewCatalogRuntime(registry, nil)
}

// 首次发布必须成功。任何 ErrMissingWhereClause 都会让真实部署启动即 panic
// （`OpenAPI initialization failed; ingress remains closed` ⇒ 整个后端退出）。
func TestEnsurePublishedCatalog_FirstPublishSucceeds(t *testing.T) {
	db := publishFixture(t)
	runtime := newTestRuntime(t, db)

	outcome, err := EnsurePublishedCatalog(context.Background(), db, SystemActorID, runtime)
	require.NoError(t, err, "首次发布失败会让绿色包启动即 panic")
	require.Equal(t, PublishActionPublished, outcome.Action)
	require.NotZero(t, outcome.ReleaseID)
	require.NotZero(t, outcome.Version)
	require.Equal(t, len(CoreCatalogScopes()), outcome.ItemCount)
	require.Empty(t, outcome.CoreScopesMissing)
}

// 二次调用必须完全不动数据 —— 启动路径对健康安装是幂等的。
func TestEnsurePublishedCatalog_SecondCallIsNoOp(t *testing.T) {
	db := publishFixture(t)
	runtime := newTestRuntime(t, db)
	first, err := EnsurePublishedCatalog(context.Background(), db, SystemActorID, runtime)
	require.NoError(t, err)

	second, err := EnsurePublishedCatalog(context.Background(), db, SystemActorID, runtime)
	require.NoError(t, err)
	require.Equal(t, PublishActionExisting, second.Action)
	require.Equal(t, first.ReleaseID, second.ReleaseID)
	require.Equal(t, first.Version, second.Version, "健康安装重启不得推进契约版本")

	var releases int64
	require.NoError(t, db.Model(&openapimodels.Release{}).Count(&releases).Error)
	require.Equal(t, int64(1), releases, "不得重复发布出第二份release")
}

// 覆盖 retireRemovedPlaybackCapability / disableRetiredPlaybackGroup：
// 这两条路径带 Updates()，是 missing_where_clause 的高发区。
// 造出"退役的playback 能力 + 非空 playback 分组"让它们真正执行到。
func TestEnsurePublishedCatalog_RetiresLegacyPlaybackRows(t *testing.T) {
	db := publishFixture(t)
	runtime := newTestRuntime(t, db)
	require.NoError(t, db.Create(&appmodels.SysApi{BaseModel: appmodels.BaseModel{ID: 9}, Title: "旧播放", Path: "/api/gb28181/openapi/legacy", Method: "GET", ApiGroup: "媒体管理"}).Error)
	group := openapimodels.CapabilityGroup{Code: "playback", Name: "多屏播放", Sort: 20, Status: openapimodels.CatalogStatusDraft, RowVersion: 1}
	require.NoError(t, db.Create(&group).Error)
	legacy := openapimodels.Capability{
		Scope: "play:live:apply", Code: "legacy-apply", Name: "旧播放授权", GroupID: group.ID,
		Status: openapimodels.CatalogStatusActive, RowVersion: 1,
	}
	require.NoError(t, db.Create(&legacy).Error)
	// 该分组下还有一条非退役能力 ⇒ disableRetiredPlaybackGroup 必须不动它。
	keep := openapimodels.Capability{
		Scope: "play:live", Code: "live", Name: "播放", GroupID: group.ID,
		Status: openapimodels.CatalogStatusActive, RowVersion: 1,
	}
	require.NoError(t, db.Create(&keep).Error)
	op := openapimodels.Operation{
		Code: "apply", Name: "旧播放授权", CapabilityID: legacy.ID, AdapterKey: adapters.PlayLiveAdapterKey,
		Method: "POST", ExternalPath: "/openapi/v1/play/legacy-apply",
		Status: openapimodels.CatalogStatusActive,
	}
	require.NoError(t, db.Create(&op).Error)

	_, err := EnsurePublishedCatalog(context.Background(), db, SystemActorID, runtime)
	require.NoError(t, err)

	var got openapimodels.Capability
	require.NoError(t, db.Where("scope = ?", "play:live:apply").First(&got).Error)
	require.Equal(t, openapimodels.CatalogStatusDisabled, got.Status, "退役能力必须被置为禁用")

	var gotOp openapimodels.Operation
	require.NoError(t, db.Where("capability_id = ?", legacy.ID).First(&gotOp).Error)
	require.Equal(t, openapimodels.CatalogStatusDisabled, gotOp.Status)

	var gotGroup openapimodels.CapabilityGroup
	require.NoError(t, db.Where("code = ?", "playback").First(&gotGroup).Error)
	require.Equal(t, openapimodels.CatalogStatusDraft, gotGroup.Status, "分组内仍有活跃能力，不得禁用分组")
}

// 分组已空时才退役 —— 覆盖 disableRetiredPlaybackGroup 真正执行 Updates 的分支。
func TestEnsurePublishedCatalog_DisablesPlaybackGroupOnlyWhenEmpty(t *testing.T) {
	db := publishFixture(t)
	runtime := newTestRuntime(t, db)
	group := openapimodels.CapabilityGroup{Code: "playback", Name: "多屏播放", Sort: 20, Status: openapimodels.CatalogStatusDraft, RowVersion: 1}
	require.NoError(t, db.Create(&group).Error)
	// 组里只有一条已禁用的退役能力 ⇒ count 为 0 ⇒ 分组应被禁用。
	legacy := openapimodels.Capability{
		Scope: "play:live:apply", Code: "legacy-apply", Name: "旧播放授权", GroupID: group.ID,
		Status: openapimodels.CatalogStatusDisabled, RowVersion: 1,
	}
	require.NoError(t, db.Create(&legacy).Error)

	_, err := EnsurePublishedCatalog(context.Background(), db, SystemActorID, runtime)
	require.NoError(t, err)

	var got openapimodels.CapabilityGroup
	require.NoError(t, db.Where("code = ?", "playback").First(&got).Error)
	require.Equal(t, openapimodels.CatalogStatusDisabled, got.Status)
}