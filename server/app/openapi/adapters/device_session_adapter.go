package adapters

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/google/uuid"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	devicesessions "uvplatform.com/uvp-gb28181/app/openapi/device_sessions"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

const (
	CaptureCreateScope      = "channel:snapshot:create"
	CaptureReadScope        = "channel:snapshot:read"
	CaptureImageReadScope   = "channel:snapshot:image:read"
	CaptureCreateAdapterKey = "device.capture.create.v1"
	CaptureReadAdapterKey   = "device.capture.read.v1"
	CaptureImageAdapterKey  = "device.capture.image-content.v1"

	// 图像库（gb_channel_snapshot）的读与删。
	// ⛔ 删除单独一个权限码、且是新码：图像库删除是**不可恢复**的（库行 + 磁盘文件一起删），
	// 绝不能挂在任何既有的读码上 —— 否则所有只读账号一夜之间获得删图权。
	LibraryReadScope        = "channel:snapshot:library:read"
	LibraryImageReadScope   = "channel:snapshot:library:image:read"
	LibraryDeleteScope      = "channel:snapshot:library:delete"
	LibraryReadAdapterKey   = "device.capture.library.v1"
	LibraryImageAdapterKey  = "device.capture.library-image.v1"
	LibraryDeleteAdapterKey = "device.capture.library-delete.v1"
)

type captureOperation uint8

const (
	captureCreate captureOperation = iota + 1
	captureRead
	captureImageRead
	libraryList
	libraryImageRead
	libraryDelete
)

type DeviceSessionService interface {
	CreateCapture(context.Context, devicesessions.CaptureRequest) (devicesessions.CaptureSession, error)
	GetCapture(context.Context, int64, resource.DepartmentScope, string, string, string) (devicesessions.CaptureSession, error)
	ReadImage(context.Context, int64, resource.DepartmentScope, string, string, string, string) (devicesessions.ImageContent, error)
	ListLibrary(context.Context, resource.DepartmentScope, string, string, devicesessions.LibraryListOptions) (devicesessions.LibraryPage, error)
	ReadLibraryImage(context.Context, resource.DepartmentScope, string, string, uint64) (devicesessions.ImageContent, error)
	DeleteLibraryImages(context.Context, resource.DepartmentScope, string, string, []uint64) (devicesessions.LibraryDeleteResult, error)
}

type deviceSessionAdapter struct {
	key       string
	scope     string
	method    string
	operation captureOperation
	service   DeviceSessionService
}

var _ catalogruntime.Adapter = (*deviceSessionAdapter)(nil)
var _ DeviceSessionService = (*devicesessions.Service)(nil)

func newDeviceSessionAdapters(service DeviceSessionService) []*deviceSessionAdapter {
	return []*deviceSessionAdapter{
		&deviceSessionAdapter{key: CaptureCreateAdapterKey, scope: CaptureCreateScope, method: "POST", operation: captureCreate, service: service},
		&deviceSessionAdapter{key: CaptureReadAdapterKey, scope: CaptureReadScope, method: "GET", operation: captureRead, service: service},
		&deviceSessionAdapter{key: CaptureImageAdapterKey, scope: CaptureImageReadScope, method: "GET", operation: captureImageRead, service: service},
		&deviceSessionAdapter{key: LibraryReadAdapterKey, scope: LibraryReadScope, method: "GET", operation: libraryList, service: service},
		&deviceSessionAdapter{key: LibraryImageAdapterKey, scope: LibraryImageReadScope, method: "GET", operation: libraryImageRead, service: service},
		&deviceSessionAdapter{key: LibraryDeleteAdapterKey, scope: LibraryDeleteScope, method: "DELETE", operation: libraryDelete, service: service},
	}
}

// NewDeviceSessionCaptureAdapterRegistrations 只注册需要"下发命令 + 设备回传"的三条抓图能力。
//
// ⛔ 它依赖 SIP 收图注册表**和**管理员配置的设备可达上传基址 —— 缺任一条，
// 抓图下发了也收不回图，因此调用方应当在依赖不全时整组跳过而不是让它运行期 503。
func NewDeviceSessionCaptureAdapterRegistrations(service DeviceSessionService) []catalogruntime.AdapterRegistration {
	return deviceSessionRegistrations(newDeviceSessionAdapters(service)[0:3])
}

// NewDeviceSessionLibraryAdapterRegistrations 只注册图像库的读与删。
//
// 图像库只依赖库行和磁盘目录，设备离线或设备入口不可用时仍可读取历史图片。
func NewDeviceSessionLibraryAdapterRegistrations(service DeviceSessionService) []catalogruntime.AdapterRegistration {
	return deviceSessionRegistrations(newDeviceSessionAdapters(service)[3:6])
}

