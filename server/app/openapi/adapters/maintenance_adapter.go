package adapters

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/catalog"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	runtimeptz "uvplatform.com/uvp-gb28181/app/gb28181/ptz"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

const (
	DeviceRecordStartScope             = "device:recording:start"
	DeviceRecordStopScope              = "device:recording:stop"
	DeviceAlarmGuardSetScope           = "device:alarm:guard:set"
	DeviceAlarmGuardResetScope         = "device:alarm:guard:reset"
	DeviceAlarmResetScope              = "device:alarm:reset"
	DeviceRebootScope                  = "device:maintenance:reboot"
	StorageCardFormatScope             = "channel:storage-card:format"
	MaintenanceOperationReadScope      = "device:maintenance:operation:read"
	DeviceRecordStartAdapterKey        = "device.control.record-start.v1"
	DeviceRecordStopAdapterKey         = "device.control.record-stop.v1"
	DeviceAlarmGuardSetAdapterKey      = "device.control.alarm-guard-set.v1"
	DeviceAlarmGuardResetAdapterKey    = "device.control.alarm-guard-reset.v1"
	DeviceAlarmResetAdapterKey         = "device.control.alarm-reset.v1"
	DeviceRebootAdapterKey             = "device.maintenance.reboot.v1"
	StorageCardFormatAdapterKey        = "device.maintenance.storage-card-format.v1"
	MaintenanceOperationReadAdapterKey = "device.maintenance.operation-read.v1"
)

var (
	ErrMaintenanceTargetNotFound = errors.New("maintenance target not found")
	ErrMaintenanceCapability     = errors.New("maintenance capability is not explicitly supported")
	ErrMaintenanceProfile        = errors.New("maintenance protocol version is unsupported")
)

type MaintenanceDevice struct {
	Target   runtimeptz.DeviceRebootTarget
	TeleBoot manscdp.ControlCapability
}

type MaintenanceChannel struct {
	Target            runtimeptz.Target
	Capabilities      manscdp.ControlCapabilities
	StorageCardFormat manscdp.ControlCapability
	AlarmTargetCode   string
}

type MaintenanceTargetResolver interface {
	ResolveDevice(context.Context, resource.DepartmentScope, string) (MaintenanceDevice, error)
	ResolveChannel(context.Context, resource.DepartmentScope, string, string) (MaintenanceChannel, error)
	ResolveAlarmTarget(context.Context, resource.DepartmentScope, MaintenanceChannel) (string, error)
}

type MaintenanceExecutor interface {
	Execute(context.Context, runtimeptz.Target, runtimeptz.Command) (gbmodels.GbPTZOperation, error)
	ExecuteDeviceRebootDetailed(context.Context, runtimeptz.DeviceRebootTarget, string, uint, uint) (gbmodels.GbPTZOperation, bool, error)
	FormatStorageCard(context.Context, runtimeptz.Target, int, uint, uint, string) (gbmodels.GbPTZOperation, error)
}

type MaintenanceOperationReader interface {
	GetOperationForClient(context.Context, resource.DepartmentScope, string, string, int64) (gbmodels.GbPTZOperation, error)
}

// GormMaintenanceResolver resolves machine-visible GB identifiers under the
// authenticated department boundary. It rejects duplicate device/channel
// identities and preserves an unknown effective protocol version as unknown.
type GormMaintenanceResolver struct {
	db *gorm.DB
}

func NewGormMaintenanceResolver(db *gorm.DB) *GormMaintenanceResolver {
	return &GormMaintenanceResolver{db: db}
}

