package gb28181

import (
	"context"
	"encoding/xml"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/emiago/sipgo/sip"
	"go.uber.org/zap"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/control"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.com/uvp-gb28181/app/gb28181/catalog"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

type cascadeAdvancedControlCommand struct {
	SN           int
	DeviceID     string
	Action       string
	TargetCode   string
	Alarm        manscdp.AlarmResetOptions
	StreamNumber *int
}

type cascadeAdvancedControlResult struct {
	body      []byte
	authorize func(context.Context) error
}
type cascadeAdvancedControlExecutor func(context.Context, cascadeConfigWriteTarget, cascadeAdvancedControlCommand, ptz.CascadeDeviceControlAuthorization, string) (cascadeAdvancedControlResult, error)

func newCascadeAdvancedControlHandler(store *repository.GormRepository, clients *cascadePlatformClientFactory) func(*sip.Request, sip.ServerTransaction) bool {
	handler, _ := newCascadeAdvancedControlHandlerWithExecutor(store, clients, executeCascadeAdvancedControl)
	return handler
}

func newCascadeAdvancedControlHandlerWithExecutor(store *repository.GormRepository, clients *cascadePlatformClientFactory, execute cascadeAdvancedControlExecutor) (func(*sip.Request, sip.ServerTransaction) bool, func()) {
	var running sync.WaitGroup
	handler := func(req *sip.Request, tx sip.ServerTransaction) bool {
		var head struct {
			XMLName   xml.Name `xml:"Control"`
			CmdType   string   `xml:"CmdType"`
			RecordCmd *string  `xml:"RecordCmd"`
			GuardCmd  *string  `xml:"GuardCmd"`
			AlarmCmd  *string  `xml:"AlarmCmd"`
		}
		if manscdp.DecodeProfiledXML(protocol.ProfileFor(protocol.Version2016), req.Body(), &head) != nil || head.XMLName.Local != "Control" || head.CmdType != manscdp.CmdDeviceControl {
			return false
		}
		if head.RecordCmd == nil && head.GuardCmd == nil && head.AlarmCmd == nil {
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
			return reject(503, "Device control unavailable")
		}
		platform, ambiguous := findCascadeControlPeer(req, platforms)
		if platform == nil && !ambiguous {
			return false
		}
		if ambiguous || platform == nil || !platform.Enabled || !platform.DeviceControlEnabled {
			return reject(403, "Device control not permitted")
		}
		command, err := parseCascadeAdvancedControl(cascadeWireProfile(*platform), req.Body())
		if err != nil {
			return reject(400, "Invalid device control command")
		}
		if !validCascadeGBID(command.DeviceID) {
			return reject(400, "Invalid device control target")
		}
		toID := ""
		if req.To() != nil {
			toID = req.To().Address.User
		}
		if !cascadeDeviceStatusRequestTargetsMatch(platform.LocalDeviceID, command.DeviceID, toID, req.Recipient.User) {
			return reject(403, "Device control target mismatch")
		}
		projection, err := store.ProjectionSnapshot(ctx, platform.ID)
		if err != nil || projection == nil || !projection.Platform.DeviceControlEnabled || !cascadeDeviceStatusAuthorizationCurrent(ctx, store, *platform) {
			return reject(403, "Device control authorization changed")
		}
		publication, err := buildCascadeCatalogSnapshot(ctx, store, &projection.Platform)
		if err != nil || publication.Revision != projection.Revision {
			return reject(503, "Device control publication unavailable")
		}
		target, ok := resolveCascadeDeviceStatusTarget(*projection, publication, command.DeviceID)
		if !ok || target.IsPlatform {
			return reject(403, "Device control target is not published")
		}
		loaded, err := control.NewGormTargetLoader(app.DB()).LoadAuthorized(ctx, target.SourceDeviceID, target.SourceChannelID)
		if err != nil {
			return reject(403, "Device control source unavailable")
		}
		targetInfo := cascadeConfigWriteTarget{cascadeDeviceStatusTarget: target, source: loaded}
		targetCode := loaded.ChannelCode
		if command.Action == "guard_set" || command.Action == "guard_reset" || command.Action == "alarm_reset" {
			resolution, resolveErr := catalog.ResolveAlarmTarget(ctx, app.DB(), uint(target.SourceDeviceID), loaded.DeviceCode, loaded.ChannelCode)
			if resolveErr != nil {
				return reject(503, "Alarm target unavailable")
			}
			switch resolution.Status {
			case catalog.AlarmTargetResolved:
				if resolution.Target == nil || strings.TrimSpace(resolution.Target.AlarmCode) == "" {
					return reject(403, "Alarm target unavailable")
				}
				targetCode = strings.TrimSpace(resolution.Target.AlarmCode)
			case catalog.AlarmTargetAmbiguous:
				return reject(403, "Alarm target is ambiguous")
			case catalog.AlarmTargetUnavailable:
				targetCode = loaded.DeviceCode
			default:
				return reject(403, "Alarm target unavailable")
			}
		}
		command.TargetCode = targetCode
		client, err := clients.NewClient(projection.Platform)
		if err != nil {
			return reject(503, "Device control response unavailable")
		}
		if tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil)) != nil {
			return true
		}
		key := fmt.Sprintf("cascade-control:%d:%s:%d:%s", platform.ID, cascadeCallID(req), cascadeCSeq(req), command.DeviceID)
		snapshot, profile := projection.Platform, cascadeWireProfile(projection.Platform)
		authorization := ptz.CascadeDeviceControlAuthorization{PlatformID: snapshot.ID, ConfigRevision: snapshot.ConfigRevision, ProjectionRevision: snapshot.ProjectionRevision, PublishedID: command.DeviceID, Action: command.Action}
		running.Add(1)
		go func() {
			defer running.Done()
			workCtx, workCancel := context.WithTimeout(context.Background(), 35*time.Second)
			defer workCancel()
			result, runErr := execute(workCtx, targetInfo, command, authorization, key)
			if runErr == nil && result.authorize == nil {
				runErr = fmt.Errorf("device source authorization missing")
			}
			if runErr == nil {
				runErr = result.authorize(workCtx)
			}
			if runErr != nil {
				app.Log(workCtx).Warn("级联高级控制未取得设备业务结果", zap.String("event", "cascade.control.advanced_failed"), zap.Uint64("platform_id", snapshot.ID), zap.String("published_target_id", command.DeviceID), zap.String("action", command.Action), zap.Error(runErr))
				checkCtx, checkCancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer checkCancel()
				if cascadeDeviceControlAuthorizationCurrent(checkCtx, store, snapshot) && cascadeAdvancedSourceAuthorizationCurrent(checkCtx, targetInfo) {
					sendCascadeAdvancedControlResponse(client.(*cascadePlatformClient), store, snapshot, profile, command, manscdp.DeviceControlResultError)
				}
				return
			}
			ack, parseErr := manscdp.ParseDeviceControlResponseWithProfile(loaded.Profile, result.body)
			if parseErr != nil {
				return
			}
			if !cascadeDeviceControlAuthorizationCurrent(workCtx, store, snapshot) {
				return
			}
			if cascadeDeviceControlAuthorizationCurrent(workCtx, store, snapshot) {
				sendCascadeAdvancedControlResponse(client.(*cascadePlatformClient), store, snapshot, profile, command, ack.Result)
			}
		}()
		return true
	}
	return handler, running.Wait
}

