package gb28181

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/emiago/sipgo"
	sipwire "github.com/emiago/sipgo/sip"

	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/repository"
	cascaderuntime "uvplatform.cn/uvp-gb28181/app/gb28181/cascade/runtime"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/securestore"
	cascadeservice "uvplatform.cn/uvp-gb28181/app/gb28181/cascade/service"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/sipclient"
	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
	gbroutes "uvplatform.cn/uvp-gb28181/app/gb28181/routes"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"

	"go.uber.org/zap"
)

const (
	cascadeCredentialKeyEnv            = "UVP_GB28181_CASCADE_KEY"
	cascadeCredentialPurpose           = "upstream-password"
	cascadeCredentialVersion           = "v1"
	cascadeCredentialKeyWarningMessage = "国标级联凭据密钥不可用,配置写入受限,已有加密凭据平台运行受限"
	cascadeCredentialKeyWarningEvent   = "cascade.credential_key_unavailable"
)

type cascadeRuntimeLifecycle interface {
	cascadeservice.ManagementRuntime
	Shutdown(context.Context) error
}

var cascadeRuntimeManager cascadeRuntimeLifecycle

type cascadeKeepaliveEncoder struct {
	deviceID        string
	charsetOverride string
	sequence        atomic.Uint64
}

func newCascadeKeepaliveEncoder(deviceID, charsetOverride string) *cascadeKeepaliveEncoder {
	return &cascadeKeepaliveEncoder{deviceID: strings.TrimSpace(deviceID), charsetOverride: strings.TrimSpace(charsetOverride)}
}

func (e *cascadeKeepaliveEncoder) EncodeKeepalive(version protocol.Version) ([]byte, error) {
	if e == nil || !validCascadeGBID(e.deviceID) {
		return nil, fmt.Errorf("invalid cascade Keepalive device ID")
	}
	profile := protocol.ProfileFor(version)
	if e.charsetOverride != "" {
		charset := protocol.Charset(strings.ToUpper(e.charsetOverride))
		if !protocol.IsSupportedCharset(charset) {
			return nil, fmt.Errorf("unsupported cascade Keepalive charset")
		}
		profile.Charset = charset
	}
	return manscdp.MarshalProfiledXML(profile, manscdp.Notify{
		CmdType:  manscdp.CmdKeepalive,
		SN:       fmt.Sprintf("%d", e.sequence.Add(1)),
		DeviceID: e.deviceID,
		Status:   "OK",
	})
}

type cascadePlatformClient struct {
	transactions *sipclient.TransactionClient
	keepalive    sipclient.KeepaliveEncoder
}

func (c *cascadePlatformClient) Register(ctx context.Context, expires int, callID string) sipclient.TransactionResult {
	return c.transactions.Register(ctx, expires, callID)
}

func (c *cascadePlatformClient) Logout(ctx context.Context, callID string) sipclient.TransactionResult {
	return c.transactions.Logout(ctx, callID)
}

func (c *cascadePlatformClient) Keepalive(ctx context.Context, callID string) sipclient.TransactionResult {
	return c.transactions.Keepalive(ctx, callID, c.keepalive)
}

type cascadePlatformClientFactory struct {
	transport sipclient.Transport
	cipher    *securestore.Cipher
	timeout   time.Duration
}

func newCascadePlatformClientFactory(transport sipclient.Transport, cipher *securestore.Cipher, timeout time.Duration) *cascadePlatformClientFactory {
	return &cascadePlatformClientFactory{transport: transport, cipher: cipher, timeout: timeout}
}

func (f *cascadePlatformClientFactory) NewClient(platform model.GbCascadePlatform) (cascaderuntime.PlatformClient, error) {
	if f == nil || f.transport == nil {
		return nil, fmt.Errorf("cascade SIP transport is unavailable")
	}
	profile := effectiveCascadeProfile(platform)
	requestFactory, err := sipclient.NewRequestFactory(sipclient.Identity{
		UpstreamServerID: platform.UpstreamServerID,
		UpstreamDomain:   platform.UpstreamDomain,
		Host:             platform.Host,
		Port:             platform.Port,
		LocalDeviceID:    platform.LocalDeviceID,
		LocalDomain:      platform.LocalDomain,
		LocalIP:          platform.LocalSIPIP,
		LocalPort:        platform.LocalSIPPort,
		Transport:        platform.Transport,
		Profile:          profile,
	})
	if err != nil {
		return nil, err
	}
	credentials, err := f.credentials(platform)
	if err != nil {
		return nil, err
	}
	transactions, err := sipclient.NewTransactionClient(requestFactory, f.transport, credentials, f.timeout)
	if err != nil {
		return nil, err
	}
	return &cascadePlatformClient{
		transactions: transactions,
		keepalive:    newCascadeKeepaliveEncoder(platform.LocalDeviceID, platform.CharsetOverride),
	}, nil
}