func (resolver *GormMaintenanceResolver) ResolveDevice(ctx context.Context, scope resource.DepartmentScope, deviceCode string) (MaintenanceDevice, error) {
	if resolver == nil || resolver.db == nil || ctx == nil {
		return MaintenanceDevice{}, ErrUnavailable
	}
	if err := validateResourceScope(scope); err != nil {
		return MaintenanceDevice{}, err
	}
	if !validMaintenanceGBCode(deviceCode) {
		return MaintenanceDevice{}, ErrMaintenanceTargetNotFound
	}
	departmentIDs, err := resource.ResolveDepartmentIDs(ctx, resolver.db, scope)
	if err != nil {
		return MaintenanceDevice{}, err
	}
	var devices []gbmodels.GbDevice
	if err := resolver.db.WithContext(ctx).
		Where("device_id = ? AND owner_dept_id IN ? AND deleted_at IS NULL", deviceCode, departmentIDs).
		Limit(2).Find(&devices).Error; err != nil {
		return MaintenanceDevice{}, err
	}
	if len(devices) != 1 {
		return MaintenanceDevice{}, ErrMaintenanceTargetNotFound
	}
	device := devices[0]
	profile := profileForEffectiveVersion(device.EffectiveVersion)
	teleBoot, err := resolver.resolveDeviceTeleBootCapability(ctx, device)
	if err != nil {
		return MaintenanceDevice{}, err
	}
	return MaintenanceDevice{Target: runtimeptz.DeviceRebootTarget{
		DeviceID: device.ID, DeviceCode: device.DeviceID, IP: device.IP, Port: device.Port,
		Transport: device.Transport, DeviceOnline: device.Status == gbmodels.DeviceStatusOnline, Profile: profile,
	}, TeleBoot: teleBoot}, nil
}

// TeleBoot is a device-level command, while the platform currently persists
// reported control capabilities with channels. Accept explicit support only
// from channels owned by the resolved parent device; conflicting reports and
// incomplete reports remain unknown so callers fail closed.
func (resolver *GormMaintenanceResolver) resolveDeviceTeleBootCapability(ctx context.Context, device gbmodels.GbDevice) (manscdp.ControlCapability, error) {
	var channels []gbmodels.GbChannel
	if err := resolver.db.WithContext(ctx).
		Where("device_id = ? AND owner_dept_id = ? AND deleted_at IS NULL", device.DeviceID, device.OwnerDeptID).
		Find(&channels).Error; err != nil {
		return manscdp.ControlCapability{}, err
	}
	unknown := manscdp.ControlCapability{State: manscdp.CapabilityUnknown, Reason: "设备通道未完整报告 TeleBoot 能力"}
	if len(channels) == 0 {
		return unknown, nil
	}
	supported, unsupported := 0, 0
	for _, channel := range channels {
		capability := manscdp.ParseControlCapabilities(channel.Capabilities, channel.PTZType).TeleBoot
		switch capability.State {
		case manscdp.CapabilitySupported:
			supported++
		case manscdp.CapabilityUnsupported:
			unsupported++
		}
	}
	if supported == len(channels) {
		return manscdp.ControlCapability{State: manscdp.CapabilitySupported, Reason: "设备所属通道明确上报支持"}, nil
	}
	if unsupported == len(channels) {
		return manscdp.ControlCapability{State: manscdp.CapabilityUnsupported, Reason: "设备所属通道均明确上报不支持"}, nil
	}
	if supported > 0 && unsupported > 0 {
		unknown.Reason = "设备所属通道的 TeleBoot 能力报告冲突"
	}
	return unknown, nil
}

