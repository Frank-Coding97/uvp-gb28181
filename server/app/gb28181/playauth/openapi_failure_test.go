package playauth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/openapi/limit"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

func TestOpenAPIFailUnboundGrantIsExactAndDoesNotReviveTombstone(t *testing.T) {
	f := newOpenAPIGrantFixture(t)
	defer f.close(t)
	s, token, issued := issueOpenAPITestGrant(t, f)
	quota := limit.NewQuota(f.db, func() time.Time { return f.now })
	pending, err := quota.ReservePending(context.Background(), limit.ReservationRequest{ClientID: testClientID, Scope: limit.PlayLiveApplyScope, DeviceID: testDeviceID, ChannelID: testChannelID})
	require.NoError(t, err)
	require.NoError(t, s.FailUnboundGrant(context.Background(), testClientID, issued.GrantID))
	failed := requireGrantState(t, f.db, issued.GrantID, models.GrantStateFailed)
	require.Equal(t, "apply_failed", failed.Reason)
	requireGrantState(t, f.db, pending.GrantID, models.GrantStatePending)
	_, err = s.BindViewer(context.Background(), token, openAPIViewerRequest("failed-apply"))
	require.ErrorIs(t, err, ErrOpenAPIViewerDenied)
	f.now = f.now.Add(time.Minute)
	require.NoError(t, s.FailUnboundGrant(context.Background(), testClientID, issued.GrantID))
	require.Equal(t, failed.UpdatedAt, requireGrantState(t, f.db, issued.GrantID, models.GrantStateFailed).UpdatedAt)
	require.NoError(t, s.FailUnboundGrant(context.Background(), testClientID, pending.GrantID))
	occupied, err := quota.Occupied(context.Background(), testClientID)
	require.NoError(t, err)
	require.Zero(t, occupied)
}

func TestOpenAPIFailUnboundGrantRejectsOtherClientAndPreservesBoundViewer(t *testing.T) {
	f := newOpenAPIGrantFixture(t)
	defer f.close(t)
	s, token, issued := issueOpenAPITestGrant(t, f)
	require.Error(t, s.FailUnboundGrant(context.Background(), testClientID+1, issued.GrantID))
	requireGrantState(t, f.db, issued.GrantID, models.GrantStateIssued)
	_, err := s.BindViewer(context.Background(), token, openAPIViewerRequest("existing-viewer"))
	require.NoError(t, err)
	require.Error(t, s.FailUnboundGrant(context.Background(), testClientID, issued.GrantID))
	requireGrantState(t, f.db, issued.GrantID, models.GrantStateBound)
	require.Equal(t, models.ViewerStateActive, requireViewer(t, f.db, issued.GrantID).State)
}

func TestOpenAPIFailUnboundGrantPreservesRevokedStateAndRejectsCanceledContext(t *testing.T) {
	f := newOpenAPIGrantFixture(t)
	defer f.close(t)
	s, _, issued := issueOpenAPITestGrant(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.Error(t, s.FailUnboundGrant(ctx, testClientID, issued.GrantID))
	requireGrantState(t, f.db, issued.GrantID, models.GrantStateIssued)
	require.NoError(t, f.db.Model(&models.PlayGrant{}).Where("grant_id = ?", issued.GrantID).Updates(map[string]any{"state": models.GrantStateRevoked, "reason": "client.disabled"}).Error)
	revoked := requireGrantState(t, f.db, issued.GrantID, models.GrantStateRevoked)
	f.now = f.now.Add(time.Minute)
	require.NoError(t, s.FailUnboundGrant(context.Background(), testClientID, issued.GrantID))
	current := requireGrantState(t, f.db, issued.GrantID, models.GrantStateRevoked)
	require.Equal(t, revoked.UpdatedAt, current.UpdatedAt)
	require.Equal(t, revoked.Reason, current.Reason)
}

func TestOpenAPIFailUnboundGrantDatabaseFailureRollsBackAndDoesNotLeakError(t *testing.T) {
	f := newOpenAPIGrantFixture(t)
	defer f.close(t)
	s, _, issued := issueOpenAPITestGrant(t, f)
	require.NoError(t, f.db.Exec("CREATE TRIGGER reject_apply_failure BEFORE UPDATE ON gb_openapi_play_grant BEGIN SELECT RAISE(ABORT, 'fixture-private-driver-value'); END").Error)
	err := s.FailUnboundGrant(context.Background(), testClientID, issued.GrantID)
	require.ErrorIs(t, err, ErrOpenAPIGrantUnavailable)
	require.NotContains(t, err.Error(), "fixture-private-driver-value")
	requireGrantState(t, f.db, issued.GrantID, models.GrantStateIssued)
}
