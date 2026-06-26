package models

import (
	"context"
	"testing"

	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// skipIfChannelSchemaStale 老 schema(无 capabilities 列)时跳过,提示先跑 migration
//
// 背景:A1 给 gb_channel 加了 capabilities JSON 列,既有 MySQL dev 库尚未升级
// 不跑 migration 时这些测试会失败。Overnight 阶段不操作生产/dev 库,所以跳过。
func skipIfChannelSchemaStale(t *testing.T) {
	t.Helper()
	if app.GormDbMysql == nil {
		return
	}
	if !app.GormDbMysql.Migrator().HasColumn(&GbChannel{}, "capabilities") {
		t.Skipf("跳过(MySQL gb_channel 缺 capabilities 列,请先跑 migration 2026-06-26-catalog-b-plus.sql)")
	}
}

// TestUpsertChannelInsert T2-测1: Upsert 新通道
func TestUpsertChannelInsert(t *testing.T) {
	setupTestDB(t)
	skipIfChannelSchemaStale(t)
	ctx := context.TODO()
	const dev, ch = "34020000001320000018", "34020000001320000018"
	cleanupChannel(dev, ch)
	defer cleanupChannel(dev, ch)

	c := &GbChannel{DeviceID: dev, ChannelID: ch, Name: "测试通道", Status: ChannelStatusOnline, PTZType: 1}
	if err := UpsertChannel(ctx, c); err != nil {
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

// TestUpsertChannelUpdate T2-测3: 重复 upsert 更新不重复插入
func TestUpsertChannelUpdate(t *testing.T) {
	setupTestDB(t)
	skipIfChannelSchemaStale(t)
	ctx := context.TODO()
	const dev, ch = "34020000001320000018", "34020000001320000019"
	cleanupChannel(dev, ch)
	defer cleanupChannel(dev, ch)

	_ = UpsertChannel(ctx, &GbChannel{DeviceID: dev, ChannelID: ch, Name: "旧名"})
	_ = UpsertChannel(ctx, &GbChannel{DeviceID: dev, ChannelID: ch, Name: "新名"})

	var count int64
	app.GormDbMysql.Model(&GbChannel{}).Where("device_id = ? AND channel_id = ?", dev, ch).Count(&count)
	if count != 1 {
		t.Errorf("期望1条(更新),实际%d条", count)
	}
	got, _ := FindChannel(ctx, dev, ch)
	if got.Name != "新名" {
		t.Errorf("更新未生效: %s", got.Name)
	}
}

// TestListChannelsByDevice T2-测2: 按设备列通道
func TestListChannelsByDevice(t *testing.T) {
	setupTestDB(t)
	skipIfChannelSchemaStale(t)
	ctx := context.TODO()
	const dev = "34020000001320000099"
	cleanupChannel(dev, "ch1")
	cleanupChannel(dev, "ch2")
	defer func() { cleanupChannel(dev, "ch1"); cleanupChannel(dev, "ch2") }()

	_ = UpsertChannel(ctx, &GbChannel{DeviceID: dev, ChannelID: "ch1"})
	_ = UpsertChannel(ctx, &GbChannel{DeviceID: dev, ChannelID: "ch2"})

	list, err := ListChannelsByDevice(ctx, dev)
	if err != nil {
		t.Fatalf("列通道失败: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("期望2个通道,实际%d", len(list))
	}
}

func cleanupChannel(dev, ch string) {
	if app.GormDbMysql != nil {
		app.GormDbMysql.Unscoped().Where("device_id = ? AND channel_id = ?", dev, ch).Delete(&GbChannel{})
	}
}
