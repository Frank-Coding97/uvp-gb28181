package playback

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

type parentTestSIP struct {
	local bool
	err   error
}

func (c *parentTestSIP) Invite(context.Context) (DialogInfo, error)                    { return DialogInfo{}, nil }
func (c *parentTestSIP) SendINFO(context.Context, playauth.DeviceSIPINFOCommand) error { return nil }
func (c *parentTestSIP) Close(context.Context) (bool, error)                           { return c.local, c.err }

type parentTestRTP struct {
	local bool
	err   error
}

func (c *parentTestRTP) Open(context.Context) (RTPAllocation, error) { return RTPAllocation{}, nil }
func (c *parentTestRTP) Close(context.Context) (bool, error)         { return c.local, c.err }
func (c *parentTestRTP) Unbind(context.Context) error                { return nil }

func TestPlaybackIntentParentRequiresBothChildrenAndInitializationJoin(t *testing.T) {
	db, barrier, req := playbackEpochFixture(t)
	require.NoError(t, db.Exec("CREATE TABLE gb_channel (id INTEGER PRIMARY KEY, device_id TEXT, channel_id TEXT, deleted_at DATETIME)").Error)
	require.NoError(t, db.Exec("INSERT INTO gb_channel VALUES(2,?,?,NULL)", req.DeviceID, req.SIPChannelID).Error)
	o, err := newPlaybackIntentOwner(context.Background(), playauth.NewDeviceOperationIntentStore(db), barrier, req)
	require.NoError(t, err)
	require.NoError(t, o.begin(context.Background()))
	sip := &parentTestSIP{local: true, err: errors.New("remote unknown")}
	rtp := &parentTestRTP{local: false, err: errors.New("still running")}
	o.sip, o.rtp = sip, rtp
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, o.Teardown(ctx), context.DeadlineExceeded)
	require.ErrorIs(t, o.lease.Context().Err(), context.Canceled, "cancelled is not released")
	o.finishInitialization()
	require.Error(t, o.Teardown(context.Background()))
	require.Error(t, o.CloseRTP(context.Background()))
	wait, stop := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer stop()
	require.ErrorIs(t, barrier.WaitBefore(wait, 1, 2), context.DeadlineExceeded)
	rtp.local = true
	require.Error(t, o.CloseRTP(context.Background()), "remote unknown remains visible")
	require.NoError(t, barrier.WaitBefore(context.Background(), 1, 2), "only the actual parent releases the lane")
	var intent playauth.DeviceOperationIntent
	require.NoError(t, db.First(&intent).Error)
	require.Equal(t, playauth.IntentDispatched, intent.State)
}
