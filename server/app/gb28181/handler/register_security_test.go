package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/icholy/digest"
	"github.com/stretchr/testify/require"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/device"
	gbsecurity "uvplatform.cn/uvp-gb28181/app/gb28181/security"
)

type fakeRegisterSecurity struct {
	nonces       []string
	issueCalls   int
	validateErr  error
	validatedNC  string
	events       []gbsecurity.Event
	trustedCalls []trustedRegisterCall
}

type trustedRegisterCall struct {
	deviceID string
	address  string
	expires  time.Duration
}

const (
	securityTestDeviceID = "34020000001320000088"
	securityTestServerID = "34020000002000000001"
	securityTestPassword = "12345678"
)

func securityTestCfg() gbconfig.Config {
	return gbconfig.Config{
		SIP: gbconfig.SIPConfig{
			Domain: "3402000000", ServerID: securityTestServerID, Password: securityTestPassword,
		},
	}
}

func (f *fakeRegisterSecurity) IssueNonce() (string, error) {
	f.issueCalls++
	if len(f.nonces) == 0 {
		return "", errors.New("no nonce configured")
	}
	nonce := f.nonces[0]
	f.nonces = f.nonces[1:]
	return nonce, nil
}

func TestRegisterTransactionReusesInitialChallengeForUDPReplay(t *testing.T) {
	security := &fakeRegisterSecurity{nonces: []string{"stable-nonce"}}
	handler := NewRegisterHandler(securityTestCfg())
	handler.SetSecurity(security)
	req := newSecurityRegisterRequest(securityTestDeviceID, securityTestServerID)
	first, replay := &captureServerTransaction{}, &captureServerTransaction{}

	handler.Handle(req, first)
	handler.Handle(req, replay)

	require.Equal(t, sip.StatusUnauthorized, first.response.StatusCode)
	require.Equal(t, sip.StatusUnauthorized, replay.response.StatusCode)
	require.Equal(t, first.response.GetHeader("WWW-Authenticate").Value(), replay.response.GetHeader("WWW-Authenticate").Value())
	require.Equal(t, 1, security.issueCalls)
}

func TestRegisterTransactionReplaysSuccessWithoutDuplicateSideEffects(t *testing.T) {
	security := &fakeRegisterSecurity{}
	handler := NewRegisterHandler(securityTestCfg())
	handler.SetSecurity(security)
	registerCalls := 0
	handler.handleRegister = func(context.Context, device.RegisterInfo, int) (bool, error) {
		registerCalls++
		return false, nil
	}
	req := authorizedRegisterRequest(t, "accepted-nonce", securityTestPassword)
	first, replay := &captureServerTransaction{}, &captureServerTransaction{}

	handler.Handle(req, first)
	handler.Handle(req, replay)

	require.Equal(t, sip.StatusOK, first.response.StatusCode)
	require.Equal(t, sip.StatusOK, replay.response.StatusCode)
	require.Equal(t, 1, registerCalls)
	require.Equal(t, 1, len(security.trustedCalls))
}

func (f *fakeRegisterSecurity) ValidateNonce(_ string, nonceCount string) error {
	f.validatedNC = nonceCount
	return f.validateErr
}

func (f *fakeRegisterSecurity) Record(event gbsecurity.Event) error {
	f.events = append(f.events, event)
	return nil
}

func (f *fakeRegisterSecurity) TrustEndpoint(deviceID, _ string, address string, expires time.Duration) error {
	f.trustedCalls = append(f.trustedCalls, trustedRegisterCall{deviceID: deviceID, address: address, expires: expires})
	return nil
}

type captureServerTransaction struct{ response *sip.Response }

func (t *captureServerTransaction) Terminate()                         {}
func (t *captureServerTransaction) OnTerminate(sip.FnTxTerminate) bool { return true }
func (t *captureServerTransaction) Done() <-chan struct{}              { return make(chan struct{}) }
func (t *captureServerTransaction) Err() error                         { return nil }
func (t *captureServerTransaction) Respond(response *sip.Response) error {
	t.response = response
	return nil
}
func (t *captureServerTransaction) Acks() <-chan *sip.Request    { return make(chan *sip.Request) }
func (t *captureServerTransaction) OnCancel(sip.FnTxCancel) bool { return true }