func (resolver *GormMaintenanceResolver) ResolveChannel(ctx context.Context, scope resource.DepartmentScope, deviceCode, channelCode string) (MaintenanceChannel, error) {
	if resolver == nil || resolver.db == nil || ctx == nil {
		return MaintenanceChannel{}, ErrUnavailable
	}
	if err := validateResourceScope(scope); err != nil {
		return MaintenanceChannel{}, err
	}
	if !validMaintenanceGBCode(deviceCode) || !validMaintenanceGBCode(channelCode) {
		return MaintenanceChannel{}, ErrMaintenanceTargetNotFound
	}
	departmentIDs, err := resource.ResolveDepartmentIDs(ctx, resolver.db, scope)
	if err != nil {
		return MaintenanceChannel{}, err
	}
	var devices []gbmodels.GbDevice
	if err := resolver.db.WithContext(ctx).
		Where("device_id = ? AND owner_dept_id IN ? AND deleted_at IS NULL", deviceCode, departmentIDs).
		Limit(2).Find(&devices).Error; err != nil {
		return MaintenanceChannel{}, err
	}
	if len(devices) != 1 {
		return MaintenanceChannel{}, ErrMaintenanceTargetNotFound
	}
	device := devices[0]
	var channels []gbmodels.GbChannel
	if err := resolver.db.WithContext(ctx).
		Where("device_id = ? AND channel_id = ? AND owner_dept_id = ? AND owner_dept_id IN ? AND deleted_at IS NULL",
			device.DeviceID, channelCode, device.OwnerDeptID, departmentIDs).
		Limit(2).Find(&channels).Error; err != nil {
		return MaintenanceChannel{}, err
	}
	if len(channels) != 1 {
		return MaintenanceChannel{}, ErrMaintenanceTargetNotFound
	}
	channel := channels[0]
	profile := profileForEffectiveVersion(device.EffectiveVersion)
	target := runtimeptz.Target{
		DeviceID: device.ID, DeviceCode: device.DeviceID, ChannelID: channel.ID, ChannelCode: channel.ChannelID,
		IP: device.IP, Port: device.Port, Transport: device.Transport,
		DeviceOnline:  device.Status == gbmodels.DeviceStatusOnline,
		ChannelOnline: channel.Status == gbmodels.ChannelStatusOnline,
		Profile:       profile,
	}
	return MaintenanceChannel{
		Target:            target,
		Capabilities:      manscdp.ParseControlCapabilities(channel.Capabilities, channel.PTZType),
		StorageCardFormat: parseStorageCardFormatCapability(channel.Capabilities),
	}, nil
}

func (resolver *GormMaintenanceResolver) ResolveAlarmTarget(ctx context.Context, scope resource.DepartmentScope, channel MaintenanceChannel) (string, error) {
	if resolver == nil || resolver.db == nil || ctx == nil {
		return "", ErrUnavailable
	}
	if err := validateResourceScope(scope); err != nil {
		return "", err
	}
	target := channel.Target
	resolution, err := catalog.ResolveAlarmTarget(ctx, resolver.db, target.DeviceID, target.DeviceCode, target.ChannelCode)
	if err != nil {
		return "", err
	}
	if resolution.Status != catalog.AlarmTargetResolved || resolution.Target == nil || strings.TrimSpace(resolution.Target.AlarmCode) == "" {
		return "", ErrMaintenanceTargetNotFound
	}
	return strings.TrimSpace(resolution.Target.AlarmCode), nil
}

// GormMaintenanceOperationReader scopes operation status by external device
// code and by an opaque client-namespaced idempotency key. Knowing an
// operation UUID alone is not enough for another OpenAPI client to read it.
type GormMaintenanceOperationReader struct {
	db *gorm.DB
}

func NewGormMaintenanceOperationReader(db *gorm.DB) *GormMaintenanceOperationReader {
	return &GormMaintenanceOperationReader{db: db}
}

func (reader *GormMaintenanceOperationReader) GetOperationForClient(ctx context.Context, scope resource.DepartmentScope, deviceCode, operationID string, clientID int64) (gbmodels.GbPTZOperation, error) {
	if reader == nil || reader.db == nil || ctx == nil || clientID <= 0 || !validMaintenanceGBCode(deviceCode) || !validMaintenanceOperationID(operationID) {
		return gbmodels.GbPTZOperation{}, ErrMaintenanceTargetNotFound
	}
	if err := validateResourceScope(scope); err != nil {
		return gbmodels.GbPTZOperation{}, err
	}
	departmentIDs, err := resource.ResolveDepartmentIDs(ctx, reader.db, scope)
	if err != nil {
		return gbmodels.GbPTZOperation{}, err
	}
	var operation gbmodels.GbPTZOperation
	result := reader.db.WithContext(ctx).
		Where("operation_id = ? AND device_code = ? AND actor_dept_id IN ? AND (idempotency_key LIKE ? OR idempotency_key LIKE ?)",
			operationID, deviceCode, departmentIDs, clientIdempotencyPrefix(clientID)+"%", runtimeptz.DeviceRebootClientOperationPrefix(uint64(clientID))+"%").
		Limit(1).Find(&operation)
	if result.Error != nil {
		return gbmodels.GbPTZOperation{}, result.Error
	}
	if result.RowsAffected != 1 {
		return gbmodels.GbPTZOperation{}, ErrMaintenanceTargetNotFound
	}
	return operation, nil
}

