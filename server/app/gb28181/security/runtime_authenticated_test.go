package security

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestPersistentRuntimeRestoresOfflineAuthenticatedSourceProtection(t *testing.T) {
	ctx := context.Background()
	clock := &fakeClock{now: time.Now()}
	db := newSecurityStoreTestDB(t)
	store := NewGormStore(db)
	require.NoError(t, db.Exec(`INSERT INTO gb_device(device_id,transport,ip,register_time,register_expire_at) VALUES(?,?,?,?,?)`, "34020000001320000001", "UDP", "198.51.100.10", clock.Now().Add(-time.Hour), clock.Now().Add(-time.Minute)).Error)
	for restart := 0; restart < 2; restart++ {
		r, err := NewPersistentRuntime(ctx, store, clock, &fakeAgent{}, nil)
		require.NoError(t, err)
		_, trusted := r.TrustedEndpoint("34020000001320000001")
		require.False(t, trusted, "expired endpoint cannot authorize INVITE")
		for i := 0; i < 20; i++ {
			require.NoError(t, r.Record(Event{SourceIP: "198.51.100.10", Transport: "TCP", Method: "REGISTER", DeviceID: fmt.Sprint(i + 1000), Reason: ReasonRegisterIDInvalid, TransactionID: fmt.Sprint(i)}))
		}
		require.Empty(t, r.Bans())
		require.NoError(t, r.Close(ctx))
	}
}

func TestRuntimeCanDisableIPBansWhenHistoryUnavailable(t *testing.T) {
	r := NewRuntime(DefaultPolicy(), nil, &fakeAgent{}, nil)
	r.SetAutoBanEnabled(false)
	props := readProps()
	props.Transport = "TCP"
	for i := 0; i < 20; i++ {
		out, err := r.Admission().Filter(props, admissionSIPFrame("TCP", "INVITE", "scanner", fmt.Sprint(i), ""))
		require.NoError(t, err)
		require.Empty(t, out)
	}
	require.Empty(t, r.Bans())
	require.NotEmpty(t, r.Events())
}
