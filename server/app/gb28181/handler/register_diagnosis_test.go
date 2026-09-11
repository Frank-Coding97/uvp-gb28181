package handler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/icholy/digest"
	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/device"
	gbsecurity "uvplatform.cn/uvp-gb28181/app/gb28181/security"
	"uvplatform.cn/uvp-gb28181/app/gb28181/trace/diagnosis"
)

type captureDiagnosticSink struct {
	mu     sync.Mutex
	events []diagnosis.Event
	err    error
}

func (sink *captureDiagnosticSink) Emit(_ context.Context, event diagnosis.Event) error {
	sink.mu.Lock()
	sink.events = append(sink.events, event)
	sink.mu.Unlock()
	return sink.err
}

func (sink *captureDiagnosticSink) snapshot() []diagnosis.Event {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	return append([]diagnosis.Event(nil), sink.events...)
}

func TestRegisterDiagnosisChallengeIsNotFailureAndTimeoutIs(t *testing.T) {
	now := time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC)
	security := &fakeRegisterSecurity{nonces: []string{"challenge-nonce"}}
	sink := &captureDiagnosticSink{}
	handler := NewRegisterHandler(securityTestCfg())
	handler.SetSecurity(security)
	handler.SetDiagnosticSink(sink)
	handler.now = func() time.Time { return now }
	tx := &captureServerTransaction{}
	req := newSecurityRegisterRequest(securityTestDeviceID, securityTestServerID)

	handler.Handle(req, tx)

	require.Equal(t, sip.StatusUnauthorized, tx.response.StatusCode)
	require.Empty(t, sink.snapshot())
	key := diagnosis.RegisterCorrelationKey(securityTestDeviceID, "security-register-test", 1, "challenge-nonce")
	require.True(t, handler.attempts.expire(key))
	events := sink.snapshot()
	require.Len(t, events, 1)
	require.Equal(t, diagnosis.CodeRegisterTimeout, events[0].Code)
	require.Equal(t, key, events[0].CorrelationKey)
}

func TestRegisterDiagnosisMapsExplicitFailures(t *testing.T) {
	tests := []struct {
		name string
		code diagnosis.Code
		run  func(*RegisterHandler, *fakeRegisterSecurity) *sip.Response
	}{
		{
			name: "server id", code: diagnosis.CodeServerIDMismatch,
			run: func(handler *RegisterHandler, _ *fakeRegisterSecurity) *sip.Response {
				tx := &captureServerTransaction{}
				handler.Handle(newSecurityRegisterRequest(securityTestDeviceID, "34020000002000000099"), tx)
				return tx.response
			},
		},
		{
			name: "digest", code: diagnosis.CodeDigestFailure,
			run: func(handler *RegisterHandler, security *fakeRegisterSecurity) *sip.Response {
				security.nonces = []string{"fresh"}
				req := newSecurityRegisterRequest(securityTestDeviceID, securityTestServerID)
				req.AppendHeader(sip.NewHeader("Authorization", "broken"))
				tx := &captureServerTransaction{}
				handler.Handle(req, tx)
				return tx.response
			},
		},
		{
			name: "nonce expired", code: diagnosis.CodeNonceExpired,
			run: func(handler *RegisterHandler, security *fakeRegisterSecurity) *sip.Response {
				security.nonces = []string{"fresh"}
				security.validateErr = errors.New("unused")
				security.validateErr = diagnosisNonceError(diagnosis.CodeNonceExpired)
				req := authorizedRegisterRequest(t, "presented", securityTestPassword)
				tx := &captureServerTransaction{}
				handler.Handle(req, tx)
				return tx.response
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			security := &fakeRegisterSecurity{}
			sink := &captureDiagnosticSink{}
			handler := NewRegisterHandler(securityTestCfg())
			handler.SetSecurity(security)
			handler.SetDiagnosticSink(sink)

			response := tt.run(handler, security)

			require.NotNil(t, response)
			events := sink.snapshot()
			require.Len(t, events, 1)
			require.Equal(t, tt.code, events[0].Code)
			require.Equal(t, diagnosis.CategoryRegisterFailure, events[0].Category)
		})
	}
}

