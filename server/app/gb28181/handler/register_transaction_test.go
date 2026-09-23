package handler

import (
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
)

type transactionTestClock struct{ now time.Time }

func (clock *transactionTestClock) Now() time.Time { return clock.now }

func TestRegisterTransactionKeySeparatesCSeqAndAuthorizationFingerprint(t *testing.T) {
	first := newSecurityRegisterRequest(securityTestDeviceID, securityTestServerID)
	first.RemoveHeader("Via")
	second := first.Clone()
	second.AppendHeader(sip.NewHeader("Authorization", "Digest username=one"))
	third := first.Clone()
	third.RemoveHeader("CSeq")
	third.AppendHeader(sip.NewHeader("CSeq", "2 REGISTER"))

	require.NotEqual(t, registerTransactionKey(first, securityTestDeviceID), registerTransactionKey(second, securityTestDeviceID))
	require.NotEqual(t, registerTransactionKey(first, securityTestDeviceID), registerTransactionKey(third, securityTestDeviceID))
}

func TestRegisterTransactionLedgerEvictsOldCompletedEntries(t *testing.T) {
	clock := &transactionTestClock{now: time.Unix(100, 0)}
	ledger := newRegisterTransactionLedger(time.Minute, 2, clock.Now)
	for _, key := range []string{"one", "two"} {
		_, finish, execute := ledger.begin(key)
		require.True(t, execute)
		finish(registerTransactionResult{status: 200, reason: "OK"}, true)
		clock.now = clock.now.Add(time.Millisecond)
	}
	_, finish, execute := ledger.begin("three")
	require.True(t, execute)
	finish(registerTransactionResult{status: 200, reason: "OK"}, true)
	_, _, execute = ledger.begin("one")
	require.True(t, execute, "oldest completed transaction must be evicted")
}
