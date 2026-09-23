package integration

// This opt-in test runs the actual desktop simulator against the platform's
// SIP registration, message handler and UAC, with an isolated SQLite database.
// UVP_UPGRADE_SIMULATOR must point to a freshly built register_one executable.
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/handler"
	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.cn/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/gb28181/upgrade"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/ymlconfig"
)

const simulatorDeviceCode = "34020000001320000991"
const simulatorPlatformCode = "34020000002000000991"

type upgradeHarness struct {
	db            *gorm.DB
	uac           *uac.UAC
	messages      *handler.MessageHandler
	port          int
	executable    string
	stateDir      string
	mu            sync.Mutex
	results       []string
	infoResponses chan *manscdp.DeviceInfoResponse
}

func newUpgradeHarness(t *testing.T) *upgradeHarness {
	t.Helper()
	executable := os.Getenv("UVP_UPGRADE_SIMULATOR")
	if executable == "" {
		t.Skip("set UVP_UPGRADE_SIMULATOR to the current desktop register_one executable")
	}
	info, err := os.Stat(executable)
	require.NoError(t, err)
	require.False(t, info.IsDir())
	configDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yml"), []byte("gormv2:\n  usedbtype: mysql\ngb28181:\n  device:\n    preallocation_mode: true\n"), 0600))
	oldDB, oldConfig, oldLog := app.GormDbMysql, app.ConfigYml, app.ZapLog
	t.Cleanup(func() { app.GormDbMysql, app.ConfigYml, app.ZapLog = oldDB, oldConfig, oldLog })
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "upgrade.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	app.GormDbMysql, app.ConfigYml, app.ZapLog = db, ymlconfig.CreateYamlFactory(configDir), zap.NewNop()
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbDeviceStatusEvent{}, &gbmodels.GbPTZOperation{}))
	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: simulatorDeviceCode, OwnerDeptID: 1, Manufacturer: "UVP", Model: "Desktop-Sim", Firmware: "0.1.0-dev", Status: gbmodels.DeviceStatusOffline}).Error)
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	port := conn.LocalAddr().(*net.UDPAddr).Port
	ua, err := sipgo.NewUA()
	require.NoError(t, err)
	t.Cleanup(func() { _ = ua.Close() })
	server, err := sipgo.NewServer(ua)
	require.NoError(t, err)
	t.Cleanup(func() { _ = server.Close() })
	sender, err := uac.New(ua, simulatorPlatformCode, "3402000000", "127.0.0.1", port, true)
	require.NoError(t, err)
	cfg := gbconfig.Config{SIP: gbconfig.SIPConfig{ServerID: simulatorPlatformCode, Domain: "3402000000", Password: "isolated-upgrade-test", XGBVersion: "3.0"}}
	messageHandler := handler.NewMessageHandler(cfg)
	registration := handler.NewRegisterHandler(cfg)
	server.OnRegister(registration.Handle)
	h := &upgradeHarness{db: db, uac: sender, messages: messageHandler, port: port, executable: executable, stateDir: t.TempDir(), infoResponses: make(chan *manscdp.DeviceInfoResponse, 32)}
	server.OnMessage(func(req *sip.Request, tx sip.ServerTransaction) {
		if head, err := manscdp.ParseHead(req.Body()); err == nil && head.CmdType == "DeviceUpgradeResult" {
			h.mu.Lock()
			h.results = append(h.results, string(req.Body()))
			h.mu.Unlock()
		}
		messageHandler.Handle(req, tx)
		if head, err := manscdp.ParseHead(req.Body()); err == nil && head.CmdType == manscdp.CmdDeviceInfo {
			if response, err := manscdp.ParseDeviceInfoResponse(req.Body()); err == nil {
				select {
				case h.infoResponses <- response:
				default:
				}
			}
		}
	})
	go func() { _ = server.ServeUDP(conn) }()
	return h
}

