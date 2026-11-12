package gb28181

import (
	"context"
	"crypto/sha256"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/emiago/sipgo/sip"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/deviceaccess"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/control"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/gb28181/ptz"
	gbsetup "uvplatform.com/uvp-gb28181/app/gb28181/setup"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

type cascadeConfigWriteTarget struct {
	cascadeDeviceStatusTarget
	source ptz.Target
}

type cascadeConfigWriteFunc func(context.Context, cascadeConfigWriteTarget, *manscdp.CascadeConfigWrite, ptz.CascadeConfigAuthorization, string) (cascadeConfigQueryResult, error)

func newCascadeConfigWriteHandler(store *repository.GormRepository, clients *cascadePlatformClientFactory) func(*sip.Request, sip.ServerTransaction) bool {
	handler, _ := newCascadeConfigWriteHandlerWithExecutor(store, clients, writeCascadeConfig)
	return handler
}

func newCascadeConfigWriteHandlerWithExecutor(store *repository.GormRepository, clients *cascadePlatformClientFactory, execute cascadeConfigWriteFunc) (func(*sip.Request, sip.ServerTransaction) bool, func()) {
	var running sync.WaitGroup
	handler := func(req *sip.Request, tx sip.ServerTransaction) bool {
		var envelope struct {
			XMLName xml.Name `xml:"Control"`
			CmdType string   `xml:"CmdType"`
		}
		if manscdp.DecodeProfiledXML(protocol.ProfileFor(protocol.Version2016), req.Body(), &envelope) != nil || envelope.CmdType != manscdp.CmdDeviceConfig {
			return false
		}
		reject := func(code int, reason string) bool {
			_ = tx.Respond(sip.NewResponseFromRequest(req, code, reason, nil))
			return true
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		platforms, err := store.ListPlatforms(ctx)
		if err != nil {
			return reject(503, "Configuration write unavailable")
		}
		platform, ambiguous := findCascadeControlPeer(req, platforms)
		if platform == nil && !ambiguous {
			return false
		}
		if ambiguous || platform == nil || !platform.Enabled || !platform.ConfigEnabled {
			return reject(403, "Configuration write not permitted")
		}
		command, err := manscdp.ParseCascadeConfigWrite(req.Body())
		if err != nil || !validCascadeGBID(command.DeviceID()) {
			return reject(400, "Invalid configuration command")
		}
		toID := ""
		if req.To() != nil {
			toID = req.To().Address.User
		}
		if !cascadeDeviceStatusRequestTargetsMatch(platform.LocalDeviceID, command.DeviceID(), toID, req.Recipient.User) {
			return reject(403, "Configuration write target mismatch")
		}
		projection, err := store.ProjectionSnapshot(ctx, platform.ID)
		if err != nil || projection == nil || !projection.Platform.ConfigEnabled || !cascadeDeviceStatusAuthorizationCurrent(ctx, store, *platform) {
			return reject(403, "Configuration write authorization changed")
		}
		publication, err := buildCascadeCatalogSnapshot(ctx, store, &projection.Platform)
		if err != nil || publication.Revision != projection.Revision {
			return reject(503, "Configuration write publication unavailable")
		}
		target, ok := resolveCascadeDeviceStatusTarget(*projection, publication, command.DeviceID())
		if !ok || target.IsPlatform {
			return reject(403, "Configuration write target is not published")
		}
		boundSource, err := control.NewGormTargetLoader(app.DB()).LoadAuthorized(ctx, target.SourceDeviceID, target.SourceChannelID)
		if err != nil {
			return reject(403, "Configuration write source unavailable")
		}
		cseq := uint32(0)
		if req.CSeq() != nil {
			cseq = req.CSeq().SeqNo
		}
		key := strings.Replace(cascadeDeviceStatusIdempotencyKey(platform.ID, cascadeCallID(req), cseq, command.DeviceID()), "cascade-device-status:", "cascade-config-write:", 1)
		canonical, err := command.Build(protocol.ProfileFor(protocol.Version2022), command.DeviceID(), command.SN())
		if err != nil {
			return reject(400, "Invalid configuration command")
		}
		requestKey, fingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(key))), fmt.Sprintf("%x", sha256.Sum256(canonical))
		var priorCapture model.GbCascadeSnapshotSession
		priorErr := app.DB().WithContext(ctx).Where("platform_id = ? AND request_key = ?", platform.ID, requestKey).First(&priorCapture).Error
		if priorErr != nil && !errors.Is(priorErr, gorm.ErrRecordNotFound) {
			return reject(503, "Snapshot session persistence unavailable")
		}
		if priorErr == nil && priorCapture.RequestFingerprint != fingerprint {
			return reject(409, "Snapshot transaction content changed")
		}
		var captureSession *model.GbCascadeSnapshotSession
		captureOwner := false
		if snapshot, ok := command.SnapshotConfig(); ok {
			if cascadeWireProfile(projection.Platform).Version != protocol.Version2022 || boundSource.Profile.Version != protocol.Version2022 || snapshot.SessionID == nil {
				return reject(403, "Snapshot capture requires GB/T 28181-2022 on both cascade legs")
			}
			base, baseErr := cascadeSnapshotBaseURL(ctx)
			if baseErr != nil || base == "" || snapshot.UploadURL == nil {
				return reject(503, "Snapshot upload base unavailable")
			}
			now := time.Now().UTC()
			duration := 10 * time.Minute
			if snapshot.SnapNum != nil && snapshot.Interval != nil && *snapshot.SnapNum > 0 && *snapshot.Interval > 0 {
				duration += time.Duration(*snapshot.SnapNum**snapshot.Interval) * time.Second
			}
			if duration > 24*time.Hour {
				duration = 24 * time.Hour
			}
			captureSession = &model.GbCascadeSnapshotSession{
				PlatformID: projection.Platform.ID, UpstreamSessionID: strings.TrimSpace(*snapshot.SessionID),
				UpstreamUploadURL: strings.TrimSpace(*snapshot.UploadURL),
				RequestKey:        requestKey, RequestFingerprint: fingerprint,
				PublishedChannelID: target.PublishedID,
				SourceDeviceID:     target.SourceDeviceID, SourceChannelID: target.SourceChannelID,
				SourceDeviceEpoch: boundSource.DeviceEpoch,
				ConfigRevision:    projection.Platform.ConfigRevision, ProjectionRevision: projection.Platform.ProjectionRevision,
				State: model.CascadeSnapshotSessionPending, ExpiresAt: now.Add(duration), CreatedAt: now, UpdatedAt: now,
			}
			savedCaptureSession, created, err := reserveCascadeSnapshotSession(ctx, app.DB(), *captureSession)
			if err != nil {
				if errors.Is(err, ErrCascadeSnapshotSessionConflict) {
					return reject(409, "Snapshot session conflicts with an existing request")
				}
				return reject(503, "Snapshot session persistence unavailable")
			}
			captureSession = &savedCaptureSession
			captureOwner = created
			if captureSession.DownstreamSessionID == nil || captureSession.UploadToken == nil {
				return reject(409, "Legacy snapshot routing cannot be replaced")
			}
			command, err = command.WithSnapshotRouting(*captureSession.DownstreamSessionID, base+"/api/gb28181/cascade-snapshots/uploads/"+*captureSession.UploadToken)
			if err != nil {
				if captureOwner {
					failCascadeSnapshotSession(ctx, app.DB(), captureSession.ID)
				}
				return reject(400, "Invalid snapshot routing")
			}
		}
		boundTarget := cascadeConfigWriteTarget{cascadeDeviceStatusTarget: target, source: boundSource}
		client, err := clients.NewClient(projection.Platform)
		if err != nil {
			if captureOwner {
				failCascadeSnapshotSession(context.Background(), app.DB(), captureSession.ID)
			}
			return reject(503, "Configuration write response unavailable")
		}
		if tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil)) != nil {
			if captureOwner {
				failCascadeSnapshotSession(context.Background(), app.DB(), captureSession.ID)
			}
			return true
		}
		snapshot, profile := projection.Platform, cascadeWireProfile(projection.Platform)
		authorization := ptz.CascadeConfigAuthorization{PlatformID: snapshot.ID, ConfigRevision: snapshot.ConfigRevision, ProjectionRevision: snapshot.ProjectionRevision, PublishedID: command.DeviceID()}
		running.Add(1)
		go func() {
			defer running.Done()
			writeCtx, writeCancel := context.WithTimeout(context.Background(), 40*time.Second)
			defer writeCancel()
			captureAccepted := false
			if captureOwner {
				defer func() {
					if !captureAccepted {
						failCascadeSnapshotSession(context.Background(), app.DB(), captureSession.ID)
					}
				}()
			}
			result := "OK"
			if captureSession != nil && !captureOwner {
				cached, err := awaitCascadeSnapshotRequestResult(writeCtx, app.DB(), captureSession.ID)
				result = cached
				if err != nil {
					result = "ERROR"
				}
			}
			var sourceChecks []func(context.Context) error
			// Channel parameters precede BasicParam, which may change device registration.
			for _, deviceScope := range []bool{false, true} {
				if captureSession != nil && !captureOwner {
					break
				}
				scoped := command.Scoped(deviceScope)
				if scoped == nil {
					continue
				}
				if !cascadeConfigWriteAuthorizationCurrent(writeCtx, store, snapshot) {
					return
				}
				source, err := execute(writeCtx, boundTarget, scoped, authorization, fmt.Sprintf("%s:%t", key, deviceScope))
				if err == nil && source.authorize == nil {
					err = fmt.Errorf("configuration source authorization missing")
				}
				if err == nil {
					var ack *manscdp.DeviceConfigAck
					ack, err = manscdp.ParseCascadeConfigWriteResponse(source.body, manscdp.ConfigDownloadExpectation{})
					if err == nil && !ack.Accepted() {
						err = fmt.Errorf("device rejected configuration")
					}
				}
				if err != nil {
					result = "ERROR"
					app.Log(writeCtx).Warn("级联配置写入失败", zap.String("event", "cascade.config.write_failed"), zap.Uint64("platform_id", snapshot.ID), zap.String("published_target_id", target.PublishedID), zap.Error(err))
					break
				}
				sourceChecks = append(sourceChecks, source.authorize)
			}
			sendCtx, sendCancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer sendCancel()
			for _, check := range sourceChecks {
				if check(sendCtx) != nil {
					result = "ERROR"
					break
				}
			}
			if !cascadeConfigWriteAuthorizationCurrent(sendCtx, store, snapshot) {
				return
			}
			if captureSession != nil {
				if result == "OK" {
					current, err := control.NewGormTargetLoader(app.DB()).LoadAuthorized(sendCtx, captureSession.SourceDeviceID, captureSession.SourceChannelID)
					if err != nil || current.DeviceEpoch != captureSession.SourceDeviceEpoch {
						result = "ERROR"
					}
				}
				if captureOwner {
					if err := recordCascadeSnapshotRequestResult(sendCtx, app.DB(), captureSession.ID, result); err != nil {
						return
					}
				}
			}
			if result == "OK" && captureSession != nil {
				captureAccepted = true
			}
			body, err := manscdp.MarshalProfiledXML(profile, struct {
				XMLName  xml.Name `xml:"Response"`
				CmdType  string   `xml:"CmdType"`
				SN       int      `xml:"SN"`
				DeviceID string   `xml:"DeviceID"`
				Result   string   `xml:"Result"`
			}{CmdType: manscdp.CmdDeviceConfig, SN: command.SN(), DeviceID: command.DeviceID(), Result: result})
			if err != nil {
				return
			}
			sent := client.(*cascadePlatformClient).transactions.SendMessage(sendCtx, body, fmt.Sprintf("config-write-%d-%d", snapshot.ID, time.Now().UnixNano()))
			if !sent.Success {
				app.Log(sendCtx).Warn("级联配置应答发送失败", zap.String("event", "cascade.config.response_send_failed"), zap.Uint64("platform_id", snapshot.ID), zap.Int("status", sent.StatusCode))
			}
		}()
		return true
	}
	return handler, running.Wait
}