func (f *cascadePlatformClientFactory) credentials(platform model.GbCascadePlatform) (sipclient.Credentials, error) {
	username := strings.TrimSpace(platform.AuthUsername)
	if username == "" {
		username = platform.LocalDeviceID
	}
	if len(platform.SecretCiphertext) == 0 {
		return sipclient.Credentials{Username: username}, nil
	}
	if f.cipher == nil {
		return sipclient.Credentials{}, securestore.ErrKeyUnavailable
	}
	password, err := f.cipher.Decrypt(cascadeCredentialPurpose, securestore.Envelope{
		Ciphertext: platform.SecretCiphertext,
		Nonce:      platform.SecretNonce,
		Algorithm:  platform.SecretAlg,
		KeyVersion: platform.SecretKeyVersion,
	})
	if err != nil {
		return sipclient.Credentials{}, fmt.Errorf("open cascade credential: %w", err)
	}
	return sipclient.Credentials{Username: username, Password: string(password)}, nil
}

func effectiveCascadeProfile(platform model.GbCascadePlatform) protocol.Version {
	switch platform.ProfileOverride {
	case model.CascadeProfileOverride2022:
		return protocol.Version2022
	case model.CascadeProfileOverride2016:
		return protocol.Version2016
	}
	if strings.TrimSpace(platform.EffectiveVersion) == protocol.Version2022 {
		return protocol.Version2022
	}
	return protocol.Version2016
}

type cascadeResourceAcquirer struct {
	registry *sipclient.ListenerRegistry
}

func newCascadeResourceAcquirer(cfg gbconfig.Config) *cascadeResourceAcquirer {
	registry := sipclient.NewListenerRegistry(func(endpoint sipclient.ListenerEndpoint) (func() error, error) {
		if endpoint.Port != cfg.SIP.Port || !cascadeTransportEnabled(cfg.SIP.Transport, endpoint.Transport) || !cascadeListenerIPMatches(cfg.SIP, endpoint.IP) {
			return nil, fmt.Errorf("cascade endpoint is not served by the shared SIP listener")
		}
		return func() error { return nil }, nil
	})
	return &cascadeResourceAcquirer{registry: registry}
}

func (a *cascadeResourceAcquirer) Acquire(platform model.GbCascadePlatform) (func() error, error) {
	if a == nil || a.registry == nil {
		return nil, fmt.Errorf("cascade shared SIP listener is unavailable")
	}
	return a.registry.Acquire(sipclient.ListenerEndpoint{
		IP:        platform.LocalSIPIP,
		Port:      platform.LocalSIPPort,
		Transport: platform.Transport,
	})
}

func cascadeTransportEnabled(configured []string, wanted string) bool {
	for _, transport := range configured {
		if strings.EqualFold(strings.TrimSpace(transport), strings.TrimSpace(wanted)) {
			return true
		}
	}
	return false
}

func cascadeListenerIPMatches(cfg gbconfig.SIPConfig, localIP string) bool {
	listenIP := strings.TrimSpace(cfg.ListenIP)
	if listenIP == "0.0.0.0" || listenIP == "::" {
		return true
	}
	return localIP == listenIP || localIP == strings.TrimSpace(cfg.AdvertiseIP)
}