func (h *upgradeHarness) startSimulator(t *testing.T) func() {
	t.Helper()
	cmd := exec.Command(h.executable)
	filtered := make([]string, 0)
	for _, item := range os.Environ() {
		key := strings.SplitN(item, "=", 2)[0]
		switch key {
		case "SERVER_HOST", "SERVER_PORT", "SERVER_DOMAIN", "SERVER_ID", "DEVICE_ID", "PASSWORD", "GB_VERSION", "VIDEO_SOURCE", "ALARM_AFTER_SECS", "POSITION_AFTER_SECS", "UVP_SIM_UPGRADE_STATE_DIR":
			continue
		}
		filtered = append(filtered, item)
	}
	cmd.Env = append(filtered, "SERVER_HOST=127.0.0.1", "SERVER_PORT="+strconv.Itoa(h.port), "SERVER_DOMAIN=3402000000", "SERVER_ID="+simulatorPlatformCode, "DEVICE_ID="+simulatorDeviceCode, "PASSWORD=isolated-upgrade-test", "GB_VERSION=2022", "UVP_SIM_UPGRADE_STATE_DIR="+h.stateDir)
	logs, err := os.Create(filepath.Join(t.TempDir(), "simulator.log"))
	require.NoError(t, err)
	cmd.Stdout, cmd.Stderr = logs, logs
	require.NoError(t, cmd.Start())
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			_ = cmd.Process.Signal(os.Interrupt)
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				_ = cmd.Process.Kill()
				<-done
			}
			_ = logs.Close()
			if t.Failed() {
				if b, e := os.ReadFile(logs.Name()); e == nil {
					t.Logf("simulator: %s", b)
				}
			}
		})
	}
	t.Cleanup(stop)
	require.Eventually(t, func() bool {
		var device gbmodels.GbDevice
		return h.db.Where("device_id = ?", simulatorDeviceCode).First(&device).Error == nil && device.Status == gbmodels.DeviceStatusOnline && device.EffectiveVersion == "2022"
	}, 20*time.Second, 25*time.Millisecond, "simulator must complete the real digest REGISTER handshake")
	return stop
}

func (h *upgradeHarness) device(t *testing.T) gbmodels.GbDevice {
	t.Helper()
	var device gbmodels.GbDevice
	require.NoError(t, h.db.Where("device_id = ?", simulatorDeviceCode).First(&device).Error)
	return device
}

func (h *upgradeHarness) queryFirmware(t *testing.T, sn int) string {
	t.Helper()
	device := h.device(t)
	body, err := manscdp.BuildDeviceInfoQueryWithProfile(protocol.ProfileFor(protocol.Version2022), simulatorDeviceCode, sn)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := h.uac.SendMessageTracked(ctx, simulatorDeviceCode, net.JoinHostPort(device.IP, strconv.Itoa(device.Port)), "UDP", body)
	require.NoError(t, err)
	require.Equal(t, 200, result.StatusCode)
	for {
		select {
		case response := <-h.infoResponses:
			if response.SN == sn && response.DeviceID == simulatorDeviceCode {
				return response.Firmware
			}
		case <-ctx.Done():
			t.Fatal("DeviceInfo query received no matching business response")
			return ""
		}
	}
}

func TestDesktopSimulatorRegistrationHarness(t *testing.T) {
	h := newUpgradeHarness(t)
	h.startSimulator(t)
	require.Equal(t, "0.1.0-dev", h.queryFirmware(t, 900001))
}