func deviceSessionRegistrations(adapters []*deviceSessionAdapter) []catalogruntime.AdapterRegistration {
	registrations := make([]catalogruntime.AdapterRegistration, 0, len(adapters))
	for _, adapter := range adapters {
		registrations = append(registrations, catalogruntime.AdapterRegistration{
			Key: adapter.Key(), ContractVersion: ResourceAdapterContractVersion, Adapter: adapter,
		})
	}
	return registrations
}

func (adapter *deviceSessionAdapter) Key() string {
	if adapter == nil {
		return ""
	}
	return adapter.key
}

func (adapter *deviceSessionAdapter) Execute(ctx context.Context, invocation catalogruntime.Invocation) (data any, err error) {
	defer func() {
		if recover() != nil {
			data, err = nil, ErrUnavailable
		}
	}()
	if adapter == nil || adapter.service == nil || ctx == nil {
		return nil, ErrUnavailable
	}
	if invocation.Scope != adapter.scope || strings.ToUpper(invocation.Method) != adapter.method {
		return nil, ErrInvalidRequest
	}
	request, ok := invocation.Value.(Request)
	if !ok || request.ClientID <= 0 || !validResourceScope(request.ResourceScope) {
		return nil, ErrInvalidRequest
	}
	deviceID, channelID := invocation.Params["deviceId"], invocation.Params["channelId"]
	if !validPathID(deviceID) || !validPathID(channelID) {
		return nil, ErrInvalidRequest
	}
	switch adapter.operation {
	case captureCreate:
		input, decodeErr := decodeCaptureInput(invocation.Body)
		if decodeErr != nil || strings.TrimSpace(request.IdempotencyKey) == "" {
			return nil, ErrInvalidRequest
		}
		data, err := adapter.service.CreateCapture(ctx, devicesessions.CaptureRequest{
			ClientID: request.ClientID, Scope: request.ResourceScope,
			DeviceID: deviceID, ChannelID: channelID, IdempotencyKey: request.IdempotencyKey,
			SnapNum: input.SnapNum, Interval: input.Interval,
		})
		return data, normalizeDeviceSessionError(err)
	case captureRead:
		if len(invocation.Body) != 0 || len(invocation.Query) != 0 {
			return nil, ErrInvalidRequest
		}
		sessionID := invocation.Params["sessionId"]
		if !validSessionID(sessionID) {
			return nil, ErrInvalidRequest
		}
		data, err := adapter.service.GetCapture(ctx, request.ClientID, request.ResourceScope, deviceID, channelID, sessionID)
		return data, normalizeDeviceSessionError(err)
	case captureImageRead:
		if len(invocation.Body) != 0 || len(invocation.Query) != 0 {
			return nil, ErrInvalidRequest
		}
		sessionID, fileName := invocation.Params["sessionId"], invocation.Params["fileName"]
		if !validSessionID(sessionID) || fileName == "" {
			return nil, ErrInvalidRequest
		}
		content, readErr := adapter.service.ReadImage(ctx, request.ClientID, request.ResourceScope, deviceID, channelID, sessionID, fileName)
		if readErr != nil {
			return nil, normalizeDeviceSessionError(readErr)
		}
		return imageContentEnvelope(content), nil
	case libraryList, libraryImageRead, libraryDelete:
		return adapter.executeLibrary(ctx, invocation, request, deviceID, channelID)
	default:
		return nil, ErrUnavailable
	}
}

// 图像库三条与抓图三条共用同一个适配器类型，但**不共用任何一条请求语义**：
// 抓图按会话说话（sessionId / fileName），图像库按通道说话（时间窗 / 图片主键）。
func (adapter *deviceSessionAdapter) executeLibrary(ctx context.Context, invocation catalogruntime.Invocation, request Request, deviceID, channelID string) (any, error) {
	switch adapter.operation {
	case libraryList:
		if len(invocation.Body) != 0 {
			return nil, ErrInvalidRequest
		}
		options, err := decodeLibraryOptions(invocation.Query)
		if err != nil {
			return nil, err
		}
		page, err := adapter.service.ListLibrary(ctx, request.ResourceScope, deviceID, channelID, options)
		return page, normalizeDeviceSessionError(err)
	case libraryImageRead:
		if len(invocation.Body) != 0 || len(invocation.Query) != 0 {
			return nil, ErrInvalidRequest
		}
		snapshotID, err := parseSnapshotID(invocation.Params["snapshotId"])
		if err != nil {
			return nil, err
		}
		content, err := adapter.service.ReadLibraryImage(ctx, request.ResourceScope, deviceID, channelID, snapshotID)
		if err != nil {
			return nil, normalizeDeviceSessionError(err)
		}
		return imageContentEnvelope(content), nil
	case libraryDelete:
		if len(invocation.Query) != 0 {
			return nil, ErrInvalidRequest
		}
		input, err := decodeLibraryDelete(invocation.Body)
		if err != nil {
			return nil, err
		}
		result, err := adapter.service.DeleteLibraryImages(ctx, request.ResourceScope, deviceID, channelID, input.IDs)
		return result, normalizeDeviceSessionError(err)
	default:
		return nil, ErrUnavailable
	}
}