func TestRegisterChallengeUsesSecurityNonce(t *testing.T) {
	security := &fakeRegisterSecurity{nonces: []string{"signed-random-nonce"}}
	handler := NewRegisterHandler(securityTestCfg())
	handler.SetSecurity(security)
	tx := &captureServerTransaction{}

	handler.Handle(newSecurityRegisterRequest(securityTestDeviceID, securityTestServerID), tx)

	require.Equal(t, sip.StatusUnauthorized, tx.response.StatusCode)
	challenge, err := digest.ParseChallenge(tx.response.GetHeader("WWW-Authenticate").Value())
	require.NoError(t, err)
	require.Equal(t, "signed-random-nonce", challenge.Nonce)
}

func TestRegisterSecurityRecordsServerIDMismatch(t *testing.T) {
	security := &fakeRegisterSecurity{}
	handler := NewRegisterHandler(securityTestCfg())
	handler.SetSecurity(security)
	tx := &captureServerTransaction{}

	handler.Handle(newSecurityRegisterRequest(securityTestDeviceID, "34020000002000000099"), tx)

	require.Equal(t, sip.StatusForbidden, tx.response.StatusCode)
	require.Len(t, security.events, 1)
	require.Equal(t, gbsecurity.ReasonServerMismatch, security.events[0].Reason)
	require.Equal(t, "198.51.100.23", security.events[0].SourceIP)
}

func TestRegisterSecurityClassifiesNonceFailuresAndReturnsFreshChallenge(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		err    error
		reason gbsecurity.Reason
	}{
		{name: "invalid", err: gbsecurity.ErrNonceInvalid, reason: gbsecurity.ReasonNonceInvalid},
		{name: "expired", err: gbsecurity.ErrNonceExpired, reason: gbsecurity.ReasonNonceExpired},
		{name: "replay", err: gbsecurity.ErrNonceReplay, reason: gbsecurity.ReasonNonceReplay},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			security := &fakeRegisterSecurity{nonces: []string{"fresh-nonce"}, validateErr: testCase.err}
			handler := NewRegisterHandler(securityTestCfg())
			handler.SetSecurity(security)
			req := newSecurityRegisterRequest(securityTestDeviceID, securityTestServerID)
			challenge := &digest.Challenge{Realm: securityTestCfg().SIP.Domain, Nonce: "presented-nonce", Algorithm: "MD5"}
			credentials, err := digest.Digest(challenge, digest.Options{
				Method: "REGISTER", URI: req.Recipient.String(), Username: securityTestDeviceID, Password: securityTestPassword,
			})
			require.NoError(t, err)
			req.AppendHeader(sip.NewHeader("Authorization", credentials.String()))
			tx := &captureServerTransaction{}

			handler.Handle(req, tx)

			require.Equal(t, sip.StatusUnauthorized, tx.response.StatusCode)
			fresh, parseErr := digest.ParseChallenge(tx.response.GetHeader("WWW-Authenticate").Value())
			require.NoError(t, parseErr)
			require.Equal(t, "fresh-nonce", fresh.Nonce)
			require.Len(t, security.events, 1)
			require.Equal(t, testCase.reason, security.events[0].Reason)
			require.Empty(t, security.validatedNC, "legacy devices without qop/nc must remain supported")
		})
	}
}

func newSecurityRegisterRequest(deviceID, serverID string) *sip.Request {
	req := sip.NewRequest(sip.REGISTER, sip.Uri{Scheme: "sip", User: serverID, Host: "3402000000"})
	req.SetSource("198.51.100.23:5060")
	req.SetTransport("UDP")
	req.AppendHeader(sip.NewHeader("From", "<sip:"+deviceID+"@3402000000>;tag=test"))
	req.AppendHeader(sip.NewHeader("To", "<sip:"+serverID+"@3402000000>"))
	req.AppendHeader(sip.NewHeader("Call-ID", "security-register-test"))
	req.AppendHeader(sip.NewHeader("CSeq", "1 REGISTER"))
	req.AppendHeader(sip.NewHeader("Via", "SIP/2.0/UDP 198.51.100.23:5060;branch=z9hG4bK-test"))
	return req
}
