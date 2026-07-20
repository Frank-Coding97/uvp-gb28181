package models

import (
	"context"
	"testing"

	"uvplatform.cn/uvp-gb28181/app/global/app"
)

func skipIfChannelSchemaStale(t *testing.T) {
	t.Helper()
	if app.GormDbMysql == nil {
		return
	}
	if !app.GormDbMysql.Migrator().HasColumn(&GbChannel{}, "capabilities") {
		t.Skipf("跳过(MySQL gb_channel 缺 capabilities 列,请先跑 migration 2026-06-26-catalog-b-plus.sql)")
	}
	if !app.GormDbMysql.Migrator().HasColumn(&GbChannel{}, "owner_dept_id") {
		t.Skipf("跳过(MySQL gb_channel 缺 owner_dept_id 列,请先跑 migration 2026-07-11-owner-dept-phase1.sql)")
	}
	if !app.GormDbMysql.Migrator().HasColumn(&GbChannel{}, "snapshot_url") {
		t.Skipf("跳过(MySQL gb_channel 缺 snapshot_url 列,请先跑 migration 2026-07-20-channel-snapshot.sql)")
	}
	if app.GormDbMysql.Migrator().HasColumn(&GbChannel{}, "tenant_id") {
		t.Skipf("跳过(MySQL gb_channel 仍有 tenant_id 列,请先跑 Phase 2 去租户迁移)")
	}
}

func TestUpsertChannelInsert(t *testing.T) {
	setupTestDB(t)
	skipIfChannelSchemaStale(t)
	ctx := context.TODO()
	const dev, ch = "34020000001320000018", "34020000001320000018"
	cleanupChannel(dev, ch)
	t.Cleanup(func() { cleanupChannel(dev, ch) })

	err := UpsertChannel(ctx, &GbChannel{DeviceID: dev, ChannelID: ch, Name: "测试通道", Status: ChannelStatusOnline, PTZType: 1})
	if err != nil {
		t.Fatalf("Upsert 通道失败: %v", err)
	}
	got, err := FindChannel(ctx, dev, ch)
	if err != nil || got == nil {
		t.Fatalf("插入后查不到: %v", err)
	}
	if got.Name != "测试通道" || got.PTZType != 1 {
		t.Errorf("字段不符: %+v", got)
	}
}

func TestUpsertChannelUpdate(t *testing.T) {
	setupTestDB(t)
	skipIfChannelSchemaStale(t)
	ctx := context.TODO()
	const dev, ch = "34020000001320000018", "34020000001320000019"
	cleanupChannel(dev, ch)
	t.Cleanup(func() { cleanupChannel(dev, ch) })

	if err := UpsertChannel(ctx, &GbChannel{DeviceID: dev, ChannelID: ch, Name: "旧名"}); err != nil {
		t.Fatalf("首次 Upsert 失败: %v", err)
	}
	if err := UpsertChannel(ctx, &GbChannel{DeviceID: dev, ChannelID: ch, Name: "新名"}); err != nil {
		t.Fatalf("二次 Upsert 失败: %v", err)
	}

	var count int64
	app.GormDbMysql.Model(&GbChannel{}).Where("device_id = ? AND channel_id = ?", dev, ch).Count(&count)
	if count != 1 {
		t.Fatalf("期望1条(更新),实际%d条", count)
	}
	got, _ := FindChannel(ctx, dev, ch)
	if got == nil || got.Name != "新名" {
		t.Fatalf("更新字段失败: %+v", got)
	}
}

func TestListChannelsByDevice(t *testing.T) {
	setupTestDB(t)
	skipIfChannelSchemaStale(t)
	ctx := context.TODO()
	const dev = "34020000001320000099"
	cleanupChannel(dev, "34020000001310000001")
	cleanupChannel(dev, "34020000001310000002")
	t.Cleanup(func() {
		cleanupChannel(dev, "34020000001310000001")
		cleanupChannel(dev, "34020000001310000002")
	})

	if err := UpsertChannel(ctx, &GbChannel{DeviceID: dev, ChannelID: "34020000001310000001"}); err != nil {
		t.Fatalf("插入通道1失败: %v", err)
	}
	if err := UpsertChannel(ctx, &GbChannel{DeviceID: dev, ChannelID: "34020000001310000002"}); err != nil {
		t.Fatalf("插入通道2失败: %v", err)
	}
	list, err := ListChannelsByDevice(ctx, dev)
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if len(list) < 2 {
		t.Fatalf("期望至少2条,实际%d条", len(list))
	}
}

func cleanupChannel(dev, ch string) {
	if app.GormDbMysql == nil {
		return
	}
	app.GormDbMysql.Unscoped().Where("device_id = ? AND channel_id = ?", dev, ch).Delete(&GbChannel{})
}
