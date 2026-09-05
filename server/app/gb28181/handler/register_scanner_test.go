package handler

import (
	"context"
	"fmt"
	"github.com/emiago/sipgo/sip"
	"github.com/icholy/digest"
	"github.com/stretchr/testify/require"
	"testing"
	"uvplatform.cn/uvp-gb28181/app/gb28181/device"
	gbsecurity "uvplatform.cn/uvp-gb28181/app/gb28181/security"
)

func TestRegisterInvalidIDsNeverReachBusinessOrBanFixedConfiguration(t *testing.T) {
	for _, transport := range []string{"TCP", "UDP"} {
		t.Run(transport, func(t *testing.T) {
			runtime := gbsecurity.NewRuntime(gbsecurity.DefaultPolicy(), nil, nil, nil)
			h := NewRegisterHandler(securityTestCfg())
			h.SetSecurity(runtime)
			calls := 0
			h.handleRegister = func(context.Context, device.RegisterInfo, int) (bool, error) { calls++; return false, nil }
			for i := 0; i < 100; i++ {
				req := newSecurityRegisterRequest("5838", securityTestServerID)
				req.SetTransport(transport)
				setRegisterCSeq(req, i+1)
				tx := &captureServerTransaction{}
				h.Handle(req, tx)
				require.NotEqual(t, sip.StatusOK, tx.response.StatusCode)
			}
			require.Empty(t, runtime.Bans())
			require.Zero(t, calls)
			challengeReq := newSecurityRegisterRequest(securityTestDeviceID, securityTestServerID)
			challengeReq.SetTransport(transport)
			tx := &captureServerTransaction{}
			h.Handle(challengeReq, tx)
			challenge, err := digest.ParseChallenge(tx.response.GetHeader("WWW-Authenticate").Value())
			require.NoError(t, err)
			corrected := authorizedRegisterRequest(t, challenge.Nonce, securityTestPassword)
			corrected.SetTransport(transport)
			setRegisterCSeq(corrected, 101)
			tx = &captureServerTransaction{}
			h.Handle(corrected, tx)
			require.Equal(t, sip.StatusOK, tx.response.StatusCode)
			require.Equal(t, 1, calls)
			require.Empty(t, runtime.Bans())
		})
	}
}

func TestRegisterEnumerationBeforeServerIDMismatch(t *testing.T) {
	for _, transport := range []string{"TCP", "UDP"} {
		t.Run(transport, func(t *testing.T) {
			runtime := gbsecurity.NewRuntime(gbsecurity.DefaultPolicy(), nil, nil, nil)
			h := NewRegisterHandler(securityTestCfg())
			h.SetSecurity(runtime)
			for i := 0; i < 10; i++ {
				id := fmt.Sprint(5838 + i)
				req := newSecurityRegisterRequest(id, id)
				req.Recipient.User = ""
				req.SetTransport(transport)
				setRegisterCSeq(req, i+1)
				h.Handle(req, &captureServerTransaction{})
			}
			events := runtime.Events()
			found := false
			for _, event := range events {
				if event.Reason == gbsecurity.ReasonRegisterEnumeration {
					found = true
				}
			}
			require.True(t, found, "four-digit enumeration must not hide as server configuration")
			if transport == "TCP" {
				require.Len(t, runtime.Bans(), 1)
				require.True(t, runtime.Bans()[0].Decision.Permanent)
			} else {
				require.Empty(t, runtime.Bans())
			}
		})
	}
}

func TestRegisterSuccessCacheCannotCrossSources(t *testing.T) {
	h := NewRegisterHandler(securityTestCfg())
	calls := 0
	h.handleRegister = func(context.Context, device.RegisterInfo, int) (bool, error) { calls++; return false, nil }
	initial := newSecurityRegisterRequest(securityTestDeviceID, securityTestServerID)
	tx := &captureServerTransaction{}
	h.Handle(initial, tx)
	challenge, err := digest.ParseChallenge(tx.response.GetHeader("WWW-Authenticate").Value())
	require.NoError(t, err)
	req := authorizedRegisterRequest(t, challenge.Nonce, securityTestPassword)
	setRegisterCSeq(req, 2)
	tx = &captureServerTransaction{}
	h.Handle(req, tx)
	require.Equal(t, sip.StatusOK, tx.response.StatusCode)
	copied := req.Clone()
	copied.SetSource("198.51.100.99:5060")
	tx = &captureServerTransaction{}
	h.Handle(copied, tx)
	require.Equal(t, sip.StatusUnauthorized, tx.response.StatusCode)
	require.Equal(t, 1, calls)
}

