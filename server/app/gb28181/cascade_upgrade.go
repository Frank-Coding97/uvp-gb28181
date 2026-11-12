package gb28181

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/emiago/sipgo/sip"
	"go.uber.org/zap"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/control"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/gb28181/upgrade"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/utils/asyncgroup"
)

func newCascadeUpgradeHandlerWithService(store *repository.GormRepository, clients *cascadePlatformClientFactory, service *upgrade.Service, lifetime context.Context, groups ...*asyncgroup.Group) (func(*sip.Request, sip.ServerTransaction) bool, func()) {
	var running sync.WaitGroup
	handler := func(req *sip.Request, tx sip.ServerTransaction) bool {
		var wire struct {
			XMLName xml.Name  `xml:"Control"`
			CmdType string    `xml:"CmdType"`
			Upgrade *struct{} `xml:"DeviceUpgrade"`
		}
		if manscdp.DecodeProfiledXML(protocol.ProfileFor(protocol.Version2016), req.Body(), &wire) != nil || wire.XMLName.Local != "Control" || wire.CmdType != manscdp.CmdDeviceControl || wire.Upgrade == nil {
			return false
		}
		reject := func(code int, reason string) bool {
			_ = tx.Respond(sip.NewResponseFromRequest(req, code, reason, nil))
			return true
		}
		ctx, cancel := context.WithTimeout(lifetime, 5*time.Second)
		defer cancel()
		platforms, err := store.ListPlatforms(ctx)
		if err != nil {
			return reject(503, "Upgrade unavailable")
		}
		platform, ambiguous := findCascadeControlPeer(req, platforms)
		if platform == nil && !ambiguous {
			return false
		}
		if ambiguous || platform == nil || !platform.Enabled || !platform.UpgradeEnabled {
			return reject(403, "Upgrade not permitted")
		}
		command, err := manscdp.ParseDeviceUpgradeControlWithProfile(cascadeWireProfile(*platform), req.Body())
		if err != nil || !validCascadeGBID(command.DeviceID) {
			return reject(400, "Invalid upgrade command")
		}
		toID := ""
		if req.To() != nil {
			toID = req.To().Address.User
		}
		if !cascadeDeviceStatusRequestTargetsMatch(platform.LocalDeviceID, command.DeviceID, toID, req.Recipient.User) || cascadeCallID(req) == "" || cascadeCSeq(req) == 0 {
			return reject(403, "Upgrade target mismatch")
		}
		projection, err := store.ProjectionSnapshot(ctx, platform.ID)
		if err != nil || projection == nil || !projection.Platform.UpgradeEnabled || !cascadeDeviceStatusAuthorizationCurrent(ctx, store, *platform) {
			return reject(403, "Upgrade authorization changed")
		}
		publication, err := buildCascadeCatalogSnapshot(ctx, store, platform)
		if err != nil || publication.Revision != projection.Revision {
			return reject(503, "Upgrade publication unavailable")
		}
		target, ok := resolveCascadeDeviceStatusTarget(*projection, publication, command.DeviceID)
		if !ok || target.IsPlatform {
			return reject(403, "Upgrade target is not published")
		}
		loaded, err := control.NewGormTargetLoader(app.DB()).LoadAuthorized(ctx, target.SourceDeviceID, target.SourceChannelID)
		if err != nil {
			return reject(403, "Upgrade source unavailable")
		}
		if loaded.Profile.Version != protocol.Version2022 {
			return reject(400, "Device upgrade requires GB2022")
		}
		var device gbmodels.GbDevice
		if err := app.DB().WithContext(ctx).First(&device, loaded.DeviceID).Error; err != nil {
			return reject(503, "Upgrade source unavailable")
		}
		key := cascadeDeviceStatusIdempotencyKey(platform.ID, cascadeCallID(req), cascadeCSeq(req), "upgrade")
		binding := upgrade.CascadeBinding{PlatformID: platform.ID, ConfigRevision: platform.ConfigRevision, ProjectionRevision: platform.ProjectionRevision, PublishedID: command.DeviceID, SourceChannelID: target.SourceChannelID, SourceChannelCode: loaded.ChannelCode, SourceOwnerDeptID: device.OwnerDeptID, SourceDeviceEpoch: loaded.DeviceEpoch, UpstreamSN: command.SN, UpstreamSessionID: command.Command.SessionID, RequestKey: key}
		if service == nil || lifetime.Err() != nil {
			return reject(503, "Upgrade service unavailable")
		}
		if _, err := clients.NewClient(*platform); err != nil {
			return reject(503, "Upgrade response unavailable")
		}
		running.Add(1)
		admitted := make(chan struct{})
		var admission sync.Once
		release := func() { admission.Do(func() { close(admitted) }) }
		work := func() {
			defer running.Done()
			defer release()
			workCtx, workCancel := context.WithTimeout(lifetime, 15*time.Second)
			defer workCancel()
			responded := false
			_, _, err := service.ExecuteCascade(workCtx, upgrade.Target{DeviceID: loaded.DeviceID, DeviceCode: loaded.DeviceCode, IP: loaded.IP, Port: loaded.Port, Transport: loaded.Transport, DeviceOnline: loaded.DeviceOnline, Profile: loaded.Profile}, upgrade.Request{Confirmed: true, IdempotencyKey: key, Firmware: command.Command.Firmware, FileURL: command.Command.FileURL, Manufacturer: command.Command.Manufacturer, Cascade: &binding}, func() error {
				responded = true
				err := tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
				release()
				return err
			})
			if err != nil {
				if !responded {
					code := 503
					if errors.Is(err, upgrade.ErrCascadeUnauthorized) {
						code = 403
					}
					if errors.Is(err, upgrade.ErrIdempotencyConflict) {
						code = 409
					}
					reject(code, "Upgrade request not accepted")
				}
				app.Log(workCtx).Warn("级联升级执行失败", zap.String("event", "cascade.upgrade.execute_failed"), zap.Uint64("platform_id", platform.ID), zap.Error(err))
			}
		}
		if len(groups) > 0 {
			if !groups[0].Go(work) {
				running.Done()
				reject(503, "Upgrade service stopping")
				release()
			}
		} else {
			go work()
		}
		<-admitted
		return true
	}
	return handler, running.Wait
}

