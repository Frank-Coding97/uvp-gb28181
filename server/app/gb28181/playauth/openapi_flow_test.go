package playauth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	openapiclient "uvplatform.cn/uvp-gb28181/app/openapi/client"
	"uvplatform.cn/uvp-gb28181/app/openapi/limit"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

func TestOpenAPIObserveFlowClosesBoundViewerWithoutChangingGrant(t *testing.T) {
	fixture := newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	service, token, reservation := issueOpenAPITestGrant(t, fixture)
	request := openAPIViewerRequest("flow-bound")
	_, err := service.BindViewer(context.Background(), token, request)
	require.NoError(t, err)

	quota := limit.NewQuota(fixture.db, func() time.Time { return fixture.now })
	occupied, err := quota.Occupied(context.Background(), testClientID)
	require.NoError(t, err)
	require.EqualValues(t, 1, occupied)

	var grantBefore models.PlayGrant
	require.NoError(t, fixture.db.First(&grantBefore, "grant_id = ?", reservation.GrantID).Error)
	fixture.now = fixture.now.Add(OpenAPIPlayTTL + time.Second)
	expectedNow := fixture.now.UTC().Truncate(time.Microsecond)
	require.NoError(t, service.ObserveFlow(context.Background(), openAPIFlowReport(request)))

	var viewer models.Viewer
	require.NoError(t, fixture.db.First(&viewer, "grant_id = ?", reservation.GrantID).Error)
	require.Equal(t, models.ViewerStateClosed, viewer.State)
	require.NotNil(t, viewer.LastSeenAt)
	require.Equal(t, expectedNow, viewer.LastSeenAt.UTC())
	require.Equal(t, expectedNow, viewer.UpdatedAt.UTC())
	require.Nil(t, viewer.RetryAt)
	require.Zero(t, viewer.Attempts)
	require.Empty(t, viewer.LastErrorClass)

	var grantAfter models.PlayGrant
	require.NoError(t, fixture.db.First(&grantAfter, "grant_id = ?", reservation.GrantID).Error)
	require.Equal(t, models.GrantStateBound, grantAfter.State)
	require.Equal(t, grantBefore.UpdatedAt.UTC(), grantAfter.UpdatedAt.UTC())
	require.Equal(t, grantBefore.Reason, grantAfter.Reason)

	occupied, err = quota.Occupied(context.Background(), testClientID)
	require.NoError(t, err)
	require.Zero(t, occupied)

	// A closed row is a durable terminal observation. Replaying the same flow
	// must not rewrite its tombstone or re-open the viewer.
	closedBefore := viewer
	require.NoError(t, service.ObserveFlow(context.Background(), openAPIFlowReport(request)))
	var repeated models.Viewer
	require.NoError(t, fixture.db.First(&repeated, "grant_id = ?", reservation.GrantID).Error)
	require.Equal(t, closedBefore.State, repeated.State)
	require.Equal(t, closedBefore.LastSeenAt.UTC(), repeated.LastSeenAt.UTC())
	require.Equal(t, closedBefore.UpdatedAt.UTC(), repeated.UpdatedAt.UTC())

	_, err = service.BindViewer(context.Background(), token, request)
	require.ErrorIs(t, err, ErrOpenAPIViewerDenied)
}

func TestOpenAPIObserveFlowClosesExpiredViewerAfterTTL(t *testing.T) {
	fixture := newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	service, token, reservation := issueOpenAPITestGrant(t, fixture)
	request := openAPIViewerRequest("flow-expired")
	_, err := service.BindViewer(context.Background(), token, request)
	require.NoError(t, err)

	require.NoError(t, fixture.db.Model(&models.PlayGrant{}).Where("grant_id = ?", reservation.GrantID).Update("state", models.GrantStateExpired).Error)
	var grantBefore models.PlayGrant
	require.NoError(t, fixture.db.First(&grantBefore, "grant_id = ?", reservation.GrantID).Error)
	fixture.now = fixture.now.Add(OpenAPIPlayTTL + time.Second)
	require.NoError(t, service.ObserveFlow(context.Background(), openAPIFlowReport(request)))

	var viewer models.Viewer
	require.NoError(t, fixture.db.First(&viewer, "grant_id = ?", reservation.GrantID).Error)
	require.Equal(t, models.ViewerStateClosed, viewer.State)
	var grantAfter models.PlayGrant
	require.NoError(t, fixture.db.First(&grantAfter, "grant_id = ?", reservation.GrantID).Error)
	require.Equal(t, models.GrantStateExpired, grantAfter.State)
	require.Equal(t, grantBefore.UpdatedAt.UTC(), grantAfter.UpdatedAt.UTC())
}

