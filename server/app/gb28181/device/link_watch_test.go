package device

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
)

const (
	// defaultLinkWatchWait / Tick 只服务「异步上抛是否生效」那一例 —— 判定本身要走一次
	// 数据库事务,在 CI 机器上给足 3 秒余量,不必为它调快到会 flaky。
	defaultLinkWatchWait = 3 * time.Second
	defaultLinkWatchTick = 10 * time.Millisecond
)

// newLinkWatchTestDB 建库,**刻意不复用 status_event_test.go 的 newStatusEventTestDB**。
//
// ⛔ 差别只在存储:那边用 `:memory:`,而 glebarez/sqlite 的内存库是**每连接一个**
// (SQLite 的 `:memory:` 语义)。[LinkWatcher.DeviceLinkLost] 的判定跑在独立 goroutine 里
// 会向连接池取到**另一条**连接,于是报 "no such table: gb_device" —— 单独跑碰巧池里
// 只有一条连接所以通过,和别的用例一起跑就红,是典型的"偶发但其实是必现"。
// 文件型 sqlite 天然跨连接共享,再配 t.TempDir() 保证用完即清。
func newLinkWatchTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	prevDB, prevConfig := app.GormDbMysql, app.ConfigYml
	t.Cleanup(func() { app.GormDbMysql, app.ConfigYml = prevDB, prevConfig })

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "link_watch.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbDevice{},
		&gbmodels.GbChannel{},
		&gbmodels.GbDeviceStatusEvent{},
		&basemodels.SysDepartment{},
	))
	app.GormDbMysql = db
	app.ConfigYml = testConfig{}
	require.NoError(t, db.Create(&basemodels.SysDepartment{BaseModel: basemodels.BaseModel{ID: 1}, Name: "接入池"}).Error)
	return db
}

// onlineDevice 落一台设备的注册事实:ip/port 就是它有 TCP 连接时的来源端点。
func onlineDevice(t *testing.T, db *gorm.DB, deviceID, ip string, port int, transport string, status int8) {
	t.Helper()
	require.NoError(t, db.Create(&gbmodels.GbDevice{
		DeviceID:          deviceID,
		Name:              "链路断开测试设备",
		OwnerDeptID:       1,
		IP:                ip,
		Port:              port,
		Transport:         transport,
		Status:            status,
		KeepaliveInterval: 60,
	}).Error)
}

func deviceStatus(t *testing.T, db *gorm.DB, deviceID string) int8 {
	t.Helper()
	var device gbmodels.GbDevice
	require.NoError(t, db.Where("device_id = ?", deviceID).Limit(1).Find(&device).Error)
	return device.Status
}

func statusEventTypes(t *testing.T, db *gorm.DB) []gbmodels.DeviceStatusEventType {
	t.Helper()
	var events []gbmodels.GbDeviceStatusEvent
	require.NoError(t, db.Order("id").Find(&events).Error)
	types := make([]gbmodels.DeviceStatusEventType, 0, len(events))
	for _, event := range events {
		types = append(types, event.EventType)
	}
	return types
}

// 核心用例:TCP 通道断开 → 设备**立刻**离线,并留下「链路断开」类型的事件
// (而不是等 180 秒后被 offline_scanner 记成「心跳超时」)。
func TestMarkEndpointOfflineOnTCPDisconnect(t *testing.T) {
	db := newLinkWatchTestDB(t)
	onlineDevice(t, db, "34020000001320000001", "203.0.113.5", 51234, "TCP", gbmodels.DeviceStatusOnline)

	NewLinkWatcher(db, nil).markEndpointOffline("TCP", "203.0.113.5:51234")

	assert.Equal(t, gbmodels.DeviceStatusOffline, deviceStatus(t, db, "34020000001320000001"))
	assert.Equal(t, []gbmodels.DeviceStatusEventType{gbmodels.DeviceEventLinkClosed}, statusEventTypes(t, db))
}

// 设备断链后已重连到**新端点**(NAT 会给新端口)并重新注册 → 记录里的 ip/port 已是新值,
// 按旧端点查不到任何在线设备,绝不能再把它判离线。
func TestMarkEndpointOfflineLeavesReconnectedDeviceOnline(t *testing.T) {
	db := newLinkWatchTestDB(t)
	// 记录里已经是重连后的新端点
	onlineDevice(t, db, "34020000001320000002", "203.0.113.9", 60001, "TCP", gbmodels.DeviceStatusOnline)

	// 旧端点上的那条连接晚一步才退出
	NewLinkWatcher(db, nil).markEndpointOffline("TCP", "203.0.113.5:51234")

	assert.Equal(t, gbmodels.DeviceStatusOnline, deviceStatus(t, db, "34020000001320000002"))
	assert.Empty(t, statusEventTypes(t, db))
}

