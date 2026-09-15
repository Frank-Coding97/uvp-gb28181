package handler

import (
	"context"
	"strconv"
	"testing"

	"github.com/emiago/sipgo/sip"
	"github.com/icholy/digest"
	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/device"
)

func TestRegisterQoplessDigestReplayCannotChangeNonceCountAndUnregister(t *testing.T) {
	handler, registerCalls, unregisterCalls := newRealNonceRegisterHandler()
	nonce, err := handler.security.(sourceBoundRegisterSecurity).IssueNonceForSource("198.51.100.23")
	require.NoError(t, err)

	first := authorizedRegisterRequest(t, nonce, securityTestPassword)
	firstTx := &captureServerTransaction{}
	handler.Handle(first, firstTx)
	require.Equal(t, sip.StatusOK, firstTx.response.StatusCode)

	attack := first.Clone()
	attack.SetSource("203.0.113.99:5099")
	setRegisterCSeq(attack, 2)
	attack.RemoveHeader("Expires")
	attack.AppendHeader(sip.NewHeader("Expires", "0"))
	copiedAuth := first.GetHeader("Authorization").Value()
	attack.RemoveHeader("Authorization")
	attack.AppendHeader(sip.NewHeader("Authorization", copiedAuth+", nc=00000001"))
	attackTx := &captureServerTransaction{}
	handler.Handle(attack, attackTx)

	t.Logf("original_status=%d replay_status=%d register_calls=%d unregister_calls=%d", firstTx.response.StatusCode, attackTx.response.StatusCode, *registerCalls, *unregisterCalls)
	require.Equal(t, sip.StatusUnauthorized, attackTx.response.StatusCode, "copied qopless Digest with unsigned nc must not authorize a new transaction")
	require.Equal(t, 1, *registerCalls)
	require.Zero(t, *unregisterCalls)
}

func TestRegisterRealNonceManagerAllowsMultipleSameTransactionRetransmissions(t *testing.T) {
	handler, registerCalls, unregisterCalls := newRealNonceRegisterHandler()
	nonce, err := handler.security.(sourceBoundRegisterSecurity).IssueNonceForSource("198.51.100.23")
	require.NoError(t, err)
	request := authorizedRegisterRequest(t, nonce, securityTestPassword)

	for i := 0; i < 3; i++ {
		tx := &captureServerTransaction{}
		handler.Handle(request, tx)
		require.Equal(t, sip.StatusOK, tx.response.StatusCode)
	}

	require.Equal(t, 1, *registerCalls)
	require.Zero(t, *unregisterCalls)
}

func TestRegisterQoplessDigestReplayWithDifferentCSeqIsRejected(t *testing.T) {
	handler, registerCalls, unregisterCalls := newRealNonceRegisterHandler()
	nonce, err := handler.security.(sourceBoundRegisterSecurity).IssueNonceForSource("198.51.100.23")
	require.NoError(t, err)

	first := authorizedRegisterRequest(t, nonce, securityTestPassword)
	firstTx := &captureServerTransaction{}
	handler.Handle(first, firstTx)
	require.Equal(t, sip.StatusOK, firstTx.response.StatusCode)

	replay := first.Clone()
	setRegisterCSeq(replay, 2)
	replayTx := &captureServerTransaction{}
	handler.Handle(replay, replayTx)

	require.Equal(t, sip.StatusUnauthorized, replayTx.response.StatusCode)
	require.Equal(t, 1, *registerCalls)
	require.Zero(t, *unregisterCalls)
}

func TestRegisterQopAuthBindsNonceCountAndAllowsNewNonce(t *testing.T) {
	handler, registerCalls, unregisterCalls := newRealNonceRegisterHandler()
	nonce, err := handler.security.(sourceBoundRegisterSecurity).IssueNonceForSource("198.51.100.23")
	require.NoError(t, err)

	first := authorizedQopAuthRegisterRequest(t, nonce, securityTestPassword, 1)
	firstTx := &captureServerTransaction{}
	handler.Handle(first, firstTx)
	require.Equal(t, sip.StatusOK, firstTx.response.StatusCode)

	for i := 0; i < 2; i++ {
		tx := &captureServerTransaction{}
		handler.Handle(first, tx)
		require.Equal(t, sip.StatusOK, tx.response.StatusCode)
	}

	sameCount := first.Clone()
	setRegisterCSeq(sameCount, 2)
	sameCountTx := &captureServerTransaction{}
	handler.Handle(sameCount, sameCountTx)
	require.Equal(t, sip.StatusUnauthorized, sameCountTx.response.StatusCode)

	changedCount := authorizedQopAuthRegisterRequest(t, nonce, securityTestPassword, 2)
	setRegisterCSeq(changedCount, 2)
	changedCountTx := &captureServerTransaction{}
	handler.Handle(changedCount, changedCountTx)
	require.Equal(t, sip.StatusOK, changedCountTx.response.StatusCode)

	newNonce, err := handler.security.(sourceBoundRegisterSecurity).IssueNonceForSource("198.51.100.23")
	require.NoError(t, err)
	newNonceRequest := authorizedQopAuthRegisterRequest(t, newNonce, securityTestPassword, 1)
	setRegisterCSeq(newNonceRequest, 3)
	newNonceTx := &captureServerTransaction{}
	handler.Handle(newNonceRequest, newNonceTx)
	require.Equal(t, sip.StatusOK, newNonceTx.response.StatusCode)

	require.Equal(t, 3, *registerCalls)
	require.Zero(t, *unregisterCalls)
}

func newRealNonceRegisterHandler() (*RegisterHandler, *int, *int) {
	handler := NewRegisterHandler(securityTestCfg())
	registerCalls := 0
	unregisterCalls := 0
	handler.handleRegister = func(context.Context, device.RegisterInfo, int) (bool, error) {
		registerCalls++
		return false, nil
	}
	handler.handleUnregister = func(context.Context, string) error {
		unregisterCalls++
		return nil
	}
	return handler, &registerCalls, &unregisterCalls
}

func authorizedQopAuthRegisterRequest(t *testing.T, nonce, password string, count int) *sip.Request {
	t.Helper()
	req := newSecurityRegisterRequest(securityTestDeviceID, securityTestServerID)
	req.AppendHeader(sip.NewHeader("Expires", "3600"))
	credentials, err := digest.Digest(&digest.Challenge{
		Realm: securityTestCfg().SIP.Domain, Nonce: nonce, Algorithm: "MD5", QOP: []string{"auth"},
	}, digest.Options{
		Method: "REGISTER", URI: req.Recipient.String(), Username: securityTestDeviceID, Password: password,
		Count: count, Cnonce: "register-test-cnonce",
	})
	require.NoError(t, err)
	req.AppendHeader(sip.NewHeader("Authorization", credentials.String()))
	return req
}

func setRegisterCSeq(req *sip.Request, sequence int) {
	req.RemoveHeader("CSeq")
	req.AppendHeader(sip.NewHeader("CSeq", strconv.Itoa(sequence)+" REGISTER"))
}
