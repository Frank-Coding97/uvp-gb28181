package handler_test

import (
	"context"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/icholy/digest"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	gbdevice "uvplatform.cn/uvp-gb28181/app/gb28181/device"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	gbsip "uvplatform.cn/uvp-gb28181/app/gb28181/sip"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/cachehelper"
	"uvplatform.cn/uvp-gb28181/app/utils/ymlconfig"

	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

const (
	testSIPPort  = 25060
	testPassword = "12345678"
	testDeviceID = "34020000001320000088"
)

// setupEnv 初始化 ConfigYml / DB / Redis / ZapLog(连真实 222)
func setupEnv(t *testing.T) {
	if app.ZapLog == nil {
		app.ZapLog = zap.NewNop()
	}
	_, thisFile, _, _ := runtime.Caller(0)
	configDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "config")
	if app.ConfigYml == nil {
		app.ConfigYml = ymlconfig.CreateYamlFactory(configDir)
	}
	c := app.ConfigYml
	if app.GormDbMysql == nil {
		dsn := c.GetString("gormv2.mysql.write.user") + ":" + c.GetString("gormv2.mysql.write.pass") +
			"@tcp(" + c.GetString("gormv2.mysql.write.host") + ":" + strconv.Itoa(c.GetInt("gormv2.mysql.write.port")) + ")/" +
			c.GetString("gormv2.mysql.write.database") + "?charset=utf8mb4&parseTime=True&loc=Local"
		db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
		if err != nil {
			t.Skipf("跳过(无法连接 MySQL): %v", err)
		}
		_ = db.Callback().Query().Before("gorm:query").Register("disable_raise_record_not_found", func(g *gorm.DB) { g.Statement.RaiseErrorOnNotFound = false })
		app.GormDbMysql = db
	}
	if app.Cache == nil {
		addr := c.GetString("redis.host") + ":" + strconv.Itoa(c.GetInt("redis.port"))
		cache, err := cachehelper.NewRedisHelper(addr, c.GetString("redis.password"), c.GetInt("redis.indexdb"))
		if err != nil {
			t.Skipf("跳过(无法连接 Redis): %v", err)
		}
		app.Cache = cache
	}
}

func testCfg() gbconfig.Config {
	return gbconfig.Config{
		Enabled: true,
		SIP: gbconfig.SIPConfig{
			IP: "127.0.0.1", Port: testSIPPort, Transport: []string{"udp"},
			Domain: "3402000000", ServerID: "34020000002000000001", Password: testPassword,
		},
		Device: gbconfig.DeviceConfig{KeepaliveInterval: 60, KeepaliveTimeoutCount: 3, OfflineScanInterval: 30},
	}
}

// startTestServer 启动一个接入真实 RegisterHandler 的 SIP server
func startTestServer(t *testing.T, cfg gbconfig.Config) func() {
	srv, err := gbsip.NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
}