type maintenanceOperation uint8

const (
	maintenanceRecordStart maintenanceOperation = iota + 1
	maintenanceRecordStop
	maintenanceGuardSet
	maintenanceGuardReset
	maintenanceAlarmReset
	maintenanceReboot
	maintenanceFormatStorageCard
	maintenanceReadOperation
)

type maintenanceAdapter struct {
	key      string
	scope    string
	action   maintenanceOperation
	resolver MaintenanceTargetResolver
	service  MaintenanceExecutor
	reader   MaintenanceOperationReader
}

var _ catalogruntime.Adapter = (*maintenanceAdapter)(nil)

// NewMaintenanceAdapterRegistrations returns the small set of maintenance
// operations backed by the existing durable PTZ domain service. Firmware
// upgrade is deliberately absent until its service enforces published status
// and target model compatibility itself.
func NewMaintenanceAdapterRegistrations(resolver MaintenanceTargetResolver, service MaintenanceExecutor, reader MaintenanceOperationReader) []catalogruntime.AdapterRegistration {
	items := []struct {
		key    string
		scope  string
		action maintenanceOperation
	}{
		{DeviceRecordStartAdapterKey, DeviceRecordStartScope, maintenanceRecordStart},
		{DeviceRecordStopAdapterKey, DeviceRecordStopScope, maintenanceRecordStop},
		{DeviceAlarmGuardSetAdapterKey, DeviceAlarmGuardSetScope, maintenanceGuardSet},
		{DeviceAlarmGuardResetAdapterKey, DeviceAlarmGuardResetScope, maintenanceGuardReset},
		{DeviceAlarmResetAdapterKey, DeviceAlarmResetScope, maintenanceAlarmReset},
		{DeviceRebootAdapterKey, DeviceRebootScope, maintenanceReboot},
		{StorageCardFormatAdapterKey, StorageCardFormatScope, maintenanceFormatStorageCard},
		{MaintenanceOperationReadAdapterKey, MaintenanceOperationReadScope, maintenanceReadOperation},
	}
	registrations := make([]catalogruntime.AdapterRegistration, 0, len(items))
	for _, item := range items {
		adapter := &maintenanceAdapter{key: item.key, scope: item.scope, action: item.action, resolver: resolver, service: service, reader: reader}
		registrations = append(registrations, catalogruntime.AdapterRegistration{
			Key: item.key, ContractVersion: ResourceAdapterContractVersion, Adapter: adapter,
		})
	}
	return registrations
}