func TestOpenAPIObserveFlowRejectsMismatchesAndNeverGuessesViewer(t *testing.T) {
	fixture := newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	service, token, reservation := issueOpenAPITestGrant(t, fixture)
	request := openAPIViewerRequest("flow-mismatch")
	_, err := service.BindViewer(context.Background(), token, request)
	require.NoError(t, err)

	cases := []struct {
		name   string
		mutate func(*OpenAPIFlowReport)
		denied bool
	}{
		{name: "publisher", mutate: func(report *OpenAPIFlowReport) { report.Player = false }},
		{name: "unknown identifier", mutate: func(report *OpenAPIFlowReport) { report.Identifier = "unknown-flow" }},
		{name: "unknown node", mutate: func(report *OpenAPIFlowReport) { report.NodeUUID = "node-openapi-b" }},
		{name: "unknown boot", mutate: func(report *OpenAPIFlowReport) { report.BootNonce = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" }},
		{name: "protocol", mutate: func(report *OpenAPIFlowReport) { report.Protocol = "wss-flv" }, denied: true},
		{name: "schema", mutate: func(report *OpenAPIFlowReport) { report.Schema = "flv" }, denied: true},
		{name: "vhost", mutate: func(report *OpenAPIFlowReport) { report.VHost = "other-vhost" }, denied: true},
		{name: "app", mutate: func(report *OpenAPIFlowReport) { report.App = "other-app" }, denied: true},
		{name: "stream", mutate: func(report *OpenAPIFlowReport) { report.Stream = "other-stream" }, denied: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			report := openAPIFlowReport(request)
			test.mutate(&report)
			err := service.ObserveFlow(context.Background(), report)
			if test.denied {
				require.ErrorIs(t, err, ErrOpenAPIFlowDenied)
			} else {
				require.NoError(t, err)
			}
			var viewer models.Viewer
			require.NoError(t, fixture.db.First(&viewer, "grant_id = ?", reservation.GrantID).Error)
			require.Equal(t, models.ViewerStateActive, viewer.State)
		})
	}

	// Generation is not supplied by the Hook report; the durable grant/viewer
	// binding must still agree before a flow can close anything.
	newGeneration := uint64(10)
	require.NoError(t, fixture.db.Model(&models.PlayGrant{}).Where("grant_id = ?", reservation.GrantID).Update("media_generation", newGeneration).Error)
	err = service.ObserveFlow(context.Background(), openAPIFlowReport(request))
	require.ErrorIs(t, err, ErrOpenAPIFlowDenied)
	var viewer models.Viewer
	require.NoError(t, fixture.db.First(&viewer, "grant_id = ?", reservation.GrantID).Error)
	require.Equal(t, models.ViewerStateActive, viewer.State)

	// A report for a real-looking stream without a viewer row is a no-op; it
	// must not create a guessed viewer or mutate the grant.
	var grant models.PlayGrant
	require.NoError(t, fixture.db.First(&grant, "grant_id = ?", reservation.GrantID).Error)
	require.NoError(t, service.ObserveFlow(context.Background(), openAPIFlowReport(openAPIViewerRequest("no-row"))))
	var count int64
	require.NoError(t, fixture.db.Model(&models.Viewer{}).Where("grant_id = ?", reservation.GrantID).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestOpenAPIObserveFlowPreservesRevokedAndRevokePendingStates(t *testing.T) {
	// An abnormal revoked+active row must not be treated as a normal stream end.
	fixture := newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	service, token, reservation := issueOpenAPITestGrant(t, fixture)
	request := openAPIViewerRequest("flow-revoked-active")
	_, err := service.BindViewer(context.Background(), token, request)
	require.NoError(t, err)
	require.NoError(t, fixture.db.Model(&models.PlayGrant{}).Where("grant_id = ?", reservation.GrantID).Updates(map[string]any{
		"state": models.GrantStateRevoked, "reason": "client.revoked",
	}).Error)
	fixture.now = fixture.now.Add(time.Second)
	expectedNow := fixture.now.UTC().Truncate(time.Microsecond)
	require.NoError(t, service.ObserveFlow(context.Background(), openAPIFlowReport(request)))
	var viewer models.Viewer
	require.NoError(t, fixture.db.First(&viewer, "grant_id = ?", reservation.GrantID).Error)
	require.Equal(t, models.ViewerStateActive, viewer.State)
	require.Equal(t, expectedNow, viewer.LastSeenAt.UTC())

	// A normal revocation store marks the viewer for the worker. Flow reports
	// may refresh liveness, but may not consume the worker's retry lease.
	fixture = newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	service, token, reservation = issueOpenAPITestGrant(t, fixture)
	request = openAPIViewerRequest("flow-revoke-pending")
	_, err = service.BindViewer(context.Background(), token, request)
	require.NoError(t, err)
	recordClientDisableRevocation(t, fixture, reservation.GrantID)
	grantBefore := requireGrant(t, fixture.db, reservation.GrantID)
	viewerBefore := requireViewer(t, fixture.db, reservation.GrantID)
	require.Equal(t, models.GrantStateRevoked, grantBefore.State)
	require.Equal(t, models.ViewerStateRevokePending, viewerBefore.State)
	fixture.now = fixture.now.Add(time.Second)
	expectedNow = fixture.now.UTC().Truncate(time.Microsecond)
	require.NoError(t, service.ObserveFlow(context.Background(), openAPIFlowReport(request)))
	viewer = requireViewer(t, fixture.db, reservation.GrantID)
	grantAfter := requireGrant(t, fixture.db, reservation.GrantID)
	require.Equal(t, models.ViewerStateRevokePending, viewer.State)
	require.Equal(t, viewerBefore.RetryAt.UTC(), viewer.RetryAt.UTC())
	require.Equal(t, viewerBefore.Attempts, viewer.Attempts)
	require.Equal(t, viewerBefore.LastErrorClass, viewer.LastErrorClass)
	require.Equal(t, expectedNow, viewer.LastSeenAt.UTC())
	require.Equal(t, expectedNow, viewer.UpdatedAt.UTC())
	require.Equal(t, grantBefore.State, grantAfter.State)
	require.Equal(t, grantBefore.UpdatedAt.UTC(), grantAfter.UpdatedAt.UTC())
	require.Equal(t, grantBefore.Reason, grantAfter.Reason)

	quota := limit.NewQuota(fixture.db, func() time.Time { return fixture.now })
	occupied, err := quota.Occupied(context.Background(), testClientID)
	require.NoError(t, err)
	require.EqualValues(t, 1, occupied)
}

func TestOpenAPIObserveFlowAndRevocationComposeInEitherOrder(t *testing.T) {
	fixture := newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	service, token, reservation := issueOpenAPITestGrant(t, fixture)
	request := openAPIViewerRequest("flow-then-revoke")
	_, err := service.BindViewer(context.Background(), token, request)
	require.NoError(t, err)
	require.NoError(t, service.ObserveFlow(context.Background(), openAPIFlowReport(request)))
	recordClientDisableRevocation(t, fixture, reservation.GrantID)
	require.Equal(t, models.GrantStateRevoked, requireGrant(t, fixture.db, reservation.GrantID).State)
	require.Equal(t, models.ViewerStateClosed, requireViewer(t, fixture.db, reservation.GrantID).State)

	fixture = newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	service, token, reservation = issueOpenAPITestGrant(t, fixture)
	request = openAPIViewerRequest("revoke-then-flow")
	_, err = service.BindViewer(context.Background(), token, request)
	require.NoError(t, err)
	recordClientDisableRevocation(t, fixture, reservation.GrantID)
	require.NoError(t, service.ObserveFlow(context.Background(), openAPIFlowReport(request)))
	require.Equal(t, models.GrantStateRevoked, requireGrant(t, fixture.db, reservation.GrantID).State)
	require.Equal(t, models.ViewerStateRevokePending, requireViewer(t, fixture.db, reservation.GrantID).State)
	quota := limit.NewQuota(fixture.db, func() time.Time { return fixture.now })
	occupied, err := quota.Occupied(context.Background(), testClientID)
	require.NoError(t, err)
	require.EqualValues(t, 1, occupied)
}

func TestOpenAPIObserveFlowRollsBackOnViewerDatabaseFailure(t *testing.T) {
	fixture := newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	service, token, reservation := issueOpenAPITestGrant(t, fixture)
	request := openAPIViewerRequest("flow-db-failure")
	_, err := service.BindViewer(context.Background(), token, request)
	require.NoError(t, err)
	grantBefore := requireGrant(t, fixture.db, reservation.GrantID)
	viewerBefore := requireViewer(t, fixture.db, reservation.GrantID)
	require.NoError(t, fixture.db.Exec("CREATE TRIGGER reject_openapi_flow_viewer_update BEFORE UPDATE ON gb_openapi_viewer BEGIN SELECT RAISE(ABORT, 'fixture flow rejection'); END").Error)
	defer func() { _ = fixture.db.Exec("DROP TRIGGER reject_openapi_flow_viewer_update").Error }()

	fixture.now = fixture.now.Add(time.Second)
	err = service.ObserveFlow(context.Background(), openAPIFlowReport(request))
	require.ErrorIs(t, err, ErrOpenAPIFlowUnavailable)
	require.Equal(t, models.ViewerStateActive, requireViewer(t, fixture.db, reservation.GrantID).State)
	require.Equal(t, viewerBefore.UpdatedAt.UTC(), requireViewer(t, fixture.db, reservation.GrantID).UpdatedAt.UTC())
	require.Equal(t, grantBefore.State, requireGrant(t, fixture.db, reservation.GrantID).State)
	require.Equal(t, grantBefore.UpdatedAt.UTC(), requireGrant(t, fixture.db, reservation.GrantID).UpdatedAt.UTC())
	require.NotContains(t, err.Error(), "fixture flow rejection")
}

func openAPIFlowReport(request OpenAPIViewerBindRequest) OpenAPIFlowReport {
	return OpenAPIFlowReport{
		NodeUUID: request.NodeUUID, BootNonce: request.BootNonce, Identifier: request.Identifier,
		Protocol: request.Protocol, Schema: request.Schema, VHost: request.VHost,
		App: request.App, Stream: request.Stream, Player: true,
	}
}

func recordClientDisableRevocation(t *testing.T, fixture *openAPIGrantFixture, grantID string) {
	t.Helper()
	const disabledEpoch int64 = 3
	require.NoError(t, fixture.db.Model(&models.Client{}).Where("id = ?", testClientID).Updates(map[string]any{
		"status": models.StatusDisabled, "auth_epoch": disabledEpoch,
	}).Error)
	store := NewOpenAPIRevocationStore(fixture.db, func() time.Time { return fixture.now })
	err := fixture.db.Transaction(func(tx *gorm.DB) error {
		return store.RecordRevocationIntent(context.Background(), tx, openapiclient.RevocationIntent{
			ClientID: testClientID, ClientEpoch: disabledEpoch, Reason: "client.disabled", CreatedAt: fixture.now,
		})
	})
	require.NoError(t, err)
	require.Equal(t, models.GrantStateRevoked, requireGrant(t, fixture.db, grantID).State)
}
