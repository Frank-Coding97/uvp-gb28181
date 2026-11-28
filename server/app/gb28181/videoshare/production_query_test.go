package videoshare

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/utils/gormhelper"
)

func TestFirstShareWithProductionNotFoundMask(t *testing.T) {
	s, channel, _ := newServiceFixture(t)
	require.NoError(t, s.db.Callback().Query().Before("gorm:query").Register("production:not_found_mask", gormhelper.MaskNotDataError))
	ctx := context.Background()
	state, err := s.Status(ctx, channel, 9)
	require.NoError(t, err)
	require.False(t, state.Enabled)
	require.Zero(t, state.Revision)
	_, err = s.AcquireLink(ctx, channel, 9, 1)
	require.ErrorIs(t, err, ErrNotFound)
	link, err := s.SetEnabled(ctx, channel, 9, true, 0)
	require.NoError(t, err)
	require.True(t, link.Enabled)
	_, err = s.Resolve(ctx, link.PublicID, link.Signature)
	require.NoError(t, err)
	_, err = s.Resolve(ctx, "missing", "missing")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestAuthorizedNewOwnerCanReplaceRetiredDeviceEpoch(t *testing.T) {
	s, channel, _ := newServiceFixture(t)
	ctx := context.Background()
	old, err := s.SetEnabled(ctx, channel, 9, true, 0)
	require.NoError(t, err)
	require.NoError(t, s.db.Exec("UPDATE gb_device SET access_epoch = 8 WHERE id = 1").Error)
	_, err = s.Resolve(ctx, old.PublicID, old.Signature)
	require.ErrorIs(t, err, ErrExpired)
	state, err := s.Status(ctx, channel, 10)
	require.NoError(t, err)
	require.False(t, state.Enabled)
	reopened, err := s.SetEnabled(ctx, channel, 10, true, state.Revision)
	require.NoError(t, err)
	require.NotEqual(t, old.Generation, reopened.Generation)
	_, err = s.Resolve(ctx, old.PublicID, old.Signature)
	require.Error(t, err)
	_, err = s.Resolve(ctx, reopened.PublicID, reopened.Signature)
	require.NoError(t, err)
}