func (adapter *maintenanceAdapter) Execute(ctx context.Context, invocation catalogruntime.Invocation) (any, error) {
	if adapter == nil || ctx == nil {
		return nil, ErrUnavailable
	}
	if invocation.Scope != adapter.scope {
		return nil, ErrInvalidRequest
	}
	request, ok := invocation.Value.(Request)
	if !ok || request.ClientID <= 0 {
		return nil, ErrInvalidRequest
	}
	if err := validateResourceScope(request.ResourceScope); err != nil {
		return nil, ErrInvalidRequest
	}
	if adapter.action == maintenanceReadOperation {
		return adapter.readOperation(ctx, invocation, request)
	}
	if adapter.service == nil || adapter.resolver == nil {
		return nil, ErrUnavailable
	}
	if invocation.Method != http.MethodPost || len(invocation.Query) != 0 {
		return nil, ErrInvalidRequest
	}
	input, err := decodeMaintenanceInput(invocation.Body, adapter.action)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	idempotencyKey, err := scopedMaintenanceIdempotencyKey(request.ClientID, request.IdempotencyKey)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	deviceCode := strings.TrimSpace(invocation.Params["deviceId"])
	channelCode := strings.TrimSpace(invocation.Params["channelId"])
	if !validMaintenanceGBCode(deviceCode) {
		return nil, ErrNotFound
	}

	if adapter.action == maintenanceReboot {
		if !input.Confirmed {
			return nil, ErrInvalidRequest
		}
		device, resolveErr := adapter.resolver.ResolveDevice(ctx, request.ResourceScope, deviceCode)
		if resolveErr != nil {
			return nil, normalizeMaintenanceResolveError(resolveErr)
		}
		if err := validateMaintenanceDevice(device.Target, deviceCode); err != nil {
			return nil, err
		}
		if err := requireMaintenanceCapability(device.TeleBoot); err != nil {
			return nil, err
		}
		operation, deduplicated, executeErr := adapter.service.ExecuteDeviceRebootDetailed(ctx, device.Target, idempotencyKey, 0, request.ResourceScope.OwnerDeptID)
		if executeErr != nil {
			return nil, normalizeMaintenanceServiceError(executeErr)
		}
		view := maintenanceOperationView(operation)
		view["deduplicated"] = deduplicated
		return view, nil
	}

	if !validMaintenanceGBCode(channelCode) {
		return nil, ErrNotFound
	}
	channel, resolveErr := adapter.resolver.ResolveChannel(ctx, request.ResourceScope, deviceCode, channelCode)
	if resolveErr != nil {
		return nil, normalizeMaintenanceResolveError(resolveErr)
	}
	if err := validateMaintenanceTarget(channel.Target, deviceCode, channelCode); err != nil {
		return nil, err
	}
	if err := requireKnownMaintenanceProfile(channel.Target.Profile); err != nil {
		return nil, err
	}
	if adapter.action == maintenanceFormatStorageCard && channel.Target.Profile.Version != protocol.Version2022 {
		return nil, fmt.Errorf("%w: %s", ErrCapabilityNotSupported, ErrMaintenanceProfile)
	}
	if adapter.action == maintenanceAlarmReset || adapter.action == maintenanceGuardSet || adapter.action == maintenanceGuardReset {
		alarmCode, alarmErr := adapter.resolver.ResolveAlarmTarget(ctx, request.ResourceScope, channel)
		if alarmErr != nil {
			return nil, normalizeMaintenanceResolveError(alarmErr)
		}
		channel.AlarmTargetCode = strings.TrimSpace(alarmCode)
		if channel.AlarmTargetCode == "" {
			return nil, ErrNotFound
		}
	}
	if adapter.action == maintenanceFormatStorageCard {
		if !input.Confirmed || input.CardIndex == nil || *input.CardIndex < 0 {
			return nil, ErrInvalidRequest
		}
		if err := requireMaintenanceCapability(channel.StorageCardFormat); err != nil {
			return nil, err
		}
		operation, executeErr := adapter.service.FormatStorageCard(ctx, channel.Target, *input.CardIndex, 0, request.ResourceScope.OwnerDeptID, idempotencyKey)
		if executeErr != nil {
			return nil, normalizeMaintenanceServiceError(executeErr)
		}
		return maintenanceOperationView(operation), nil
	}

	command, commandErr := buildMaintenanceCommand(adapter.action, channel, request.ResourceScope, input, idempotencyKey)
	if commandErr != nil {
		return nil, commandErr
	}
	operation, executeErr := adapter.service.Execute(ctx, channel.Target, command)
	if executeErr != nil {
		return nil, normalizeMaintenanceServiceError(executeErr)
	}
	return maintenanceOperationView(operation), nil
}

type maintenanceInput struct {
	Confirmed   bool   `json:"confirmed"`
	CardIndex   *int   `json:"cardIndex"`
	AlarmMethod string `json:"alarmMethod"`
	AlarmType   string `json:"alarmType"`
}

func decodeMaintenanceInput(body []byte, action maintenanceOperation) (maintenanceInput, error) {
	var input maintenanceInput
	trimmed := bytes.TrimSpace(body)
	if !utf8.Valid(body) || len(trimmed) == 0 || trimmed[0] != '{' {
		return input, fmt.Errorf("request body must be a JSON object")
	}
	if hasDuplicateMaintenanceJSONMember(trimmed) {
		return input, fmt.Errorf("request body contains duplicate members")
	}
	allowed := maintenanceInputFields(action)
	var members map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &members); err != nil {
		return input, err
	}
	for name := range members {
		if _, ok := allowed[name]; !ok {
			return input, fmt.Errorf("request body member %q is not allowed", name)
		}
	}
	if err := json.Unmarshal(trimmed, &input); err != nil {
		return input, err
	}
	return input, nil
}

