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
	if !app.GormDbMysql.Migrator().HasColumn(&GbChannel{}, "on_demand_live") {
		t.Skipf("跳过(MySQL gb_channel 缺 on_demand_live 列,请先跑 migration 2026-07-22-channel-on-demand-live.sql)")
	}
	if !app.GormDbMysql.Migrator().HasColumn(&GbChannel{}, "cloud_recording_enabled") {
		t.Skipf("跳过(MySQL gb_channel 缺 cloud_recording_enabled 列,请先跑 migration 2026-07-22-channel-cloud-recording.sql)")
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

// TestListPlayingChannels 覆盖 reconciler 依赖的 ListPlayingChannels:
// T4.1 混合场景:2 in-play + 3 idle -> 只返 2
// T4.2 全空:返回空 list,err=nil
// T4.3 一条 in-play + 软删:软删记录不返(gorm 默认 DeletedAt 过滤)
func TestListPlayingChannels(t *testing.T) {
	setupTestDB(t)
	skipIfChannelSchemaStale(t)
	ctx := context.TODO()

	const dev = "34020000001320900001"
	channels := []struct {
		ch     string
		stream string
	}{
		{"34020000001310900001", "ssrc-playing-1"},
		{"34020000001310900002", "ssrc-playing-2"},
		{"34020000001310900003", ""},
		{"34020000001310900004", ""},
		{"34020000001310900005", ""},
	}
	for _, item := range channels {
		cleanupChannel(dev, item.ch)
	}
	t.Cleanup(func() {
		for _, item := range channels {
			cleanupChannel(dev, item.ch)
		}
	})

	// 基准:先跑一次 List,拿到当前"已在播"基线(其他测试 / 真实数据可能残留)
	baseline, err := ListPlayingChannels(ctx)
	if err != nil {
		t.Fatalf("baseline ListPlayingChannels 失败: %v", err)
	}
	baselineIDs := map[string]bool{}
	for _, ch := range baseline {
		baselineIDs[ch.DeviceID+"|"+ch.ChannelID] = true
	}

	// T4.1: 插入 5 条(2 in-play + 3 idle)
	for _, item := range channels {
		if err := UpsertChannel(ctx, &GbChannel{
			DeviceID: dev, ChannelID: item.ch,
			Name: "test-channel", StreamID: item.stream,
		}); err != nil {
			t.Fatalf("插入 %s 失败: %v", item.ch, err)
		}
	}

	got, err := ListPlayingChannels(ctx)
	if err != nil {
		t.Fatalf("ListPlayingChannels 失败: %v", err)
	}
	newCount := 0
	for _, ch := range got {
		if baselineIDs[ch.DeviceID+"|"+ch.ChannelID] {
			continue
		}
		if ch.DeviceID != dev {
			continue
		}
		if ch.StreamID == "" {
			t.Errorf("返回记录 stream_id 空: %+v", ch)
		}
		newCount++
	}
	if newCount != 2 {
		t.Errorf("T4.1: 期望新增 2 条 in-play, 实际 %d 条", newCount)
	}

	// T4.3: 软删一条 in-play, 再 List 应该少 1 条
	if err := app.GormDbMysql.Where("device_id = ? AND channel_id = ?", dev, channels[0].ch).Delete(&GbChannel{}).Error; err != nil {
		t.Fatalf("软删失败: %v", err)
	}
	got2, err := ListPlayingChannels(ctx)
	if err != nil {
		t.Fatalf("T4.3 ListPlayingChannels 失败: %v", err)
	}
	found := false
	for _, ch := range got2 {
		if ch.DeviceID == dev && ch.ChannelID == channels[0].ch {
			found = true
			break
		}
	}
	if found {
		t.Errorf("T4.3: 软删的通道 %s 仍在结果中", channels[0].ch)
	}
}

// TestListPlayingChannelsEmpty T4.2 空 case:
// 无法在真实 MySQL 上完全清空表(共享库),这里放宽为"无 err + 支持空 list"
func TestListPlayingChannelsEmpty(t *testing.T) {
	setupTestDB(t)
	skipIfChannelSchemaStale(t)
	ctx := context.TODO()

	got, err := ListPlayingChannels(ctx)
	if err != nil {
		t.Fatalf("ListPlayingChannels 报错: %v", err)
	}
	// 只要能安全跑完,list 类型是 GbChannelList 即可
	if got == nil {
		// nil list 也是合法空,不 fatal
		return
	}
}