func cascadeAdvancedSourceAuthorizationCurrent(ctx context.Context, target cascadeConfigWriteTarget) bool {
	current, err := control.NewGormTargetLoader(app.DB()).LoadAuthorized(ctx, target.SourceDeviceID, target.SourceChannelID)
	return err == nil && current.DeviceEpoch == target.source.DeviceEpoch && current.DeviceCode == target.source.DeviceCode && current.ChannelCode == target.source.ChannelCode
}

func sendCascadeAdvancedControlResponse(client *cascadePlatformClient, store *repository.GormRepository, snapshot model.GbCascadePlatform, profile protocol.Profile, command cascadeAdvancedControlCommand, result manscdp.DeviceControlResult) {
	response, err := manscdp.MarshalProfiledXML(profile, struct {
		XMLName  xml.Name `xml:"Response"`
		CmdType  string   `xml:"CmdType"`
		SN       int      `xml:"SN"`
		DeviceID string   `xml:"DeviceID"`
		Result   string   `xml:"Result"`
	}{CmdType: manscdp.CmdDeviceControl, SN: command.SN, DeviceID: command.DeviceID, Result: string(result)})
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if !cascadeDeviceControlAuthorizationCurrent(ctx, store, snapshot) {
		return
	}
	sent := client.transactions.SendMessage(ctx, response, fmt.Sprintf("advanced-control-%d-%d", snapshot.ID, time.Now().UnixNano()))
	if !sent.Success {
		app.Log(ctx).Warn("级联高级控制业务应答发送失败", zap.String("event", "cascade.control.advanced_response_failed"), zap.Uint64("platform_id", snapshot.ID), zap.Int("status", sent.StatusCode))
	}
}

func cascadeCSeq(req *sip.Request) uint32 {
	if req.CSeq() != nil {
		return req.CSeq().SeqNo
	}
	return 0
}