func maintenanceInputFields(action maintenanceOperation) map[string]struct{} {
	var names []string
	switch action {
	case maintenanceReboot:
		names = []string{"confirmed"}
	case maintenanceFormatStorageCard:
		names = []string{"confirmed", "cardIndex"}
	case maintenanceAlarmReset:
		names = []string{"alarmMethod", "alarmType"}
	}
	allowed := make(map[string]struct{}, len(names))
	for _, name := range names {
		allowed[name] = struct{}{}
	}
	return allowed
}

func hasDuplicateMaintenanceJSONMember(body []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return false
	}
	seen := make(map[string]struct{})
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return false
		}
		name, ok := token.(string)
		if !ok {
			return false
		}
		if _, exists := seen[name]; exists {
			return true
		}
		seen[name] = struct{}{}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return false
		}
	}
	return false
}

func (adapter *maintenanceAdapter) readOperation(ctx context.Context, invocation catalogruntime.Invocation, request Request) (any, error) {
	if adapter.reader == nil || adapter.resolver == nil {
		return nil, ErrUnavailable
	}
	if invocation.Method != http.MethodGet || len(invocation.Body) != 0 || len(invocation.Query) != 0 {
		return nil, ErrInvalidRequest
	}
	deviceCode := strings.TrimSpace(invocation.Params["deviceId"])
	operationID := strings.TrimSpace(invocation.Params["operationId"])
	if !validMaintenanceGBCode(deviceCode) || !validMaintenanceOperationID(operationID) {
		return nil, ErrInvalidRequest
	}
	device, err := adapter.resolver.ResolveDevice(ctx, request.ResourceScope, deviceCode)
	if err != nil {
		return nil, normalizeMaintenanceResolveError(err)
	}
	if device.Target.DeviceID == 0 || device.Target.DeviceCode != deviceCode {
		return nil, ErrNotFound
	}
	operation, err := adapter.reader.GetOperationForClient(ctx, request.ResourceScope, deviceCode, operationID, request.ClientID)
	if err != nil {
		return nil, normalizeMaintenanceResolveError(err)
	}
	return maintenanceOperationView(operation), nil
}