func TestDesktopSimulatorFirmwareUpgradeRoundTrip(t *testing.T) {
	h := newUpgradeHarness(t)
	require.NoError(t, h.db.AutoMigrate(&gbmodels.GbDeviceFirmwareUpgrade{}))
	allocator, err := ptz.NewService(h.db, h.uac, time.Now)
	require.NoError(t, err)
	service, err := upgrade.NewService(h.db, h.uac, allocator, time.Now)
	require.NoError(t, err)
	h.messages.SetUpgradeProcessor(service)
	stop := h.startSimulator(t)
	require.Equal(t, "0.1.0-dev", h.queryFirmware(t, 900010))
	initialRegistration := h.device(t).RegisterTime
	require.NotNil(t, initialRegistration)
	var downloads atomic.Int32
	downloadStarted := make(chan struct{}, 1)
	releaseDownload := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseDownload) }) }
	t.Cleanup(release)
	payload := "simulated firmware bytes for version 0.2.0"
	digest := sha256.Sum256([]byte(payload))
	fixture := map[string]string{"format": "uvp-simulator-firmware-v1", "manufacturer": "UVP", "model": "Desktop-Sim", "firmware": "0.2.0", "payload": payload, "sha256": hex.EncodeToString(digest[:])}
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/valid.json" {
			select {
			case downloadStarted <- struct{}{}:
			default:
			}
			select {
			case <-releaseDownload:
			case <-r.Context().Done():
				return
			}
			_ = json.NewEncoder(w).Encode(fixture)
		} else {
			_, _ = w.Write([]byte(`{"format":"uvp-simulator-firmware-v1","manufacturer":"UVP","model":"Desktop-Sim","firmware":"0.3.0","payload":"corrupt","sha256":"invalid"}`))
		}
	}))
	t.Cleanup(func() { release(); httpServer.Close() })
	target := func() upgrade.Target {
		device := h.device(t)
		return upgrade.Target{DeviceID: device.ID, DeviceCode: device.DeviceID, IP: device.IP, Port: device.Port, Transport: device.Transport, DeviceOnline: true, Profile: protocol.ProfileFor(protocol.Version2022)}
	}
	request := upgrade.Request{Confirmed: true, IdempotencyKey: "integration-success", Firmware: "0.2.0", FileURL: httpServer.URL + "/valid.json", Manufacturer: "UVP", ActorID: 1, ActorDeptID: 1}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	op, dedup, err := service.Execute(ctx, target(), request)
	require.NoError(t, err)
	require.False(t, dedup)
	select {
	case <-downloadStarted:
	case <-ctx.Done():
		t.Fatal("simulator did not fetch the firmware URL")
	}
	require.Eventually(t, func() bool {
		current, e := service.Get(ctx, op.OperationID)
		return e == nil && current.Status == gbmodels.FirmwareUpgradeAccepted
	}, 3*time.Second, 20*time.Millisecond, "business OK must mean accepted while download is incomplete")
	require.Equal(t, "0.1.0-dev", h.queryFirmware(t, 900011))
	release()
	require.Eventually(t, func() bool {
		current, e := service.Get(ctx, op.OperationID)
		return e == nil && current.Status == gbmodels.FirmwareUpgradeSucceeded
	}, 10*time.Second, 25*time.Millisecond, "the final notification must complete the operation")
	final, err := service.Get(ctx, op.OperationID)
	require.NoError(t, err)
	require.Equal(t, "0.2.0", final.CurrentFirmware)
	require.Equal(t, "0.2.0", h.queryFirmware(t, 900012))
	require.True(t, h.device(t).RegisterTime.After(*initialRegistration), "success must include a new REGISTER handshake")
	replay, dedup, err := service.Execute(ctx, target(), request)
	require.NoError(t, err)
	require.True(t, dedup)
	require.Equal(t, op.OperationID, replay.OperationID)
	require.EqualValues(t, 1, downloads.Load())
	stop()
	h.startSimulator(t)
	require.Equal(t, "0.2.0", h.queryFirmware(t, 900013), "simulator version must survive a new process")
	// Send the same protocol session again after a new simulator process. This
	// exercises device-side durable deduplication, not just the platform's HTTP key.
	h.mu.Lock()
	priorResultCount := len(h.results)
	h.mu.Unlock()
	duplicateBody, err := manscdp.BuildDeviceUpgradeControlWithProfile(protocol.ProfileFor(protocol.Version2022), simulatorDeviceCode, allocator.NextSN(), manscdp.DeviceUpgradeCommand{Firmware: request.Firmware, FileURL: request.FileURL, Manufacturer: request.Manufacturer, SessionID: op.SessionID})
	require.NoError(t, err)
	duplicateDevice := h.device(t)
	duplicateResponse, err := h.uac.SendMessageTracked(ctx, simulatorDeviceCode, net.JoinHostPort(duplicateDevice.IP, strconv.Itoa(duplicateDevice.Port)), "UDP", duplicateBody)
	require.NoError(t, err)
	require.Equal(t, 200, duplicateResponse.StatusCode)
	require.Eventually(t, func() bool { h.mu.Lock(); defer h.mu.Unlock(); return len(h.results) > priorResultCount }, 5*time.Second, 25*time.Millisecond, "duplicate session must replay its stored final result")
	require.EqualValues(t, 1, downloads.Load(), "duplicate session must not fetch the package again")

	request.IdempotencyKey = "integration-corrupt"
	request.Firmware = "0.3.0"
	request.FileURL = httpServer.URL + "/corrupt.json"
	failed, _, err := service.Execute(ctx, target(), request)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		current, e := service.Get(ctx, failed.OperationID)
		return e == nil && current.Status == gbmodels.FirmwareUpgradeFailed
	}, 10*time.Second, 25*time.Millisecond)
	failed, err = service.Get(ctx, failed.OperationID)
	require.NoError(t, err)
	require.Equal(t, "02", failed.FailedReason)
	require.Equal(t, "0.2.0", failed.CurrentFirmware)
	require.Equal(t, "0.2.0", h.queryFirmware(t, 900014))
	require.EqualValues(t, 2, downloads.Load())
	h.mu.Lock()
	results := append([]string(nil), h.results...)
	h.mu.Unlock()
	require.GreaterOrEqual(t, len(results), 2)
	for _, body := range results {
		require.Contains(t, body, "<UpgradeResult>")
		require.NotContains(t, body, "<Percent>")
		require.NotContains(t, body, "<Result>")
	}
	t.Log("real REGISTER digest + DeviceControl business acceptance + HTTP download + new REGISTER + DeviceUpgradeResult + DeviceInfo version; persistent restart and corrupt-package failure verified")
}