type libraryDeleteInput struct {
	IDs       []uint64 `json:"ids"`
	Confirmed bool     `json:"confirmed"`
}

// decodeLibraryDelete 自己再验一遍 confirmed 与数量上限。
// ⛔ 网关已经按 RequestSchema 拒过一遍，这里重复不是因为不信任网关，而是因为
// "不可恢复的删除"这一层的判据不该只存在于一个地方 —— 适配器被别的调用路径
// 复用时（测试、内部工具），漏掉它就是直接删光数据。
func decodeLibraryDelete(body []byte) (libraryDeleteInput, error) {
	var input libraryDeleteInput
	if len(body) == 0 || len(body) > 4096 {
		return input, ErrInvalidRequest
	}
	decoder := json.NewDecoder(io.LimitReader(bytes.NewReader(body), 4097))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return libraryDeleteInput{}, ErrInvalidRequest
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return libraryDeleteInput{}, ErrInvalidRequest
	}
	if !input.Confirmed || len(input.IDs) == 0 || len(input.IDs) > maxLibraryDeleteIDs {
		return libraryDeleteInput{}, ErrInvalidRequest
	}
	return input, nil
}

const maxLibraryDeleteIDs = 100

func decodeLibraryOptions(query map[string]string) (devicesessions.LibraryListOptions, error) {
	options := devicesessions.LibraryListOptions{
		From: query["from"], To: query["to"], Source: query["source"],
	}
	for name, target := range map[string]*int{"page": &options.Page, "pageSize": &options.PageSize} {
		raw, exists := query[name]
		if !exists || raw == "" {
			continue
		}
		value, err := strconv.Atoi(raw)
		if err != nil || value <= 0 {
			return devicesessions.LibraryListOptions{}, ErrInvalidRequest
		}
		*target = value
	}
	if options.PageSize > 100 {
		return devicesessions.LibraryListOptions{}, ErrInvalidRequest
	}
	return options, nil
}

func parseSnapshotID(raw string) (uint64, error) {
	// ⛔ 图像库主键是数据库自增 id，不是 20 位国标编码：这里按十进制正整数解析，
	// 不能套用 validPathID（那会把所有真实主键判成非法）。
	if raw == "" || len(raw) > 20 {
		return 0, ErrInvalidRequest
	}
	for _, char := range raw {
		if char < '0' || char > '9' {
			return 0, ErrInvalidRequest
		}
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || value == 0 {
		return 0, ErrInvalidRequest
	}
	return value, nil
}

func imageContentEnvelope(content devicesessions.ImageContent) any {
	return struct {
		FileName      string `json:"fileName"`
		ContentType   string `json:"contentType"`
		Size          int    `json:"size"`
		MD5           string `json:"md5"`
		ContentBase64 string `json:"contentBase64"`
	}{
		FileName: content.FileName, ContentType: content.ContentType, Size: len(content.Bytes), MD5: content.MD5,
		ContentBase64: base64.StdEncoding.EncodeToString(content.Bytes),
	}
}

func normalizeDeviceSessionError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, devicesessions.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, devicesessions.ErrInvalidRequest):
		return ErrInvalidRequest
	case errors.Is(err, devicesessions.ErrConflict):
		return ErrConflict
	case errors.Is(err, devicesessions.ErrDeviceOffline):
		return ErrDeviceOffline
	case errors.Is(err, devicesessions.ErrCapabilityNotSupported), errors.Is(err, devicesessions.ErrStopUnsupported):
		return ErrCapabilityNotSupported
	default:
		return ErrUnavailable
	}
}

type captureInput struct {
	SnapNum  int `json:"snapNum"`
	Interval int `json:"interval"`
}

func decodeCaptureInput(body []byte) (captureInput, error) {
	var input captureInput
	if len(body) == 0 || len(body) > 4096 {
		return input, ErrInvalidRequest
	}
	decoder := json.NewDecoder(io.LimitReader(bytes.NewReader(body), 4097))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return captureInput{}, ErrInvalidRequest
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return captureInput{}, ErrInvalidRequest
	}
	if input.SnapNum < 1 || input.SnapNum > 10 || input.Interval < 1 || input.Interval > 3600 {
		return captureInput{}, ErrInvalidRequest
	}
	return input, nil
}

func validResourceScope(scope resource.DepartmentScope) bool {
	return scope.OwnerDeptID > 0 && (scope.DataScope == resource.DataScopeDepartment || scope.DataScope == resource.DataScopeDepartmentAndChildren)
}

func validPathID(value string) bool {
	return len(value) == 20 && allDecimal(value)
}

func validSessionID(value string) bool {
	if len(value) != 36 {
		return false
	}
	_, err := uuid.Parse(value)
	return err == nil
}

func allDecimal(value string) bool {
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}