// 同地址但传输方式不同:某台 UDP 设备不该被「同地址上某个 TCP 连接断掉」牵连。
func TestMarkEndpointOfflineIgnoresTransportMismatch(t *testing.T) {
	db := newLinkWatchTestDB(t)
	onlineDevice(t, db, "34020000001320000003", "203.0.113.5", 51234, "UDP", gbmodels.DeviceStatusOnline)

	NewLinkWatcher(db, nil).markEndpointOffline("TCP", "203.0.113.5:51234")

	assert.Equal(t, gbmodels.DeviceStatusOnline, deviceStatus(t, db, "34020000001320000003"))
	assert.Empty(t, statusEventTypes(t, db))
}

// 已经离线的设备:置离线是幂等的,不能因为「同一条连接被上抛两次」就记两条事件。
func TestMarkEndpointOfflineIsIdempotentOnOfflineDevice(t *testing.T) {
	db := newLinkWatchTestDB(t)
	onlineDevice(t, db, "34020000001320000004", "203.0.113.5", 51234, "TCP", gbmodels.DeviceStatusOffline)

	watcher := NewLinkWatcher(db, nil)
	watcher.markEndpointOffline("TCP", "203.0.113.5:51234")
	watcher.markEndpointOffline("TCP", "203.0.113.5:51234")

	assert.Equal(t, gbmodels.DeviceStatusOffline, deviceStatus(t, db, "34020000001320000004"))
	assert.Empty(t, statusEventTypes(t, db))
}

func TestMarkEndpointOfflineSkipsUnparsableEndpoint(t *testing.T) {
	db := newLinkWatchTestDB(t)
	onlineDevice(t, db, "34020000001320000005", "203.0.113.5", 51234, "TCP", gbmodels.DeviceStatusOnline)

	watcher := NewLinkWatcher(db, nil)
	// 没有端口 / 端口非法 / 主机为空 —— 都定位不到设备,必须安全跳过而不是误判。
	for _, raw := range []string{"203.0.113.5", "203.0.113.5:abc", ":0", ""} {
		watcher.markEndpointOffline("TCP", raw)
	}

	assert.Equal(t, gbmodels.DeviceStatusOnline, deviceStatus(t, db, "34020000001320000005"))
	assert.Empty(t, statusEventTypes(t, db))
}

// 端点是 IPv6 时同样要能定位:设备注册来源就是它,不能因为冒号多就解析失败。
func TestMarkEndpointOfflineHandlesIPv6Endpoint(t *testing.T) {
	db := newLinkWatchTestDB(t)
	onlineDevice(t, db, "34020000001320000006", "2001:db8::5", 51234, "TCP", gbmodels.DeviceStatusOnline)

	NewLinkWatcher(db, nil).markEndpointOffline("TCP", "[2001:db8::5]:51234")

	assert.Equal(t, gbmodels.DeviceStatusOffline, deviceStatus(t, db, "34020000001320000006"))
	assert.Equal(t, []gbmodels.DeviceStatusEventType{gbmodels.DeviceEventLinkClosed}, statusEventTypes(t, db))
}

func TestLinkWatcherWithoutDBIsNoop(t *testing.T) {
	ctx := context.Background()
	assert.NotPanics(t, func() {
		NewLinkWatcher(nil, nil).DeviceLinkLost(ctx, "TCP", "203.0.113.5:51234")
		var nilWatcher *LinkWatcher
		nilWatcher.DeviceLinkLost(ctx, "TCP", "203.0.113.5:51234")
	})
}

// DeviceLinkLost 必须**非阻塞**:它跑在 sipgo read loop 的退出路径上,同步做事务会把
// 连接回收拖住。所以判定是异步的,这里用 Eventually 等结果落地。
func TestDeviceLinkLostRunsAsynchronously(t *testing.T) {
	db := newLinkWatchTestDB(t)
	const deviceID = "34020000001320000007"
	onlineDevice(t, db, deviceID, "203.0.113.5", 51234, "TCP", gbmodels.DeviceStatusOnline)

	NewLinkWatcher(db, nil).DeviceLinkLost(context.Background(), "TCP", "203.0.113.5:51234")

	require.Eventually(t, func() bool {
		var device gbmodels.GbDevice
		if err := db.Where("device_id = ?", deviceID).Limit(1).Find(&device).Error; err != nil {
			return false
		}
		return device.Status == gbmodels.DeviceStatusOffline
	}, defaultLinkWatchWait, defaultLinkWatchTick)
}