func TestDesktopSimulatorFinalResultRetryDoesNotReapply(t *testing.T) {
	h := newUpgradeHarness(t)
	require.NoError(t, h.db.AutoMigrate(&gbmodels.GbDeviceFirmwareUpgrade{}))
	allocator, err := ptz.NewService(h.db, h.uac, time.Now)
	require.NoError(t, err)
	service, err := upgrade.NewService(h.db, h.uac, allocator, time.Now)
	require.NoError(t, err)
	h.messages.SetUpgradeProcessor(service)
	h.startSimulator(t)
	var failedOnce atomic.Bool
	require.NoError(t, h.db.Callback().Update().Before("gorm:update").Register("integration:fail-first-upgrade-final", func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Table != "gb_device_firmware_upgrade" {
			return
		}
		updates, ok := tx.Statement.Dest.(map[string]interface{})
		if !ok {
			return
		}
		if updates["status"] == gbmodels.FirmwareUpgradeSucceeded && failedOnce.CompareAndSwap(false, true) {
			_ = tx.AddError(errors.New("injected transient final-result storage failure"))
		}
	}))
	t.Cleanup(func() { _ = h.db.Callback().Update().Remove("integration:fail-first-upgrade-final") })
	var downloads atomic.Int32
	payload := "retry test payload"
	digest := sha256.Sum256([]byte(payload))
	fixture := map[string]string{"format": "uvp-simulator-firmware-v1", "manufacturer": "UVP", "model": "Desktop-Sim", "firmware": "0.2.0", "payload": payload, "sha256": hex.EncodeToString(digest[:])}
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { downloads.Add(1); _ = json.NewEncoder(w).Encode(fixture) }))
	t.Cleanup(httpServer.Close)
	device := h.device(t)
	target := upgrade.Target{DeviceID: device.ID, DeviceCode: device.DeviceID, IP: device.IP, Port: device.Port, Transport: device.Transport, DeviceOnline: true, Profile: protocol.ProfileFor(protocol.Version2022)}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	op, _, err := service.Execute(ctx, target, upgrade.Request{Confirmed: true, IdempotencyKey: "result-retry", Firmware: "0.2.0", FileURL: httpServer.URL + "/firmware.json", Manufacturer: "UVP", ActorID: 1, ActorDeptID: 1})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		current, e := service.Get(ctx, op.OperationID)
		return e == nil && current.Status == gbmodels.FirmwareUpgradeSucceeded
	}, 10*time.Second, 25*time.Millisecond, "failed final persistence must be retried as a result, never a new upgrade")
	require.True(t, failedOnce.Load(), "the persistence failure must actually have been injected")
	require.EqualValues(t, 1, downloads.Load(), "result retry must not fetch or reapply firmware")
	h.mu.Lock()
	results := append([]string(nil), h.results...)
	h.mu.Unlock()
	require.GreaterOrEqual(t, len(results), 2, "device must send the final notification again")
	require.Equal(t, "0.2.0", h.queryFirmware(t, 900020))
	t.Log("first final-result database write failed; device retried result; platform converged; package downloaded once")
}