func TestRegisterDiagnosisMapsDeviceFailureAndSuccessClosesChallenge(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
		code diagnosis.Code
	}{
		{"preallocation", device.ErrDeviceNotPreallocated, diagnosis.CodeDeviceNotPreallocated},
		{"internal", errors.New("database failed"), diagnosis.CodeInternalError},
	} {
		t.Run(tt.name, func(t *testing.T) {
			security := &fakeRegisterSecurity{nonces: []string{"fresh"}}
			sink := &captureDiagnosticSink{}
			handler := NewRegisterHandler(securityTestCfg())
			handler.SetSecurity(security)
			handler.SetDiagnosticSink(sink)
			handler.handleRegister = func(context.Context, device.RegisterInfo, int) (bool, error) { return false, tt.err }
			tx := &captureServerTransaction{}
			handler.Handle(authorizedRegisterRequest(t, "presented", securityTestPassword), tx)
			require.Len(t, sink.snapshot(), 1)
			require.Equal(t, tt.code, sink.snapshot()[0].Code)
		})
	}

	security := &fakeRegisterSecurity{nonces: []string{"challenge-nonce"}}
	sink := &captureDiagnosticSink{}
	handler := NewRegisterHandler(securityTestCfg())
	handler.SetSecurity(security)
	handler.SetDiagnosticSink(sink)
	handler.handleRegister = func(context.Context, device.RegisterInfo, int) (bool, error) { return false, nil }
	challengeTx := &captureServerTransaction{}
	handler.Handle(newSecurityRegisterRequest(securityTestDeviceID, securityTestServerID), challengeTx)
	security.validateErr = nil
	authorized := authorizedRegisterRequest(t, "challenge-nonce", securityTestPassword)
	authorized.RemoveHeader("Call-ID")
	authorized.AppendHeader(sip.NewHeader("Call-ID", "security-register-test"))
	authorized.RemoveHeader("CSeq")
	authorized.AppendHeader(sip.NewHeader("CSeq", "2 REGISTER"))
	successTx := &captureServerTransaction{}
	handler.Handle(authorized, successTx)
	require.Equal(t, sip.StatusOK, successTx.response.StatusCode)
	key := diagnosis.RegisterCorrelationKey(securityTestDeviceID, "security-register-test", 2, "challenge-nonce")
	require.False(t, handler.attempts.expire(key))
	require.Empty(t, sink.snapshot())
}

func TestRegisterDiagnosisSinkFailureDoesNotChangeResponse(t *testing.T) {
	handler := NewRegisterHandler(securityTestCfg())
	handler.SetSecurity(&fakeRegisterSecurity{})
	handler.SetDiagnosticSink(&captureDiagnosticSink{err: diagnosis.ErrQueueFull})
	tx := &captureServerTransaction{}
	handler.Handle(newSecurityRegisterRequest(securityTestDeviceID, "wrong"), tx)
	require.Equal(t, sip.StatusForbidden, tx.response.StatusCode)
}

func TestRegisterDiagnosisTimeoutSuccessRaceResolves(t *testing.T) {
	now := time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC)
	sink := &captureDiagnosticSink{}
	tracker := newRegisterAttemptTracker(sink, func() time.Time { return now })
	key := diagnosis.RegisterCorrelationKey(securityTestDeviceID, "race-call", 1, "race-nonce")
	tracker.start(diagnosis.Event{
		CorrelationKey: key, State: diagnosis.StateActive,
		Category: diagnosis.CategoryRegisterFailure, Code: diagnosis.CodeRegisterTimeout,
		Stage: diagnosis.StageRegister, Source: diagnosis.SourceRuntime,
		DeviceID: securityTestDeviceID, CallID: "race-call", CSeq: 1, Method: "REGISTER",
	})

	require.True(t, tracker.expire(key))
	tracker.finish(key, now.Add(defaultRegisterObservationWindow))

	events := sink.snapshot()
	require.Len(t, events, 2)
	require.Equal(t, diagnosis.StateActive, events[0].State)
	require.Equal(t, diagnosis.StateResolved, events[1].State)
	require.Equal(t, events[0].CorrelationKey, events[1].CorrelationKey)
}

func TestRegisterDiagnosisNonceCodeMapping(t *testing.T) {
	for _, tt := range []struct {
		err  error
		code diagnosis.Code
	}{
		{gbsecurity.ErrNonceInvalid, diagnosis.CodeNonceInvalid},
		{gbsecurity.ErrNonceExpired, diagnosis.CodeNonceExpired},
		{gbsecurity.ErrNonceReplay, diagnosis.CodeNonceReplay},
		{gbsecurity.ErrNonceStale, diagnosis.CodeNonceStale},
	} {
		require.Equal(t, tt.code, diagnosisCodeForNonceError(tt.err))
	}
}

func TestRegisterDiagnosisMissingDeviceIsInvalidRequest(t *testing.T) {
	handler := NewRegisterHandler(securityTestCfg())
	sink := &captureDiagnosticSink{}
	handler.SetDiagnosticSink(sink)
	req := newSecurityRegisterRequest("", securityTestServerID)
	tx := &captureServerTransaction{}

	handler.Handle(req, tx)

	require.Equal(t, sip.StatusBadRequest, tx.response.StatusCode)
	require.Len(t, sink.snapshot(), 1)
	require.Equal(t, diagnosis.CodeInvalidRequest, sink.snapshot()[0].Code)
}

func authorizedRegisterRequest(t *testing.T, nonce, password string) *sip.Request {
	t.Helper()
	req := newSecurityRegisterRequest(securityTestDeviceID, securityTestServerID)
	req.AppendHeader(sip.NewHeader("Expires", "3600"))
	credentials, err := digest.Digest(&digest.Challenge{
		Realm: securityTestCfg().SIP.Domain, Nonce: nonce, Algorithm: "MD5",
	}, digest.Options{
		Method: "REGISTER", URI: req.Recipient.String(), Username: securityTestDeviceID, Password: password,
	})
	require.NoError(t, err)
	req.AppendHeader(sip.NewHeader("Authorization", credentials.String()))
	return req
}

func diagnosisNonceError(code diagnosis.Code) error {
	switch code {
	case diagnosis.CodeNonceExpired:
		return gbsecurity.ErrNonceExpired
	default:
		return gbsecurity.ErrNonceInvalid
	}
}