func buildMaintenanceCommand(action maintenanceOperation, channel MaintenanceChannel, scope resource.DepartmentScope, input maintenanceInput, idempotencyKey string) (runtimeptz.Command, error) {
	profile := channel.Target.Profile
	command := runtimeptz.Command{
		CmdType: manscdp.CmdDeviceControl, IdempotencyKey: idempotencyKey, Profile: profile,
		ActorDeptID: scope.OwnerDeptID, TargetScope: gbmodels.ControlTargetScopeChannel, TargetCode: channel.Target.ChannelCode,
	}
	switch action {
	case maintenanceRecordStart, maintenanceRecordStop:
		if err := requireMaintenanceCapability(channel.Capabilities.Record); err != nil {
			return runtimeptz.Command{}, err
		}
		command.Action = "record_start"
		recordAction := manscdp.RecordStart
		if action == maintenanceRecordStop {
			command.Action = "record_stop"
			recordAction = manscdp.RecordStop
		}
		command.Payload = map[string]interface{}{"action": command.Action}
		semantic := profile.ResponseFor(protocol.ActionRecord)
		command.ResponseRequired, command.MaxAttempts = semantic.ResponseRequired, 1
		if semantic.ResponseRequired {
			command.MaxAttempts = 3
		}
		command.Build = func(sn int) ([]byte, error) {
			return manscdp.BuildRecordControlWithProfile(profile, channel.Target.ChannelCode, sn, recordAction)
		}
	case maintenanceGuardSet, maintenanceGuardReset, maintenanceAlarmReset:
		var capability manscdp.ControlCapability
		var semantic protocol.Action
		var targetCode string
		if action == maintenanceAlarmReset {
			capability, semantic = channel.Capabilities.AlarmReset, protocol.ActionAlarm
		} else {
			capability, semantic = channel.Capabilities.Guard, protocol.ActionGuard
		}
		if err := requireMaintenanceCapability(capability); err != nil {
			return runtimeptz.Command{}, err
		}
		if action == maintenanceAlarmReset {
			// Alarm controls require one resolved input. The adapter resolves it
			// under the client's department scope before reaching this builder.
			targetCode = strings.TrimSpace(channel.AlarmTargetCode)
			if targetCode == "" {
				return runtimeptz.Command{}, ErrNotFound
			}
			command.TargetScope = gbmodels.ControlTargetScopeAlarm
			command.TargetCode = targetCode
			command.Payload = map[string]interface{}{"action": "alarm_reset", "alarmMethod": input.AlarmMethod, "alarmType": input.AlarmType}
			command.Action = "alarm_reset"
			semantic = protocol.ActionAlarm
			command.Build = func(sn int) ([]byte, error) {
				return manscdp.BuildAlarmResetControlWithProfile(profile, targetCode, sn, manscdp.AlarmResetOptions{AlarmMethod: input.AlarmMethod, AlarmType: input.AlarmType})
			}
		} else {
			guardAction := manscdp.GuardSet
			command.Action = "guard_set"
			if action == maintenanceGuardReset {
				command.Action, guardAction = "guard_reset", manscdp.GuardReset
			}
			command.TargetScope = gbmodels.ControlTargetScopeAlarm
			command.TargetCode = strings.TrimSpace(channel.AlarmTargetCode)
			if command.TargetCode == "" {
				return runtimeptz.Command{}, ErrNotFound
			}
			command.Payload = map[string]interface{}{"action": command.Action}
			command.Build = func(sn int) ([]byte, error) {
				return manscdp.BuildGuardControlWithProfile(profile, command.TargetCode, sn, guardAction)
			}
		}
		response := profile.ResponseFor(semantic)
		command.ResponseRequired, command.MaxAttempts = response.ResponseRequired, 1
		if response.ResponseRequired {
			command.MaxAttempts = 3
		}
	default:
		return runtimeptz.Command{}, ErrInvalidRequest
	}
	return command, nil
}

func requireMaintenanceCapability(capability manscdp.ControlCapability) error {
	if capability.State != manscdp.CapabilitySupported {
		return fmt.Errorf("%w: %s", ErrCapabilityNotSupported, ErrMaintenanceCapability)
	}
	return nil
}

func requireKnownMaintenanceProfile(profile protocol.Profile) error {
	if (profile.Version != protocol.Version2016 && profile.Version != protocol.Version2022) || !protocol.IsSupportedCharset(profile.Charset) {
		return fmt.Errorf("%w: %s", ErrCapabilityNotSupported, ErrMaintenanceProfile)
	}
	return nil
}

func validateMaintenanceTarget(target runtimeptz.Target, deviceCode, channelCode string) error {
	if target.DeviceID == 0 || target.ChannelID == 0 || target.DeviceCode != deviceCode || target.ChannelCode != channelCode {
		return ErrNotFound
	}
	return requireKnownMaintenanceProfile(target.Profile)
}

func validateMaintenanceDevice(target runtimeptz.DeviceRebootTarget, deviceCode string) error {
	if target.DeviceID == 0 || target.DeviceCode != deviceCode {
		return ErrNotFound
	}
	return requireKnownMaintenanceProfile(target.Profile)
}

func profileForEffectiveVersion(version string) protocol.Profile {
	switch strings.TrimSpace(version) {
	case protocol.Version2016:
		return protocol.ProfileFor(protocol.Version2016)
	case protocol.Version2022:
		return protocol.ProfileFor(protocol.Version2022)
	default:
		return protocol.Profile{}
	}
}

