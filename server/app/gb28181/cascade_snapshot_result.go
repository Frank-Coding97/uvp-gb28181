package gb28181

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/control"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

func newCascadeSnapshotResultHandler(store *repository.GormRepository, clients *cascadePlatformClientFactory) func(*sip.Request, sip.ServerTransaction) bool {
	return func(req *sip.Request, tx sip.ServerTransaction) bool {
		head, err := manscdp.ParseHead(req.Body())
		if err != nil || head.CmdType != manscdp.CmdUploadSnapShotFinished {
			return false
		}
		reject := func(code int, reason string) bool {
			_ = tx.Respond(sip.NewResponseFromRequest(req, code, reason, nil))
			return true
		}
		notification, err := manscdp.ParseUploadSnapShotFinished(req.Body())
		if err != nil {
			return reject(400, "Invalid snapshot completion")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		sessions, err := findCascadeSnapshotSessions(ctx, app.DB(), notification.SessionID)
		if err != nil {
			return reject(503, "Snapshot result routing unavailable")
		}
		if len(sessions) == 0 {
			return false
		}
		if len(sessions) != 1 {
			return reject(403, "Ambiguous snapshot session")
		}
		session := sessions[0]
		loaded, err := control.NewGormTargetLoader(app.DB()).LoadAuthorized(ctx, session.SourceDeviceID, session.SourceChannelID)
		if err != nil || session.SourceDeviceEpoch <= 0 || loaded.DeviceEpoch != session.SourceDeviceEpoch || loaded.Profile.Version != protocol.Version2022 ||
			!snapshotCompletionIdentityMatches(req, notification.DeviceID, loaded.DeviceCode, loaded.ChannelCode) {
			return reject(403, "Snapshot source identity mismatch")
		}
		if session.State == model.CascadeSnapshotSessionCompleted {
			_ = tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
			return true
		}
		if session.State == model.CascadeSnapshotSessionForwarding {
			reclaimed, err := reclaimStaleCascadeSnapshotForwarding(ctx, app.DB(), session, time.Now(), 15*time.Second)
			if err != nil || !reclaimed {
				return reject(503, "Snapshot result forwarding is in progress")
			}
			session.State = model.CascadeSnapshotSessionPending
		}
		if session.State != model.CascadeSnapshotSessionPending || !time.Now().Before(session.ExpiresAt) {
			return reject(403, "Snapshot session is not active")
		}
		platform, err := store.FindPlatform(ctx, session.PlatformID)
		if err != nil || !cascadeSnapshotSessionAuthorized(session, *platform, session.SourceDeviceID, session.SourceChannelID, time.Now()) || cascadeWireProfile(*platform).Version != protocol.Version2022 {
			return reject(403, "Snapshot session authorization changed")
		}
		projection, err := store.ProjectionSnapshot(ctx, platform.ID)
		if err != nil || projection == nil || projection.Revision != session.ProjectionRevision {
			return reject(403, "Snapshot publication changed")
		}
		publication, err := buildCascadeCatalogSnapshot(ctx, store, &projection.Platform)
		if err != nil || publication.Revision != session.ProjectionRevision {
			return reject(503, "Snapshot publication unavailable")
		}
		target, ok := resolveCascadeDeviceStatusTarget(*projection, publication, session.PublishedChannelID)
		if !ok || target.IsPlatform || target.SourceDeviceID != session.SourceDeviceID || target.SourceChannelID != session.SourceChannelID {
			return reject(403, "Snapshot target is no longer published")
		}
		// Use a precision supported by every schema dialect for the lease token.
		claimedAt := time.Now().UTC().Truncate(time.Second)
		claimed, err := claimCascadeSnapshotSession(ctx, app.DB(), session.ID, claimedAt)
		if err != nil {
			return reject(503, "Snapshot result routing unavailable")
		}
		if !claimed {
			return reject(503, "Snapshot result forwarding is in progress")
		}
		session.UpdatedAt = claimedAt
		upstreamBody, err := manscdp.BuildUploadSnapShotFinished(cascadeWireProfile(*platform), manscdp.UploadSnapShotFinished{
			SN: notification.SN, DeviceID: session.PublishedChannelID,
			SessionID: session.UpstreamSessionID, FileIDs: notification.FileIDs,
		})
		if err != nil {
			_ = retryCascadeSnapshotSession(context.Background(), app.DB(), session, time.Now())
			return reject(500, "Snapshot result encoding failed")
		}
		client, err := clients.NewClient(*platform)
		if err != nil {
			_ = retryCascadeSnapshotSession(context.Background(), app.DB(), session, time.Now())
			return reject(503, "Snapshot upstream unavailable")
		}
		result := client.(*cascadePlatformClient).transactions.SendMessage(ctx, upstreamBody, fmt.Sprintf("snapshot-result-%d-%d", platform.ID, time.Now().UnixNano()))
		if !result.Success {
			_ = retryCascadeSnapshotSession(context.Background(), app.DB(), session, time.Now())
			return reject(502, "Snapshot upstream rejected result")
		}
		if err := completeCascadeSnapshotSession(ctx, app.DB(), session, time.Now()); err != nil {
			return reject(503, "Snapshot result state unavailable")
		}
		_ = tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
		return true
	}
}

func snapshotCompletionIdentityMatches(req *sip.Request, deviceID, sourceDeviceID, sourceChannelID string) bool {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID != sourceDeviceID && deviceID != sourceChannelID {
		return false
	}
	if req == nil || req.From() == nil || req.From().Address.User == "" {
		return false
	}
	sender := strings.TrimSpace(req.From().Address.User)
	return sender == sourceDeviceID || sender == sourceChannelID
}