func validCascadeGBID(value string) bool {
	if len(value) != 20 {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

type cascadeClientProvider interface {
	NewCascadeClient() (*sipgo.Client, error)
}

func setupCascadeManagement(runtime cascadeservice.ManagementRuntime, cipher *securestore.Cipher, pusher cascadeservice.CatalogPusher) {
	db := app.DB()
	if db == nil {
		gbroutes.SetCascadeManagementService(nil, nil)
		return
	}
	store := repository.NewGormRepository(db)
	if runtime != nil {
		runtime = cascadeVideoManagementRuntime{runtime}
	}
	service := cascadeservice.NewManagementService(store, cipher, runtime, nil)
	service.SetCatalogPusher(pusher)
	gbroutes.SetCascadeManagementService(service, db)
}

func loadCascadeCredentialCipher() (*securestore.Cipher, error) {
	cipher, err := securestore.LoadOrCreateCipher(cascadeCredentialKeyEnv, filepath.Join(app.BasePath, "data", "secrets", "cascade.key"), cascadeCredentialVersion)
	if err != nil {
		return nil, err
	}
	return cipher, nil
}

func cascadeCredentialWarningNeeded(err error, alreadyReported bool) bool {
	return err != nil && !alreadyReported
}

func warnCascadeCredentialKeyUnavailable() {
	if app.ZapLog == nil {
		return
	}
	app.ZapLog.Warn(cascadeCredentialKeyWarningMessage,
		zap.String("event", cascadeCredentialKeyWarningEvent),
		zap.String("env", cascadeCredentialKeyEnv),
		zap.String("config_write", "restricted"),
		zap.String("encrypted_platform_runtime", "restricted"))
}

func startCascadeRuntime(cfg gbconfig.Config, server sipRuntimeServer, credentialWarningReported bool) error {
	provider, ok := server.(cascadeClientProvider)
	if !ok {
		return fmt.Errorf("shared SIP server does not expose a cascade client")
	}
	client, err := provider.NewCascadeClient()
	if err != nil {
		return err
	}
	transport, err := sipclient.NewSipgoTransport(client)
	if err != nil {
		return err
	}
	cipher, cipherErr := loadCascadeCredentialCipher()
	if cipherErr != nil && !errors.Is(cipherErr, securestore.ErrKeyUnavailable) {
		return cipherErr
	}
	if cascadeCredentialWarningNeeded(cipherErr, credentialWarningReported) {
		warnCascadeCredentialKeyUnavailable()
	}
	store := repository.NewGormRepository(app.DB())
	manager := cascaderuntime.NewManager(cascaderuntime.Dependencies{
		Store:     store,
		Clients:   newCascadePlatformClientFactory(transport, cipher, gbconfig.SIPCommandTimeout()),
		Resources: newCascadeResourceAcquirer(cfg),
	})
	ctx, cancel := context.WithTimeout(context.Background(), gbconfig.SIPCommandTimeout())
	defer cancel()
	if err := manager.Reload(ctx); err != nil {
		_ = manager.Shutdown(ctx)
		return fmt.Errorf("load cascade platforms: %w", err)
	}
	if receiver, ok := server.(interface {
		SetCascadeMessageHandler(func(*sipwire.Request, sipwire.ServerTransaction) bool)
	}); ok {
		receiver.SetCascadeMessageHandler(newCascadeMessageHandler(store, newCascadePlatformClientFactory(transport, cipher, gbconfig.SIPCommandTimeout())))
	}
	cascadeRuntimeManager = manager
	setupCascadeManagement(manager, cipher, newCascadeCatalogPusher(store, newCascadePlatformClientFactory(transport, cipher, gbconfig.SIPCommandTimeout())))
	return nil
}

func stopCascadeRuntime(ctx context.Context) error {
	stopCascadeVideoRuntime(ctx)
	manager := cascadeRuntimeManager
	cascadeRuntimeManager = nil
	if manager == nil {
		return nil
	}
	err := manager.Shutdown(ctx)
	if err != nil {
		app.Log(ctx).Named("cascade").Error("Cascade shutdown incomplete", zap.String("event", "cascade.shutdown_incomplete"), logging.Error(err))
	}
	return err
}

var _ cascaderuntime.ClientFactory = (*cascadePlatformClientFactory)(nil)
var _ cascaderuntime.ResourceAcquirer = (*cascadeResourceAcquirer)(nil)

// Management changes invalidate media dialogs whose upstream identity changed.
type cascadeVideoManagementRuntime struct {
	cascadeservice.ManagementRuntime
}

func (r cascadeVideoManagementRuntime) Reload(ctx context.Context) error {
	if h := cascadeVideo.Load(); h != nil {
		if err := h.revalidate(ctx); err != nil {
			return err
		}
	}
	if r.ManagementRuntime == nil {
		return nil
	}
	return r.ManagementRuntime.Reload(ctx)
}
func (r cascadeVideoManagementRuntime) PlatformIDs() []uint64 {
	if r.ManagementRuntime == nil {
		return nil
	}
	return r.ManagementRuntime.PlatformIDs()
}