// doRegister 模拟设备两步注册,返回最终状态码
// password 错误可触发鉴权失败;expires=0 触发注销
func doRegister(t *testing.T, deviceID, password string, expires int) int {
	// UA 名设为 deviceID,使 sipgo 构造的 From.user = 国标编码(模拟真实设备)
	ua, _ := sipgo.NewUA(sipgo.WithUserAgent(deviceID))
	client, err := sipgo.NewClient(ua, sipgo.WithClientHostname("127.0.0.1"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	recipient := sip.Uri{}
	sip.ParseUri("sip:"+deviceID+"@127.0.0.1:"+strconv.Itoa(testSIPPort), &recipient)
	req := sip.NewRequest(sip.REGISTER, recipient)
	req.AppendHeader(sip.NewHeader("Contact", "<sip:"+deviceID+"@127.0.0.1>"))
	req.AppendHeader(sip.NewHeader("Expires", strconv.Itoa(expires)))
	req.SetTransport("UDP")

	ctx := context.Background()
	tx, err := client.TransactionRequest(ctx, req, sipgo.ClientRequestRegisterBuild)
	if err != nil {
		t.Fatalf("首次事务: %v", err)
	}
	res := <-tx.Responses()
	tx.Terminate()
	if res == nil {
		t.Fatal("首次无响应")
	}
	if res.StatusCode != 401 {
		return int(res.StatusCode) // 未挑战,直接返回
	}

	// 解析挑战,带 Authorization 重发
	wwwAuth := res.GetHeader("WWW-Authenticate")
	chal, err := digest.ParseChallenge(wwwAuth.Value())
	if err != nil {
		t.Fatalf("解析挑战: %v", err)
	}
	cred, _ := digest.Digest(chal, digest.Options{
		Method: "REGISTER", URI: recipient.String(), Username: deviceID, Password: password,
	})
	newReq := req.Clone()
	newReq.RemoveHeader("Via")
	newReq.AppendHeader(sip.NewHeader("Authorization", cred.String()))
	tx2, err := client.TransactionRequest(ctx, newReq, sipgo.ClientRequestIncreaseCSEQ, sipgo.ClientRequestAddVia)
	if err != nil {
		t.Fatalf("二次事务: %v", err)
	}
	defer tx2.Terminate()
	res2 := <-tx2.Responses()
	if res2 == nil {
		t.Fatal("二次无响应")
	}
	return int(res2.StatusCode)
}

func cleanupDevice(id string) {
	if app.GormDbMysql != nil {
		app.GormDbMysql.Unscoped().Where("device_id = ?", id).Delete(&gbmodels.GbDevice{})
	}
	if app.Cache != nil {
		_ = app.Cache.Del(context.Background(), gbdevice.OnlineKey(id))
	}
}

// TestRegisterChallenge T4-测1(AC-2): 无鉴权 REGISTER → 401 挑战
func TestRegisterChallenge(t *testing.T) {
	setupEnv(t)
	stop := startTestServer(t, testCfg())
	defer stop()

	ua, _ := sipgo.NewUA(sipgo.WithUserAgent(testDeviceID))
	client, _ := sipgo.NewClient(ua, sipgo.WithClientHostname("127.0.0.1"))
	defer client.Close()
	recipient := sip.Uri{}
	sip.ParseUri("sip:"+testDeviceID+"@127.0.0.1:"+strconv.Itoa(testSIPPort), &recipient)
	req := sip.NewRequest(sip.REGISTER, recipient)
	req.AppendHeader(sip.NewHeader("Contact", "<sip:"+testDeviceID+"@127.0.0.1>"))
	req.SetTransport("UDP")
	tx, _ := client.TransactionRequest(context.Background(), req, sipgo.ClientRequestRegisterBuild)
	defer tx.Terminate()
	res := <-tx.Responses()
	if res == nil || res.StatusCode != 401 {
		t.Fatalf("期望 401 挑战,实际 %v", res)
	}
	if res.GetHeader("WWW-Authenticate") == nil {
		t.Error("401 响应缺少 WWW-Authenticate 头")
	}
}

// TestRegisterSuccess T4-测2/4(AC-1/2/3): 正确密码 → 200 + 自动建档 + Redis 在线
func TestRegisterSuccess(t *testing.T) {
	setupEnv(t)
	cleanupDevice(testDeviceID)
	defer cleanupDevice(testDeviceID)
	stop := startTestServer(t, testCfg())
	defer stop()

	code := doRegister(t, testDeviceID, testPassword, 3600)
	if code != 200 {
		t.Fatalf("期望 200,实际 %d", code)
	}
	// 自动建档
	d, err := gbmodels.FindByDeviceID(context.Background(), testDeviceID)
	if err != nil || d == nil {
		t.Fatalf("未自动建档: err=%v", err)
	}
	if d.Status != gbmodels.DeviceStatusOnline {
		t.Errorf("期望在线,实际 status=%d", d.Status)
	}
	// Redis 在线态
	if !gbdevice.IsOnline(context.Background(), testDeviceID) {
		t.Error("Redis 在线态未写入")
	}
}

// TestRegisterWrongPassword T4-测3(AC-2): 错误密码 → 拒绝,不建档
func TestRegisterWrongPassword(t *testing.T) {
	setupEnv(t)
	const did = "34020000001320000087"
	cleanupDevice(did)
	defer cleanupDevice(did)
	stop := startTestServer(t, testCfg())
	defer stop()

	code := doRegister(t, did, "wrongpass", 3600)
	if code == 200 {
		t.Fatalf("错误密码不应 200")
	}
	d, _ := gbmodels.FindByDeviceID(context.Background(), did)
	if d != nil {
		t.Errorf("错误密码不应建档,但查到 %+v", d)
	}
}

// TestRegisterRefresh T4-测5(AC-7): 重复注册 → 更新不重复建档
func TestRegisterRefresh(t *testing.T) {
	setupEnv(t)
	cleanupDevice(testDeviceID)
	defer cleanupDevice(testDeviceID)
	stop := startTestServer(t, testCfg())
	defer stop()

	_ = doRegister(t, testDeviceID, testPassword, 3600)
	code := doRegister(t, testDeviceID, testPassword, 3600)
	if code != 200 {
		t.Fatalf("二次注册期望 200,实际 %d", code)
	}
	var count int64
	app.GormDbMysql.Model(&gbmodels.GbDevice{}).Where("device_id = ?", testDeviceID).Count(&count)
	if count != 1 {
		t.Errorf("期望 1 条记录,实际 %d", count)
	}
}

// sendKeepalive 模拟设备发 Keepalive MESSAGE
func sendKeepalive(t *testing.T, deviceID string) int {
	ua, _ := sipgo.NewUA(sipgo.WithUserAgent(deviceID))
	client, _ := sipgo.NewClient(ua, sipgo.WithClientHostname("127.0.0.1"))
	defer client.Close()

	recipient := sip.Uri{}
	sip.ParseUri("sip:34020000002000000001@127.0.0.1:"+strconv.Itoa(testSIPPort), &recipient)
	req := sip.NewRequest(sip.MESSAGE, recipient)
	body := `<?xml version="1.0" encoding="UTF-8"?>
<Notify><CmdType>Keepalive</CmdType><SN>1</SN><DeviceID>` + deviceID + `</DeviceID><Status>OK</Status></Notify>`
	req.SetBody([]byte(body))
	req.AppendHeader(sip.NewHeader("Content-Type", "Application/MANSCDP+xml"))
	req.SetTransport("UDP")

	tx, err := client.TransactionRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("发心跳事务: %v", err)
	}
	defer tx.Terminate()
	res := <-tx.Responses()
	if res == nil {
		t.Fatal("心跳无响应")
	}
	return int(res.StatusCode)
}

// TestKeepalive T5-测1(AC-4): 注册后发心跳 → 200 + Redis 在线态刷新 + keepalive_time 更新
func TestKeepalive(t *testing.T) {
	setupEnv(t)
	cleanupDevice(testDeviceID)
	defer cleanupDevice(testDeviceID)
	stop := startTestServer(t, testCfg())
	defer stop()

	// 先注册建档
	if code := doRegister(t, testDeviceID, testPassword, 3600); code != 200 {
		t.Fatalf("前置注册失败: %d", code)
	}
	// 删掉 Redis 在线态,验证心跳能重新写回
	_ = app.Cache.Del(context.Background(), gbdevice.OnlineKey(testDeviceID))

	code := sendKeepalive(t, testDeviceID)
	if code != 200 {
		t.Fatalf("心跳期望 200,实际 %d", code)
	}
	if !gbdevice.IsOnline(context.Background(), testDeviceID) {
		t.Error("心跳后 Redis 在线态未刷新")
	}
	d, _ := gbmodels.FindByDeviceID(context.Background(), testDeviceID)
	if d == nil || d.KeepaliveTime == nil {
		t.Error("心跳后 keepalive_time 未更新")
	}
}

// TestMalformedMessage T5-测3: 畸形 MESSAGE → 不 panic,回 200
func TestMalformedMessage(t *testing.T) {
	setupEnv(t)
	stop := startTestServer(t, testCfg())
	defer stop()

	ua, _ := sipgo.NewUA(sipgo.WithUserAgent("badmsg"))
	client, _ := sipgo.NewClient(ua, sipgo.WithClientHostname("127.0.0.1"))
	defer client.Close()
	recipient := sip.Uri{}
	sip.ParseUri("sip:34020000002000000001@127.0.0.1:"+strconv.Itoa(testSIPPort), &recipient)
	req := sip.NewRequest(sip.MESSAGE, recipient)
	req.SetBody([]byte("garbage-not-xml"))
	req.SetTransport("UDP")
	tx, _ := client.TransactionRequest(context.Background(), req)
	defer tx.Terminate()
	res := <-tx.Responses()
	if res == nil || res.StatusCode != 200 {
		t.Fatalf("畸形消息期望回 200,实际 %v", res)
	}
}
