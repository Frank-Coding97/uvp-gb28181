package gb28181

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/securestore"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/sipclient"
	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

func TestCascadeKeepaliveEncoderUsesProfileCharsetAndRequiredFields(t *testing.T) {
	for _, version := range []protocol.Version{protocol.Version2016, protocol.Version2022} {
		t.Run(string(version), func(t *testing.T) {
			encoder := newCascadeKeepaliveEncoder("34020000002000000002", "")
			body, err := encoder.EncodeKeepalive(version)
			require.NoError(t, err)

			profile := protocol.ProfileFor(version)
			var notify manscdp.Notify
			require.NoError(t, manscdp.DecodeProfiledXML(profile, body, &notify))
			require.Equal(t, manscdp.CmdKeepalive, notify.CmdType)
			require.Equal(t, "1", notify.SN)
			require.Equal(t, "34020000002000000002", notify.DeviceID)
			require.Equal(t, "OK", notify.Status)
			require.Contains(t, string(body), `encoding="`+string(profile.Charset)+`"`)

			body, err = encoder.EncodeKeepalive(version)
			require.NoError(t, err)
			require.NoError(t, manscdp.DecodeProfiledXML(profile, body, &notify))
			require.Equal(t, "2", notify.SN)
		})
	}
}

type cascadeTransportFake struct {
	requests    []*sip.Request
	credentials sipclient.Credentials
}

func (f *cascadeTransportFake) Do(_ context.Context, request *sip.Request) (*sip.Response, error) {
	f.requests = append(f.requests, request)
	response := sip.NewResponseFromRequest(request, sip.StatusUnauthorized, "Unauthorized", nil)
	response.AppendHeader(sip.NewHeader("WWW-Authenticate", `Digest realm="3402000000", nonce="nonce-1"`))
	return response, nil
}

func (f *cascadeTransportFake) DoDigestAuth(_ context.Context, request *sip.Request, _ *sip.Response, credentials sipclient.Credentials) (*sip.Response, error) {
	f.credentials = credentials
	return sip.NewResponseFromRequest(request, sip.StatusOK, "OK", nil), nil
}

func TestCascadePlatformClientFactoryDecryptsCredentialAndBuildsProfiledClient(t *testing.T) {
	cipher, err := securestore.NewCipher([]byte("0123456789abcdef0123456789abcdef"), "v1")
	require.NoError(t, err)
	envelope, err := cipher.Encrypt(cascadeCredentialPurpose, []byte("secret-123"))
	require.NoError(t, err)
	transport := &cascadeTransportFake{}
	factory := newCascadePlatformClientFactory(transport, cipher, time.Second)

	client, err := factory.NewClient(model.GbCascadePlatform{
		UpstreamServerID: "34020000002000000001", UpstreamDomain: "3402000000", Host: "192.0.2.10", Port: 5060,
		LocalDeviceID: "34020000002000000002", LocalDomain: "3402000000", LocalSIPIP: "192.0.2.20", LocalSIPPort: 5060,
		AuthUsername: "cascade-user", SecretNonce: envelope.Nonce, SecretCiphertext: envelope.Ciphertext,
		SecretAlg: envelope.Algorithm, SecretKeyVersion: envelope.KeyVersion,
		ProfileOverride: model.CascadeProfileOverride2022, Transport: "UDP",
	})
	require.NoError(t, err)

	result := client.Register(context.Background(), 3600, "register-1")
	require.True(t, result.Success)
	require.Equal(t, sipclient.Credentials{Username: "cascade-user", Password: "secret-123"}, transport.credentials)
	require.Len(t, transport.requests, 1)
	require.Equal(t, "3.0", transport.requests[0].GetHeader("X-GB-Ver").Value())
}

func TestCascadePlatformClientFactoryRejectsEncryptedCredentialWithoutKey(t *testing.T) {
	factory := newCascadePlatformClientFactory(&cascadeTransportFake{}, nil, time.Second)
	_, err := factory.NewClient(model.GbCascadePlatform{
		UpstreamServerID: "34020000002000000001", UpstreamDomain: "3402000000", Host: "192.0.2.10", Port: 5060,
		LocalDeviceID: "34020000002000000002", LocalDomain: "3402000000", LocalSIPIP: "192.0.2.20", LocalSIPPort: 5060,
		SecretCiphertext: []byte("encrypted"), ProfileOverride: model.CascadeProfileOverride2016, Transport: "UDP",
	})
	require.ErrorIs(t, err, securestore.ErrKeyUnavailable)
	require.NotContains(t, err.Error(), "encrypted")
}

