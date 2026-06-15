package models

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/ymlconfig"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// setupTestDB 初始化测试用 GORM 连接(指向 config.yml 配置的 MySQL)
// 用轻量裸连,避开底座 ZapLog/BasePath 全局初始化的 cwd 依赖
func setupTestDB(t *testing.T) {
	if app.GormDbMysql != nil {
		return
	}
	_, thisFile, _, _ := runtime.Caller(0)
	// <server>/app/gb28181/models/gb_device_test.go → 上溯 3 层到 server
	configDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "config")
	if app.ConfigYml == nil {
		app.ConfigYml = ymlconfig.CreateYamlFactory(configDir)
	}
	c := app.ConfigYml
	host := c.GetString("gormv2.mysql.write.host")
	port := c.GetInt("gormv2.mysql.write.port")
	user := c.GetString("gormv2.mysql.write.user")
	pass := c.GetString("gormv2.mysql.write.pass")
	dbname := c.GetString("gormv2.mysql.write.database")
	dsn := user + ":" + pass + "@tcp(" + host + ":" + itoa(port) + ")/" + dbname + "?charset=utf8mb4&parseTime=True&loc=Local"
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Skipf("跳过(无法连接测试库 %s:%d): %v", host, port, err)
		return
	}
	app.GormDbMysql = db
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		p--
		b[p] = '-'
	}
	return string(b[p:])
}

// cleanup 删除测试数据(按 device_id 前缀)
func cleanup(deviceIDs ...string) {
	for _, id := range deviceIDs {
		app.GormDbMysql.Unscoped().Where("device_id = ?", id).Delete(&GbDevice{})
	}
}

// TestUpsertInsert T2-测1: Upsert 新设备 → 表里出现,字段正确
func TestUpsertInsert(t *testing.T) {
	setupTestDB(t)
	ctx := context.TODO()
	const did = "34020000001320000099"
	cleanup(did)
	defer cleanup(did)

	d := &GbDevice{DeviceID: did, Name: "测试设备", Transport: "UDP", IP: "1.2.3.4", Port: 5060, Status: DeviceStatusOnline}
	if err := Upsert(ctx, d); err != nil {
		t.Fatalf("Upsert 插入失败: %v", err)
	}
	got, err := FindByDeviceID(ctx, did)
	if err != nil || got == nil {
		t.Fatalf("插入后查不到: err=%v", err)
	}
	if got.Name != "测试设备" || got.Transport != "UDP" || got.Status != DeviceStatusOnline {
		t.Errorf("字段不符: %+v", got)
	}
}

// TestUpsertUpdate T2-测2: Upsert 已存在 device_id → 更新而非重复插入
func TestUpsertUpdate(t *testing.T) {
	setupTestDB(t)
	ctx := context.TODO()
	const did = "34020000001320000098"
	cleanup(did)
	defer cleanup(did)

	_ = Upsert(ctx, &GbDevice{DeviceID: did, Name: "旧名", Transport: "UDP"})
	_ = Upsert(ctx, &GbDevice{DeviceID: did, Name: "新名", Transport: "TCP"})

	var count int64
	app.GormDbMysql.Model(&GbDevice{}).Where("device_id = ?", did).Count(&count)
	if count != 1 {
		t.Errorf("期望 1 条记录(更新), 实际 %d 条(重复插入)", count)
	}
	got, _ := FindByDeviceID(ctx, did)
	if got.Name != "新名" || got.Transport != "TCP" {
		t.Errorf("更新未生效: %+v", got)
	}
}

// TestFindByDeviceID T2-测3: 命中/未命中
func TestFindByDeviceID(t *testing.T) {
	setupTestDB(t)
	ctx := context.TODO()
	got, err := FindByDeviceID(ctx, "00000000000000000000")
	if err != nil {
		t.Fatalf("未命中应返回 nil,nil,实际 err=%v", err)
	}
	if got != nil {
		t.Errorf("未命中应返回 nil,实际 %+v", got)
	}
}

// TestUpdateStatus T2-测4: 更新在线状态
func TestUpdateStatus(t *testing.T) {
	setupTestDB(t)
	ctx := context.TODO()
	const did = "34020000001320000097"
	cleanup(did)
	defer cleanup(did)

	_ = Upsert(ctx, &GbDevice{DeviceID: did, Status: DeviceStatusOnline})
	if err := UpdateStatus(ctx, did, DeviceStatusOffline); err != nil {
		t.Fatalf("UpdateStatus 失败: %v", err)
	}
	got, _ := FindByDeviceID(ctx, did)
	if got.Status != DeviceStatusOffline {
		t.Errorf("期望离线,实际 status=%d", got.Status)
	}
}