func parseCascadeAdvancedControl(profile protocol.Profile, body []byte) (cascadeAdvancedControlCommand, error) {
	var wire struct {
		XMLName      xml.Name                   `xml:"Control"`
		CmdType      string                     `xml:"CmdType"`
		SN           int                        `xml:"SN"`
		DeviceID     string                     `xml:"DeviceID"`
		RecordCmd    *string                    `xml:"RecordCmd"`
		StreamNumber *int                       `xml:"StreamNumber"`
		GuardCmd     *string                    `xml:"GuardCmd"`
		AlarmCmd     *string                    `xml:"AlarmCmd"`
		Info         *manscdp.AlarmResetOptions `xml:"Info"`
	}
	if err := manscdp.DecodeProfiledXML(profile, body, &wire); err != nil {
		return cascadeAdvancedControlCommand{}, err
	}
	if wire.XMLName.Local != "Control" || wire.CmdType != manscdp.CmdDeviceControl || wire.SN <= 0 || strings.TrimSpace(wire.DeviceID) == "" {
		return cascadeAdvancedControlCommand{}, fmt.Errorf("invalid DeviceControl envelope")
	}
	count := 0
	action := ""
	if wire.RecordCmd != nil {
		count++
		switch strings.TrimSpace(*wire.RecordCmd) {
		case string(manscdp.RecordStart):
			action = "record_start"
		case string(manscdp.RecordStop):
			action = "record_stop"
		default:
			return cascadeAdvancedControlCommand{}, fmt.Errorf("invalid RecordCmd")
		}
	}
	if wire.GuardCmd != nil {
		count++
		switch strings.TrimSpace(*wire.GuardCmd) {
		case string(manscdp.GuardSet):
			action = "guard_set"
		case string(manscdp.GuardReset):
			action = "guard_reset"
		default:
			return cascadeAdvancedControlCommand{}, fmt.Errorf("invalid GuardCmd")
		}
	}
	var alarm manscdp.AlarmResetOptions
	if wire.AlarmCmd != nil {
		count++
		if strings.TrimSpace(*wire.AlarmCmd) != "ResetAlarm" {
			return cascadeAdvancedControlCommand{}, fmt.Errorf("invalid AlarmCmd")
		}
		action = "alarm_reset"
		if wire.Info != nil {
			alarm = *wire.Info
		}
		if err := manscdp.ValidateAlarmResetOptions(alarm); err != nil {
			return cascadeAdvancedControlCommand{}, err
		}
	}
	if count != 1 {
		return cascadeAdvancedControlCommand{}, fmt.Errorf("exactly one advanced command is required")
	}
	if wire.StreamNumber != nil && (action != "record_start" && action != "record_stop" || *wire.StreamNumber < 0 || *wire.StreamNumber > 255) {
		return cascadeAdvancedControlCommand{}, fmt.Errorf("invalid StreamNumber")
	}
	if profile.Version != protocol.Version2022 && wire.StreamNumber != nil {
		return cascadeAdvancedControlCommand{}, fmt.Errorf("2016 RecordCmd does not support StreamNumber")
	}
	if wire.Info != nil && action != "alarm_reset" {
		return cascadeAdvancedControlCommand{}, fmt.Errorf("Info is only valid for AlarmCmd")
	}
	if profile.Version != protocol.Version2022 && wire.Info != nil && (strings.TrimSpace(wire.Info.AlarmMethod) != "" || strings.TrimSpace(wire.Info.AlarmType) != "") {
		return cascadeAdvancedControlCommand{}, fmt.Errorf("2016 AlarmCmd does not support Info selectors")
	}
	return cascadeAdvancedControlCommand{SN: wire.SN, DeviceID: strings.TrimSpace(wire.DeviceID), Action: action, Alarm: alarm, StreamNumber: wire.StreamNumber}, nil
}

func executeCascadeAdvancedControl(ctx context.Context, target cascadeConfigWriteTarget, command cascadeAdvancedControlCommand, authorization ptz.CascadeDeviceControlAuthorization, key string) (cascadeAdvancedControlResult, error) {
	service := ptzService
	if service == nil || !service.RequiresDeviceAuthorization() {
		return cascadeAdvancedControlResult{}, fmt.Errorf("authorized device-control service unavailable")
	}
	loader := control.NewGormTargetLoader(app.DB())
	loaded := target.source
	check := func(checkCtx context.Context) error {
		current, err := loader.LoadAuthorized(checkCtx, target.SourceDeviceID, target.SourceChannelID)
		if err != nil || current.DeviceEpoch != loaded.DeviceEpoch || current.DeviceCode != loaded.DeviceCode || current.ChannelCode != loaded.ChannelCode {
			return fmt.Errorf("device control source authorization changed")
		}
		return nil
	}
	if err := check(ctx); err != nil {
		return cascadeAdvancedControlResult{}, err
	}
	body, err := service.AdvancedControlForCascade(ctx, loaded, command.Action, command.TargetCode, command.Alarm, command.StreamNumber, authorization, key)
	if err != nil {
		return cascadeAdvancedControlResult{}, err
	}
	return cascadeAdvancedControlResult{body: body, authorize: check}, nil
}

func cascadeDeviceControlAuthorizationCurrent(ctx context.Context, store *repository.GormRepository, snapshot model.GbCascadePlatform) bool {
	current, err := store.FindPlatform(ctx, snapshot.ID)
	return err == nil && current.Enabled && current.DeviceControlEnabled && current.ConfigRevision == snapshot.ConfigRevision && current.ProjectionRevision == snapshot.ProjectionRevision
}
