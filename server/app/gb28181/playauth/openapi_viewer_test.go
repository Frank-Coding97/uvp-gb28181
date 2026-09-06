package playauth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/openapi/limit"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

func TestOpenAPIViewerFirstBindAndSameConnectionRemainsIdempotentAfterTokenTTL(t *testing.T) {
	fixture := newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	service, token, reservation := issueOpenAPITestGrant(t, fixture)
	viewer, err := service.BindViewer(context.Background(), token, OpenAPIViewerBindRequest{
		NodeUUID: testNodeUUID, BootNonce: testBootNonce, Identifier: "session-a",
		Protocol: "https-flv", Schema: "rtmp", VHost: "__defaultVhost__", App: "rtp", Stream: testDeviceID + "_" + testChannelID, MediaGeneration: 9,
	})
	require.NoError(t, err)
	require.NotZero(t, viewer.ID)
	require.Equal(t, reservation.GrantID, viewer.GrantID)
	require.Equal(t, models.ViewerStateActive, viewer.State)
	require.Equal(t, testBootNonce, viewer.BootNonce)

	fixture.now = fixture.now.Add(OpenAPIPlayTTL + time.Second)
	repeated, err := service.BindViewer(context.Background(), token, OpenAPIViewerBindRequest{
		NodeUUID: testNodeUUID, BootNonce: testBootNonce, Identifier: "session-a",
		Protocol: "https-flv", Schema: "rtmp", VHost: "__defaultVhost__", App: "rtp", Stream: testDeviceID + "_" + testChannelID, MediaGeneration: 9,
	})
	require.NoError(t, err, "ordinary token TTL must not kick an existing bound viewer")
	require.Equal(t, viewer.ID, repeated.ID)

	recreated, err := NewOpenAPIGrantService(fixture.db, fixture.signer, fixture.authority, func() time.Time { return fixture.now })
	require.NoError(t, err)
	recovered, err := recreated.BindViewer(context.Background(), token, OpenAPIViewerBindRequest{
		NodeUUID: testNodeUUID, BootNonce: testBootNonce, Identifier: "session-a",
		Protocol: "https-flv", Schema: "rtmp", VHost: "__defaultVhost__", App: "rtp", Stream: testDeviceID + "_" + testChannelID, MediaGeneration: 9,
	})
	require.NoError(t, err, "a recreated service must recover the durable bound viewer")
	require.Equal(t, viewer.ID, recovered.ID)

	_, err = service.BindViewer(context.Background(), token, OpenAPIViewerBindRequest{
		NodeUUID: testNodeUUID, BootNonce: testBootNonce, Identifier: "session-b",
		Protocol: "https-flv", Schema: "rtmp", VHost: "__defaultVhost__", App: "rtp", Stream: testDeviceID + "_" + testChannelID, MediaGeneration: 9,
	})
	require.ErrorIs(t, err, ErrOpenAPIViewerDenied)
	var count int64
	require.NoError(t, fixture.db.Model(&models.Viewer{}).Where("grant_id = ?", reservation.GrantID).Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestOpenAPIViewerBoundSameMicrosecondDoesNotIssueDuplicateUpdate(t *testing.T) {
	fixture := newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	service, token, _ := issueOpenAPITestGrant(t, fixture)
	request := openAPIViewerRequest("same-microsecond")
	first, err := service.BindViewer(context.Background(), token, request)
	require.NoError(t, err)

	// The duplicate path must succeed even when MySQL reports changed rows as
	// zero. A trigger makes any actual UPDATE fail, proving this exact-time
	// retry takes the no-op path instead of issuing a redundant write.
	require.NoError(t, fixture.db.Exec("CREATE TRIGGER reject_openapi_viewer_update BEFORE UPDATE ON gb_openapi_viewer BEGIN SELECT RAISE(ABORT, 'fixture duplicate viewer update'); END").Error)
	repeated, err := service.BindViewer(context.Background(), token, request)
	require.NoError(t, err)
	require.Equal(t, first.ID, repeated.ID)
	require.Equal(t, first.LastSeenAt.UTC(), repeated.LastSeenAt.UTC())
	require.Equal(t, first.UpdatedAt.UTC(), repeated.UpdatedAt.UTC())
}

func TestOpenAPIViewerIssuedGrantNeedsFreshTokenAndMatchingMediaTuple(t *testing.T) {
	fixture := newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	service, token, _ := issueOpenAPITestGrant(t, fixture)
	fixture.now = fixture.now.Add(OpenAPIPlayTTL + time.Second)
	_, err := service.BindViewer(context.Background(), token, OpenAPIViewerBindRequest{
		NodeUUID: testNodeUUID, BootNonce: testBootNonce, Identifier: "late-session",
		Protocol: "https-flv", Schema: "rtmp", VHost: "__defaultVhost__", App: "rtp", Stream: testDeviceID + "_" + testChannelID, MediaGeneration: 9,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrOpenAPIViewerExpired)

	fixture = newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	service, token, _ = issueOpenAPITestGrant(t, fixture)
	_, err = service.BindViewer(context.Background(), token, OpenAPIViewerBindRequest{
		NodeUUID: testNodeUUID, BootNonce: testBootNonce, Identifier: "wrong-media",
		Protocol: "https-flv", Schema: "rtmp", VHost: "__defaultVhost__", App: "rtp", Stream: "other-stream", MediaGeneration: 9,
	})
	require.ErrorIs(t, err, ErrOpenAPIViewerDenied)
}

func TestOpenAPIViewerRequiresTrustedTransportProtocolMatch(t *testing.T) {
	fixture := newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	service, token, _ := issueOpenAPITestGrantWithProtocol(t, fixture, "https-flv")
	_, err := service.BindViewer(context.Background(), token, openAPIViewerRequestWithProtocol("empty-protocol", ""))
	require.ErrorIs(t, err, ErrOpenAPIViewerDenied)
	_, err = service.BindViewer(context.Background(), token, openAPIViewerRequestWithProtocol("wrong-transport", "wss-flv"))
	require.ErrorIs(t, err, ErrOpenAPIViewerDenied)
	otherNode := openAPIViewerRequest("wrong-node")
	otherNode.NodeUUID = "node-openapi-b"
	_, err = service.BindViewer(context.Background(), token, otherNode)
	require.ErrorIs(t, err, ErrOpenAPIViewerDenied)
	otherBoot := openAPIViewerRequest("wrong-boot")
	otherBoot.BootNonce = strings.Repeat("b", 32)
	_, err = service.BindViewer(context.Background(), token, otherBoot)
	require.ErrorIs(t, err, ErrOpenAPIViewerDenied)

	fixture = newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	service, token, _ = issueOpenAPITestGrantWithProtocol(t, fixture, "wss-flv")
	_, err = service.BindViewer(context.Background(), token, openAPIViewerRequestWithProtocol("https-against-wss", "https-flv"))
	require.ErrorIs(t, err, ErrOpenAPIViewerDenied)
	viewer, err := service.BindViewer(context.Background(), token, openAPIViewerRequestWithProtocol("wss-correct", "wss-flv"))
	require.NoError(t, err)
	require.Equal(t, models.ViewerStateActive, viewer.State)
}

func TestOpenAPIViewerDoesNotAuthorizeNonActiveExistingStates(t *testing.T) {
	fixture := newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	service, token, reservation := issueOpenAPITestGrant(t, fixture)
	_, err := service.BindViewer(context.Background(), token, openAPIViewerRequest("state-check"))
	require.NoError(t, err)
	for _, state := range []models.ViewerState{models.ViewerStatePending, models.ViewerStateRevokePending, models.ViewerStateClosed} {
		require.NoError(t, fixture.db.Model(&models.Viewer{}).Where("grant_id = ?", reservation.GrantID).Update("state", state).Error)
		_, err = service.BindViewer(context.Background(), token, openAPIViewerRequest("state-check"))
		require.ErrorIs(t, err, ErrOpenAPIViewerDenied, "state %s must not be treated as an active idempotent viewer", state)
	}
}

func TestOpenAPIViewerRejectsSubsecondGrantTimestampDrift(t *testing.T) {
	fixture := newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	service, token, reservation := issueOpenAPITestGrant(t, fixture)
	var grant models.PlayGrant
	require.NoError(t, fixture.db.First(&grant, "grant_id = ?", reservation.GrantID).Error)
	drifted := grant.IssuedAt.Add(time.Microsecond)
	require.NoError(t, fixture.db.Model(&models.PlayGrant{}).Where("grant_id = ?", reservation.GrantID).Update("issued_at", drifted).Error)
	_, err := service.BindViewer(context.Background(), token, openAPIViewerRequest("timestamp-drift"))
	require.ErrorIs(t, err, ErrOpenAPIViewerDenied)
}

func TestOpenAPIViewerConcurrentIdentifiersAllowAtMostOneBinding(t *testing.T) {
	fixture := newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	service, token, reservation := issueOpenAPITestGrant(t, fixture)
	requests := []OpenAPIViewerBindRequest{
		{NodeUUID: testNodeUUID, BootNonce: testBootNonce, Identifier: "session-a", Protocol: "https-flv", Schema: "rtmp", VHost: "__defaultVhost__", App: "rtp", Stream: testDeviceID + "_" + testChannelID, MediaGeneration: 9},
		{NodeUUID: testNodeUUID, BootNonce: testBootNonce, Identifier: "session-b", Protocol: "https-flv", Schema: "rtmp", VHost: "__defaultVhost__", App: "rtp", Stream: testDeviceID + "_" + testChannelID, MediaGeneration: 9},
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(requests))
	for _, request := range requests {
		wg.Add(1)
		go func(request OpenAPIViewerBindRequest) {
			defer wg.Done()
			_, bindErr := service.BindViewer(context.Background(), token, request)
			errs <- bindErr
		}(request)
	}
	wg.Wait()
	close(errs)
	success := 0
	for err := range errs {
		if err == nil {
			success++
			continue
		}
		require.Error(t, err)
	}
	require.LessOrEqual(t, success, 1)
	var count int64
	require.NoError(t, fixture.db.Model(&models.Viewer{}).Where("grant_id = ?", reservation.GrantID).Count(&count).Error)
	require.LessOrEqual(t, count, int64(1))
}

func TestOpenAPIViewerConcurrentIdentifiersWithSingleSQLiteConnectionHaveOneWinner(t *testing.T) {
	fixture := newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	fixture.sqlDB.SetMaxOpenConns(1)
	service, token, reservation := issueOpenAPITestGrant(t, fixture)
	requests := []OpenAPIViewerBindRequest{
		openAPIViewerRequest("single-connection-a"),
		openAPIViewerRequest("single-connection-b"),
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(requests))
	for _, request := range requests {
		wg.Add(1)
		go func(request OpenAPIViewerBindRequest) {
			defer wg.Done()
			_, bindErr := service.BindViewer(context.Background(), token, request)
			errs <- bindErr
		}(request)
	}
	wg.Wait()
	close(errs)
	success := 0
	denied := 0
	for err := range errs {
		switch {
		case err == nil:
			success++
		case errors.Is(err, ErrOpenAPIViewerDenied):
			denied++
		default:
			t.Fatalf("unexpected concurrent bind error: %v", err)
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 1, denied)
	var count int64
	require.NoError(t, fixture.db.Model(&models.Viewer{}).Where("grant_id = ?", reservation.GrantID).Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestOpenAPIViewerRejectsEpochOwnerAndRuntimeInvalidation(t *testing.T) {
	fixture := newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	service, token, _ := issueOpenAPITestGrant(t, fixture)
	require.NoError(t, fixture.db.Model(&models.Client{}).Where("id = ?", testClientID).Update("auth_epoch", 99).Error)
	_, err := service.BindViewer(context.Background(), token, openAPIViewerRequest("epoch-client"))
	require.ErrorIs(t, err, ErrOpenAPIViewerDenied)

	fixture = newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	service, token, _ = issueOpenAPITestGrant(t, fixture)
	require.NoError(t, fixture.db.Model(&models.ClientScope{}).Where("client_id = ? AND scope = ?", testClientID, limit.PlayLiveApplyScope).Updates(map[string]any{"enabled": false, "scope_epoch": 4}).Error)
	_, err = service.BindViewer(context.Background(), token, openAPIViewerRequest("epoch-scope"))
	require.ErrorIs(t, err, ErrOpenAPIViewerDenied)

	fixture = newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	service, token, _ = issueOpenAPITestGrant(t, fixture)
	require.NoError(t, fixture.db.Table("gb_device").Where("device_id = ?", testDeviceID).Update("access_epoch", 5).Error)
	_, err = service.BindViewer(context.Background(), token, openAPIViewerRequest("epoch-device"))
	require.ErrorIs(t, err, ErrOpenAPIViewerDenied)

	fixture = newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	service, token, _ = issueOpenAPITestGrant(t, fixture)
	require.NoError(t, fixture.db.Table("meta_node").Where("id = ?", 3).Updates(map[string]any{"runtime_identity_status": "unknown"}).Error)
	_, err = service.BindViewer(context.Background(), token, openAPIViewerRequest("runtime-unknown"))
	require.ErrorIs(t, err, ErrOpenAPIViewerUnavailable)
}

func TestOpenAPIViewerRegrantAfterEpochChangeDoesNotReviveOldGrant(t *testing.T) {
	fixture := newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	service, oldToken, oldReservation := issueOpenAPITestGrant(t, fixture)
	_, err := service.BindViewer(context.Background(), oldToken, openAPIViewerRequest("old-epoch"))
	require.NoError(t, err)

	require.NoError(t, fixture.db.Model(&models.Client{}).Where("id = ?", testClientID).Update("auth_epoch", 3).Error)
	require.NoError(t, fixture.db.Model(&models.ClientScope{}).Where("client_id = ? AND scope = ?", testClientID, limit.PlayLiveApplyScope).Updates(map[string]any{"scope_epoch": 4}).Error)
	require.NoError(t, fixture.db.Table("gb_device").Where("device_id = ?", testDeviceID).Update("access_epoch", 5).Error)
	_, err = service.BindViewer(context.Background(), oldToken, openAPIViewerRequest("old-epoch-reconnect"))
	require.ErrorIs(t, err, ErrOpenAPIViewerDenied)

	newService, newToken, newReservation := issueOpenAPITestGrant(t, fixture)
	require.NotEqual(t, oldReservation.GrantID, newReservation.GrantID)
	require.NoError(t, fixture.db.First(&models.PlayGrant{}, "grant_id = ?", oldReservation.GrantID).Error)
	newViewer, err := newService.BindViewer(context.Background(), newToken, openAPIViewerRequest("new-epoch"))
	require.NoError(t, err)
	require.Equal(t, newReservation.GrantID, newViewer.GrantID)
	require.NotEqual(t, oldReservation.GrantID, newViewer.GrantID)
}

func TestOpenAPIViewerInsertFailureRollsBackGrantBoundTransition(t *testing.T) {
	fixture := newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	service, token, reservation := issueOpenAPITestGrant(t, fixture)
	require.NoError(t, fixture.db.Exec("CREATE TRIGGER reject_openapi_viewer_insert BEFORE INSERT ON gb_openapi_viewer BEGIN SELECT RAISE(ABORT, 'fixture viewer rejection'); END").Error)
	_, err := service.BindViewer(context.Background(), token, openAPIViewerRequest("rollback"))
	require.Error(t, err)
	require.ErrorIs(t, err, ErrOpenAPIViewerUnavailable)
	var grant models.PlayGrant
	require.NoError(t, fixture.db.First(&grant, "grant_id = ?", reservation.GrantID).Error)
	require.Equal(t, models.GrantStateIssued, grant.State)
	var count int64
	require.NoError(t, fixture.db.Model(&models.Viewer{}).Where("grant_id = ?", reservation.GrantID).Count(&count).Error)
	require.Zero(t, count)
}

func TestOpenAPIViewerNormalizesWrappedSecurityErrors(t *testing.T) {
	fixture := newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	service, token, _ := issueOpenAPITestGrant(t, fixture)
	fixture.authority = wrappedGrantErrorAuthority{err: fmt.Errorf("viewer-fixture-secret: %w", ErrOpenAPIGrantUnavailable)}
	service, err := NewOpenAPIGrantService(fixture.db, fixture.signer, fixture.authority, func() time.Time { return fixture.now })
	require.NoError(t, err)
	_, err = service.BindViewer(context.Background(), token, openAPIViewerRequest("wrapped-error"))
	require.ErrorIs(t, err, ErrOpenAPIViewerUnavailable)
	require.Equal(t, ErrOpenAPIViewerUnavailable.Error(), err.Error())
	require.NotContains(t, err.Error(), "viewer-fixture-secret")
	require.Equal(t, ErrOpenAPIViewerUnavailable, normalizeOpenAPIViewerError(fmt.Errorf("context-secret: %w", context.Canceled)))
}

func TestOpenAPIViewerRejectsForeignOwnerAndOldBootIdentity(t *testing.T) {
	fixture := newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	service, token, _ := issueOpenAPITestGrant(t, fixture)
	require.NoError(t, fixture.db.Exec("INSERT INTO sys_department (id, status, deleted_at) VALUES (?, ?, NULL)", 18, 1).Error)
	require.NoError(t, fixture.db.Model(&models.Client{}).Where("id = ?", testClientID).Update("owner_dept_id", 18).Error)
	_, err := service.BindViewer(context.Background(), token, openAPIViewerRequest("foreign-owner"))
	require.ErrorIs(t, err, ErrOpenAPIViewerDenied)

	fixture = newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	service, token, _ = issueOpenAPITestGrant(t, fixture)
	require.NoError(t, fixture.db.Table("meta_node").Where("id = ?", 3).Update("current_boot_nonce", strings.Repeat("b", 32)).Error)
	_, err = service.BindViewer(context.Background(), token, openAPIViewerRequest("old-boot"))
	require.ErrorIs(t, err, ErrOpenAPIViewerUnavailable)
}

func issueOpenAPITestGrant(t *testing.T, fixture *openAPIGrantFixture) (*OpenAPIGrantService, string, limit.Reservation) {
	return issueOpenAPITestGrantWithProtocol(t, fixture, "https-flv")
}

func issueOpenAPITestGrantWithProtocol(t *testing.T, fixture *openAPIGrantFixture, protocol string) (*OpenAPIGrantService, string, limit.Reservation) {
	t.Helper()
	quota := limit.NewQuota(fixture.db, func() time.Time { return fixture.now })
	reservation, err := quota.ReservePending(context.Background(), limit.ReservationRequest{ClientID: testClientID, Scope: limit.PlayLiveApplyScope, DeviceID: testDeviceID, ChannelID: testChannelID})
	require.NoError(t, err)
	service, err := NewOpenAPIGrantService(fixture.db, fixture.signer, fixture.authority, func() time.Time { return fixture.now })
	require.NoError(t, err)
	issued, err := service.Issue(context.Background(), OpenAPIGrantIssueRequest{
		GrantID: reservation.GrantID, DeviceID: testDeviceID, ChannelID: testChannelID, NodeUUID: testNodeUUID, BootNonce: testBootNonce,
		Protocol: protocol, Schema: "rtmp", VHost: "__defaultVhost__", App: "rtp", Stream: testDeviceID + "_" + testChannelID, MediaGeneration: 9,
	})
	require.NoError(t, err)
	return service, issued.Token, reservation
}

func openAPIViewerRequest(identifier string) OpenAPIViewerBindRequest {
	return openAPIViewerRequestWithProtocol(identifier, "https-flv")
}

func openAPIViewerRequestWithProtocol(identifier, protocol string) OpenAPIViewerBindRequest {
	return OpenAPIViewerBindRequest{
		NodeUUID: testNodeUUID, BootNonce: testBootNonce, Identifier: identifier,
		Protocol: protocol, Schema: "rtmp", VHost: "__defaultVhost__", App: "rtp", Stream: testDeviceID + "_" + testChannelID, MediaGeneration: 9,
	}
}