func TestRegisterUDPEnumerationRequiresReturnedSourceChallenge(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		ban          bool
	}{{"returned to source", "198.51.100.23:5061", true}, {"copied to another source", "198.51.100.99:5060", false}} {
		t.Run(tc.name, func(t *testing.T) {
			runtime := gbsecurity.NewRuntime(gbsecurity.DefaultPolicy(), nil, nil, nil)
			h := NewRegisterHandler(securityTestCfg())
			h.SetSecurity(runtime)
			calls := 0
			h.handleRegister = func(context.Context, device.RegisterInfo, int) (bool, error) { calls++; return false, nil }
			initial := newSecurityRegisterRequest("5838", "5838")
			initial.Recipient.User = ""
			tx := &captureServerTransaction{}
			h.Handle(initial, tx)
			challenge, err := digest.ParseChallenge(tx.response.GetHeader("WWW-Authenticate").Value())
			require.NoError(t, err)
			for i := 0; i < 10; i++ {
				id := fmt.Sprint(5900 + i)
				req := newSecurityRegisterRequest(id, id)
				req.Recipient.User = ""
				req.SetSource(tc.source)
				setRegisterCSeq(req, i+2)
				req.AppendHeader(sip.NewHeader("Authorization", fmt.Sprintf(`Digest username="%s", realm="3402000000", nonce="%s", uri="sip:example", response="incorrect"`, id, challenge.Nonce)))
				tx = &captureServerTransaction{}
				h.Handle(req, tx)
				require.NotEqual(t, sip.StatusOK, tx.response.StatusCode)
			}
			require.Zero(t, calls, "reachability is not authentication")
			source, _ := splitHostPort(tc.source)
			require.Equal(t, tc.ban, runtime.Admission().IsBanned(source))
		})
	}
}

func TestRegisterInvalidIDWithCorrectPasswordIsRejected(t *testing.T) {
	h := NewRegisterHandler(securityTestCfg())
	calls := 0
	h.handleRegister = func(context.Context, device.RegisterInfo, int) (bool, error) { calls++; return false, nil }
	req := newSecurityRegisterRequest("5838", securityTestServerID)
	tx := &captureServerTransaction{}
	h.Handle(req, tx)
	challenge, err := digest.ParseChallenge(tx.response.GetHeader("WWW-Authenticate").Value())
	require.NoError(t, err)
	cred, err := digest.Digest(challenge, digest.Options{Method: "REGISTER", URI: req.Recipient.String(), Username: "5838", Password: securityTestPassword})
	require.NoError(t, err)
	req.AppendHeader(sip.NewHeader("Authorization", cred.String()))
	setRegisterCSeq(req, 2)
	tx = &captureServerTransaction{}
	h.Handle(req, tx)
	require.Equal(t, sip.StatusBadRequest, tx.response.StatusCode)
	require.Zero(t, calls)
}

func TestRegisterObserveModeStillRejectsInvalidCredentials(t *testing.T) {
	policy := gbsecurity.DefaultPolicy()
	policy.Mode = gbsecurity.ModeObserve
	runtime := gbsecurity.NewRuntime(policy, nil, nil, nil)
	h := NewRegisterHandler(securityTestCfg())
	h.SetSecurity(runtime)
	req := authorizedRegisterRequest(t, "not-a-valid-nonce", "wrong-password")
	tx := &captureServerTransaction{}
	h.Handle(req, tx)
	require.Equal(t, sip.StatusUnauthorized, tx.response.StatusCode)
	require.Empty(t, runtime.Bans())
	require.Len(t, runtime.Events(), 1)
	require.Equal(t, gbsecurity.ActionDrop, runtime.Events()[0].Action, "observe mode does not bypass REGISTER authentication")
}
