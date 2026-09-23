package media

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

type flowDuringKickControl struct {
	*stubRevocationControl
	afterKick func()
}

func (c flowDuringKickControl) KickSessionIfMatch(ctx context.Context, boot, id string) (zlm.ConditionalKickResult, error) {
	result, err := c.stubRevocationControl.KickSessionIfMatch(ctx, boot, id)
	c.afterKick()
	return result, err
}

func TestRevocationWorkerFinalFlowDoesNotInvalidateLease(t *testing.T) {
	f := newRevocationWorkerFixture(t, time.Second)
	a := f.seed(t, 1, models.ViewerStateRevokePending)
	require.NoError(t, f.db.AutoMigrate(&models.Client{}))
	require.NoError(t, f.db.Create(&models.Client{ID: 7, AK: "flow-race", Name: "flow race", OwnerDeptID: 10,
		Status: models.StatusDisabled, SecretCiphertext: []byte{1}, SecretIV: []byte{1}, SecretKeyID: "fixture"}).Error)
	signer, err := playauth.NewSigner(bytes.Repeat([]byte{7}, 32))
	require.NoError(t, err)
	flow, err := playauth.NewOpenAPIGrantService(f.db, signer, NewNodeAuthority(), f.now)
	require.NoError(t, err)
	f.setSnapshot([]string{a.Identifier}, []string{a.Identifier})
	f.factory.runtime.Control = flowDuringKickControl{stubRevocationControl: f.control, afterKick: func() {
		// The real flow service commits after the conditional kick has run,
		// before the worker can CAS its shutdown_scheduled marker.
		f.clock = f.clock.Add(time.Millisecond)
		require.NoError(t, flow.ObserveFlow(context.Background(), playauth.OpenAPIFlowReport{
			NodeUUID: a.NodeUUID, BootNonce: a.BootNonce, Identifier: a.Identifier, Protocol: "https-flv",
			Schema: a.Schema, VHost: a.VHost, App: a.App, Stream: a.Stream, Player: true,
		}))
		f.setSnapshot(nil, nil)
	}}
	worker := f.worker(1)
	first, err := worker.Tick(context.Background())
	require.NoError(t, err)
	require.Zero(t, first.Stale, "a liveness observation must not discard the worker's valid lease")
	require.Equal(t, 1, first.Pending)
	pending := f.loadViewer(t, a.ID)
	require.Equal(t, RevocationErrorShutdownScheduled, pending.LastErrorClass)
	require.NotNil(t, pending.LastSeenAt)
	f.clock = f.clock.Add(time.Second)
	second, err := worker.Tick(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, second.Closed, "fresh absence should close on the next tick, not wait for the six-second lease to expire")
	require.Equal(t, RevocationErrorKicked, f.loadViewer(t, a.ID).LastErrorClass)
}