func TestCascadeCredentialWarningNeededIsPerAssembly(t *testing.T) {
	for _, tc := range []struct {
		name            string
		err             error
		alreadyReported bool
		want            bool
	}{
		{name: "no error", want: false},
		{name: "first missing key", err: securestore.ErrKeyUnavailable, want: true},
		{name: "same assembly already reported", err: securestore.ErrKeyUnavailable, alreadyReported: true, want: false},
		{name: "invalid key first report", err: securestore.ErrInvalidKey, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, cascadeCredentialWarningNeeded(tc.err, tc.alreadyReported))
		})
	}
}

func TestLoggingCascadeWarningOutput(t *testing.T) {
	core, observed := observer.New(zap.DebugLevel)
	previousLogger := app.ZapLog
	app.ZapLog = zap.New(core)
	t.Cleanup(func() { app.ZapLog = previousLogger })

	warnCascadeCredentialKeyUnavailable()

	require.Len(t, observed.All(), 1)
	entry := observed.All()[0]
	require.Equal(t, zap.WarnLevel, entry.Level)
	require.Equal(t, cascadeCredentialKeyWarningMessage, entry.Message)
	require.Empty(t, entry.Stack)
	fields := entry.ContextMap()
	require.Equal(t, cascadeCredentialKeyWarningEvent, fields["event"])
	require.Equal(t, cascadeCredentialKeyEnv, fields["env"])
	require.Equal(t, "restricted", fields["config_write"])
	require.Equal(t, "restricted", fields["encrypted_platform_runtime"])
}

func TestCascadeResourceAcquirerOnlyUsesExistingSharedListener(t *testing.T) {
	acquirer := newCascadeResourceAcquirer(gbconfig.Config{SIP: gbconfig.SIPConfig{
		ListenIP: "0.0.0.0", AdvertiseIP: "192.0.2.20", Port: 5060, Transport: []string{"udp", "tcp"},
	}})
	platform := model.GbCascadePlatform{LocalSIPIP: "192.0.2.20", LocalSIPPort: 5060, Transport: "UDP"}

	release, err := acquirer.Acquire(platform)
	require.NoError(t, err)
	require.NoError(t, release())

	platform.LocalSIPPort = 5062
	_, err = acquirer.Acquire(platform)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "shared SIP listener"))
}

type cascadeRuntimeLifecycleFake struct{ events *[]string }

func (f *cascadeRuntimeLifecycleFake) Reload(context.Context) error { return nil }
func (f *cascadeRuntimeLifecycleFake) PlatformIDs() []uint64        { return nil }
func (f *cascadeRuntimeLifecycleFake) Shutdown(context.Context) error {
	*f.events = append(*f.events, "cascade.shutdown")
	return nil
}

func TestStopSIPDependenciesStopsCascadeBeforeSharedSIP(t *testing.T) {
	events := []string{}
	previousServer, previousCascade := sipServer, cascadeRuntimeManager
	defer func() {
		sipServer, cascadeRuntimeManager = previousServer, previousCascade
	}()

	cascadeRuntimeManager = &cascadeRuntimeLifecycleFake{events: &events}
	sipServer = &fakeSIPRuntimeServer{events: &events}
	stopSIPDependencies(context.Background())

	require.Equal(t, []string{"record.sink.clear", "cascade.shutdown", "sip.shutdown"}, events)
	require.Nil(t, cascadeRuntimeManager)
}

func TestCascadeRuntimeShutdownFailureDoesNotSkipSharedSIPShutdown(t *testing.T) {
	events := []string{}
	previousServer, previousCascade := sipServer, cascadeRuntimeManager
	defer func() {
		sipServer, cascadeRuntimeManager = previousServer, previousCascade
	}()

	cascadeRuntimeManager = cascadeRuntimeLifecycleFunc(func(context.Context) error {
		events = append(events, "cascade.shutdown")
		return errors.New("upstream timeout")
	})
	sipServer = &fakeSIPRuntimeServer{events: &events}
	stopSIPDependencies(context.Background())

	require.Equal(t, []string{"record.sink.clear", "cascade.shutdown", "sip.shutdown"}, events)
}

type cascadeRuntimeLifecycleFunc func(context.Context) error

func (f cascadeRuntimeLifecycleFunc) Reload(context.Context) error { return nil }
func (f cascadeRuntimeLifecycleFunc) PlatformIDs() []uint64        { return nil }
func (f cascadeRuntimeLifecycleFunc) Shutdown(ctx context.Context) error {
	return f(ctx)
}