func parseStorageCardFormatCapability(raw *string) manscdp.ControlCapability {
	unknown := manscdp.ControlCapability{State: manscdp.CapabilityUnknown, Reason: "设备未显式上报存储卡格式化能力"}
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return unknown
	}
	var reported map[string]json.RawMessage
	if err := json.Unmarshal([]byte(*raw), &reported); err != nil {
		return manscdp.ControlCapability{State: manscdp.CapabilityUnknown, Reason: "Capabilities JSON 无效"}
	}
	if hasDuplicateMaintenanceJSONMember([]byte(*raw)) {
		return manscdp.ControlCapability{State: manscdp.CapabilityUnknown, Reason: "Capabilities JSON 存在重复字段"}
	}
	var valueFound bool
	var supportedValue bool
	for _, key := range []string{"format_sd", "formatSD", "storage_card_format"} {
		value, found := reported[key]
		if !found || string(value) == "null" {
			continue
		}
		var supported bool
		if err := json.Unmarshal(value, &supported); err != nil {
			return manscdp.ControlCapability{State: manscdp.CapabilityUnknown, Reason: "存储卡格式化能力值不是布尔值"}
		}
		if valueFound && supportedValue != supported {
			return manscdp.ControlCapability{State: manscdp.CapabilityUnknown, Reason: "存储卡格式化能力报告冲突"}
		}
		valueFound = true
		supportedValue = supported
	}
	if valueFound && supportedValue {
		return manscdp.ControlCapability{State: manscdp.CapabilitySupported, Reason: "设备明确上报支持"}
	}
	if valueFound {
		return manscdp.ControlCapability{State: manscdp.CapabilityUnsupported, Reason: "设备明确上报不支持"}
	}
	return unknown
}

func normalizeMaintenanceResolveError(err error) error {
	switch {
	case errors.Is(err, ErrMaintenanceTargetNotFound), errors.Is(err, resource.ErrResourceNotFound):
		return ErrNotFound
	case errors.Is(err, ErrInvalidRequest), errors.Is(err, resource.ErrInvalidDepartmentScope):
		return ErrInvalidRequest
	default:
		return ErrUnavailable
	}
}

func normalizeMaintenanceServiceError(err error) error {
	var operationErr *runtimeptz.OperationError
	if errors.As(err, &operationErr) {
		switch operationErr.Code {
		case runtimeptz.ErrorCodeHomePositionInvalidArgument:
			return ErrInvalidRequest
		case runtimeptz.ErrorCodeHomePositionNotFound:
			return ErrNotFound
		case runtimeptz.ErrorCodeHomePositionDeviceOffline:
			return ErrDeviceOffline
		case runtimeptz.ErrorCodeHomePositionIdempotencyConflict:
			return ErrConflict
		default:
			return ErrUnavailable
		}
	}
	return ErrUnavailable
}

func maintenanceOperationView(operation gbmodels.GbPTZOperation) map[string]any {
	acknowledged := operation.ResponseRequired && (operation.Status == gbmodels.PTZOperationAccepted || operation.Status == gbmodels.PTZOperationRejected)
	return map[string]any{
		"operationId": operation.OperationID, "action": operation.Action, "status": operation.Status,
		"sn": operation.SN, "responseRequired": operation.ResponseRequired,
		"deviceAcknowledged": acknowledged, "profileVersion": operation.ProfileVersion,
	}
}

func scopedMaintenanceIdempotencyKey(clientID int64, key string) (string, error) {
	key = strings.TrimSpace(key)
	if clientID <= 0 || key == "" || !utf8.ValidString(key) || len(key) > 128 {
		return "", ErrInvalidRequest
	}
	for _, r := range key {
		if unicode.IsControl(r) {
			return "", ErrInvalidRequest
		}
	}
	sum := sha256.Sum256([]byte(key))
	return clientIdempotencyPrefix(clientID) + hex.EncodeToString(sum[:]), nil
}

func clientIdempotencyPrefix(clientID int64) string {
	return fmt.Sprintf("openapi-client:%d:", clientID)
}

func validMaintenanceGBCode(value string) bool {
	if len(value) != 20 {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func validMaintenanceOperationID(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' {
			continue
		}
		return false
	}
	return true
}

var _ MaintenanceTargetResolver = (*GormMaintenanceResolver)(nil)
var _ MaintenanceOperationReader = (*GormMaintenanceOperationReader)(nil)