func cascadeConfigWriteAuthorizationCurrent(ctx context.Context, store *repository.GormRepository, snapshot model.GbCascadePlatform) bool {
	current, err := store.FindPlatform(ctx, snapshot.ID)
	return err == nil && current.ConfigEnabled && current.Enabled && current.ConfigRevision == snapshot.ConfigRevision && current.ProjectionRevision == snapshot.ProjectionRevision
}

func writeCascadeConfig(ctx context.Context, target cascadeConfigWriteTarget, command *manscdp.CascadeConfigWrite, authorization ptz.CascadeConfigAuthorization, key string) (cascadeConfigQueryResult, error) {
	if ptzService == nil || !ptzService.RequiresDeviceAuthorization() {
		return cascadeConfigQueryResult{}, fmt.Errorf("authorized configuration service unavailable")
	}
	loader := control.NewGormTargetLoader(app.DB())
	loaded := target.source
	check := func(checkCtx context.Context) error {
		current, err := loader.LoadAuthorized(checkCtx, target.SourceDeviceID, target.SourceChannelID)
		if err != nil || current.DeviceEpoch != loaded.DeviceEpoch || current.DeviceCode != loaded.DeviceCode || current.ChannelCode != loaded.ChannelCode {
			return fmt.Errorf("configuration write source authorization changed")
		}
		return nil
	}
	if err := check(ctx); err != nil {
		return cascadeConfigQueryResult{}, err
	}
	body, err := ptzService.ConfigWriteForCascade(ctx, loaded, command, authorization, key)
	if err != nil {
		return cascadeConfigQueryResult{}, err
	}
	return cascadeConfigQueryResult{body: body, authorize: check}, nil
}

// SIP-initiated captures have no HTTP ingress request. Reuse the current SIP
// address and the Nginx port supplied by the launcher, without caching browser Hosts.
func cascadeSnapshotBaseURL(ctx context.Context) (string, error) {
	view, err := gbsetup.NewSIPConfigService(app.DB()).Get(ctx)
	if err != nil {
		return "", err
	}
	if view == nil {
		return "", fmt.Errorf("SIP configuration unavailable")
	}
	addresses, err := gbsetup.ActiveSIPAddresses(*view, nil)
	if err != nil {
		return "", err
	}
	if len(addresses) != 1 {
		return "", fmt.Errorf("device ingress requires one unambiguous SIP address")
	}
	return deviceaccess.ForHost(addresses[0], os.Getenv("UVP_DEVICE_HTTP_PORT"))
}