// Delivery is derived from durable milestones, not callbacks installed after
// Execute returns. A fast device result and a process restart follow one path.
func deliverCascadeUpgradeResults(ctx context.Context, store *repository.GormRepository, clients *cascadePlatformClientFactory, service *upgrade.Service) error {
	if service == nil {
		return nil
	}
	if err := service.ExpireDue(ctx, time.Now()); err != nil {
		return err
	}
	var failures []error
	for after := uint(0); ctx.Err() == nil; {
		rows, err := service.CascadeResultsAfter(ctx, after, 100)
		if err != nil {
			return errors.Join(append(failures, err)...)
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			after = row.ID
			binding, err := upgrade.DecodeCascadeBinding(row)
			if err != nil || service.AuthorizeCascade(ctx, row) != nil {
				continue
			}
			platform, err := store.FindPlatform(ctx, binding.PlatformID)
			if err != nil {
				failures = append(failures, err)
				continue
			}
			projection, err := store.ProjectionSnapshot(ctx, platform.ID)
			if err != nil || projection == nil {
				continue
			}
			publication, err := buildCascadeCatalogSnapshot(ctx, store, platform)
			if err != nil {
				failures = append(failures, err)
				continue
			}
			target, ok := resolveCascadeDeviceStatusTarget(*projection, publication, binding.PublishedID)
			if !ok || target.SourceDeviceID != uint64(row.DeviceID) || target.SourceChannelID != binding.SourceChannelID || cascadeWireProfile(*platform).Version != protocol.Version2022 {
				continue
			}
			client, err := clients.NewClient(*platform)
			if err != nil {
				failures = append(failures, err)
				continue
			}
			outbound := client.(*cascadePlatformClient).transactions
			for _, final := range []bool{false, true} {
				var body []byte
				if final {
					if row.CascadeFinalDelivered || (row.Status != gbmodels.FirmwareUpgradeSucceeded && row.Status != gbmodels.FirmwareUpgradeFailed) {
						continue
					}
					body, err = manscdp.BuildDeviceUpgradeResultWithProfile(cascadeWireProfile(*platform), manscdp.DeviceUpgradeResultNotify{SN: binding.UpstreamSN, DeviceID: binding.PublishedID, SessionID: binding.UpstreamSessionID, UpgradeResult: row.DeviceResult, Firmware: row.CurrentFirmware, UpgradeFailedReason: row.FailedReason})
				} else {
					if row.CascadeBusinessDelivered || row.CascadeBusinessResult == "" {
						continue
					}
					body, err = manscdp.MarshalProfiledXML(cascadeWireProfile(*platform), struct {
						XMLName  xml.Name `xml:"Response"`
						CmdType  string   `xml:"CmdType"`
						SN       int      `xml:"SN"`
						DeviceID string   `xml:"DeviceID"`
						Result   string   `xml:"Result"`
					}{CmdType: manscdp.CmdDeviceControl, SN: binding.UpstreamSN, DeviceID: binding.PublishedID, Result: row.CascadeBusinessResult})
				}
				if err != nil {
					failures = append(failures, err)
					break
				}
				if strings.EqualFold(platform.Transport, "UDP") && !outbound.MessageFitsUDP(body) {
					failures = append(failures, fmt.Errorf("cascade upgrade response exceeds UDP budget"))
					break
				}
				if err := service.AuthorizeCascade(ctx, row); err != nil {
					break
				}
				sent := outbound.SendMessage(ctx, body, fmt.Sprintf("upgrade-%d-%t", row.ID, final))
				if !sent.Success {
					failures = append(failures, fmt.Errorf("cascade upgrade result delivery failed: status=%d transport=%v build=%v", sent.StatusCode, sent.TransportErr, sent.BuildErr))
					break
				}
				if err := service.MarkCascadeDelivered(ctx, row, final); err != nil {
					failures = append(failures, err)
					break
				}
			}
		}
	}
	return errors.Join(append(failures, ctx.Err())...)
}

type cascadeUpgradeRuntime struct {
	cancel context.CancelFunc
	work   asyncgroup.Group
	Handle func(*sip.Request, sip.ServerTransaction) bool
}

func newCascadeUpgradeRuntime(store *repository.GormRepository, clients *cascadePlatformClientFactory, service *upgrade.Service) *cascadeUpgradeRuntime {
	ctx, cancel := context.WithCancel(context.Background())
	runtime := &cascadeUpgradeRuntime{cancel: cancel}
	runtime.Handle, _ = newCascadeUpgradeHandlerWithService(store, clients, service, ctx, &runtime.work)
	if service != nil {
		runtime.work.Go(func() {
			timer := time.NewTicker(time.Second)
			defer timer.Stop()
			for ctx.Err() == nil {
				if err := deliverCascadeUpgradeResults(ctx, store, clients, service); err != nil && ctx.Err() == nil {
					app.Log(ctx).Warn("级联升级结果投递失败", zap.String("event", "cascade.upgrade.delivery_failed"), zap.Error(err))
				}
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
				}
			}
		})
	}
	return runtime
}

func (r *cascadeUpgradeRuntime) Shutdown(ctx context.Context) error {
	r.cancel()
	return r.work.StopContext(ctx)
}
