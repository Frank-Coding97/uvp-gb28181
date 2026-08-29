package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/management"
	"uvplatform.cn/uvp-gb28181/app/middleware"
)

// The controller interfaces are deliberately method-shaped instead of
// accepting a generic API name or query map. Concrete management services
// satisfy these interfaces directly, while tests and bootstrap adapters can
// provide a narrow implementation without widening the HTTP boundary.
type ZLMOverviewAPI interface {
	GetOverview(context.Context) (management.OverviewResult, error)
	GetNodeRuntime(context.Context, int64) (management.NodeRuntimeView, error)
	ListStreams(context.Context, management.StreamFilter, management.PageRequest) (management.StreamDistribution, error)
}

type ZLMStreamAPI interface {
	ListStreams(context.Context, management.StreamListRequest) (management.StreamListResult, error)
	GetStreamDetail(context.Context, int64, management.MediaIdentity) (*management.StreamDetail, error)
	ListStreamViewers(context.Context, int64, management.MediaIdentity, management.PageRequest) (management.StreamViewerPage, error)
	IssuePreviewGrant(context.Context, uint64, management.PreviewGrantRequest) (*management.PreviewGrantResponse, error)
	PreflightCloseStream(context.Context, management.OwnershipTarget) (management.StreamClosePreview, error)
	CloseStream(context.Context, management.CloseStreamRequest) (management.StreamCloseResult, error)
	ForceCloseStream(context.Context, management.ForceCloseStreamRequest) (management.StreamCloseResult, error)
	PreflightCloseStreams(context.Context, []management.OwnershipTarget) (management.OwnershipBatchPreflight, error)
	CloseStreams(context.Context, management.OwnershipBatchPreflight) (management.StreamCloseBatchResult, error)
}

type ZLMSnapshotAPI interface {
	FetchSnapshot(context.Context, int64, management.MediaIdentity) (management.SnapshotResult, error)
}

type ZLMSessionAPI interface {
	ListNetworkSessions(context.Context, management.NetworkSessionListRequest) (management.NetworkSessionPage, error)
	ListMediaViewers(context.Context, management.MediaViewerListRequest) (management.StreamViewerPage, error)
	KickSession(context.Context, management.KickSessionRequest) (management.KickSessionResult, error)
}

type ZLMProxyAPI interface {
	CreatePullProxy(context.Context, uint64, management.PullProxyRequest) (*management.ProxyView, error)
	CreatePushProxy(context.Context, uint64, management.PushProxyRequest) (*management.ProxyView, error)
	ListPullProxies(context.Context, int64, ...management.PageRequest) (management.ProxyPage, error)
	ListPushProxies(context.Context, int64, ...management.PageRequest) (management.ProxyPage, error)
	GetPullProxy(context.Context, int64, string) (*management.ProxyView, error)
	GetPushProxy(context.Context, int64, string) (*management.ProxyView, error)
	PreviewDeletePullProxy(context.Context, management.ProxyDeleteRequest) (management.ProxyDeletePreview, error)
	PreviewDeletePushProxy(context.Context, management.ProxyDeleteRequest) (management.ProxyDeletePreview, error)
	DeletePullProxy(context.Context, uint64, management.ProxyDeleteRequest) (*management.ProxyDeleteView, error)
	DeletePushProxy(context.Context, uint64, management.ProxyDeleteRequest) (*management.ProxyDeleteView, error)
}

type ZLMFFmpegAPI interface {
	CreateSource(context.Context, int64, uint64, management.FFmpegSourceCreateRequest) (management.FFmpegSourceView, error)
	ListSourcesPage(context.Context, int64, management.PageRequest) (management.FFmpegSourcePage, error)
	PreflightDelete(context.Context, int64, string) (management.OwnershipPreflight, error)
	Delete(context.Context, int64, string) (management.FFmpegDeleteResult, error)
}

type ZLMRTPAPI interface {
	CreateServer(context.Context, int64, uint64, management.RTPServerCreateRequest) (management.RTPServerView, error)
	ListServersPage(context.Context, int64, management.PageRequest) (management.RTPServerPage, error)
	PreflightClose(context.Context, management.RTPServerCloseRequest) (management.OwnershipPreflight, error)
	Close(context.Context, management.RTPServerCloseRequest) (management.RTPServerCloseView, error)
	ForceCloseServer(context.Context, management.RTPServerForceCloseRequest) (management.RTPServerCloseView, error)
}

type ZLMRecordingAPI interface {
	Preflight(context.Context, management.RecordingStartRequest) (management.RecordingPreflight, error)
	PreflightStop(context.Context, uint, management.RecordingStopRequest) (management.RecordingPreflight, error)
	PreflightForceStop(context.Context, management.RecordingForceStopRequest) (management.RecordingPreflight, error)
	StartRecording(context.Context, uint, management.RecordingStartRequest) (management.RecordingResult, error)
	StopRecording(context.Context, uint, management.RecordingStopRequest) (management.RecordingResult, error)
	ForceStopRecording(context.Context, uint, management.RecordingForceStopRequest) (management.RecordingResult, error)
	GetStatus(context.Context, management.RecordingStartRequest) (management.RecordingResult, error)
}

// ZLMManagementBundle is the single injection point for the management HTTP
// facade. A nil field means that capability is not wired yet and is exposed
// as a typed 503; handlers never panic or silently switch to a raw proxy.
type ZLMManagementBundle struct {
	Overview  ZLMOverviewAPI
	Streams   ZLMStreamAPI
	Snapshot  ZLMSnapshotAPI
	Sessions  ZLMSessionAPI
	Proxies   ZLMProxyAPI
	FFmpeg    ZLMFFmpegAPI
	RTP       ZLMRTPAPI
	Recording ZLMRecordingAPI
}

type ZLMManagementController struct {
	bundle *ZLMManagementBundle
}

func NewZLMManagementController(bundle *ZLMManagementBundle) *ZLMManagementController {
	return &ZLMManagementController{bundle: bundle}
}

func (controller *ZLMManagementController) serviceUnavailable(c *gin.Context, nodeID int64, capability string) {
	message := "media management service is not configured"
	if strings.TrimSpace(capability) != "" {
		message = capability + " management service is not configured"
	}
	writeManagementError(c, management.NewServiceUnavailableError(nodeIDText(nodeID), message))
}

func requestContext(c *gin.Context) context.Context {
	if c == nil || c.Request == nil || c.Request.Context() == nil {
		return context.Background()
	}
	return c.Request.Context()
}

func writeManagementSuccess(c *gin.Context, data any) {
	if c == nil {
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "", "data": data})
}

func writeManagementError(c *gin.Context, err error) {
	if c == nil {
		return
	}
	nodeID := nodeIDFromContext(c)
	var validation *management.ValidationError
	if errors.As(err, &validation) && validation != nil {
		c.JSON(http.StatusBadRequest, validation)
		return
	}
	if err == nil {
		err = management.NewInternalError(nodeID, "internal management error")
	}
	if errors.Is(err, management.ErrPreviewUnsupported) {
		err = management.NewUnsupportedCapabilityError(nodeID, "preview", err)
	}
	if errors.Is(err, management.ErrGBPreviewRequiresPlayAuth) {
		err = management.NewUnsupportedCapabilityError(nodeID, "gb28181 play authorization", err)
	}
	if errors.Is(err, management.ErrSnapshotTimeout) {
		err = management.NewUpstreamTimeoutError(nodeID, err)
	}
	if errors.Is(err, management.ErrSnapshotInvalidResponse) || errors.Is(err, management.ErrSnapshotTooLarge) || errors.Is(err, management.ErrSnapshotUpstream) {
		err = management.NewInternalError(nodeID, "snapshot request failed", err)
	}
	normalized := management.NormalizeError(err, nodeID)
	validation = nil
	if errors.As(normalized, &validation) && validation != nil {
		c.JSON(http.StatusBadRequest, validation)
		return
	}
	if typed, ok := management.AsManagementError(normalized); ok && typed != nil {
		c.JSON(typed.HTTPStatus(), typed)
		return
	}
	// NormalizeError currently returns a ManagementError for every unknown
	// cause. Keep this fallback defensive so a future service cannot make a
	// controller leak an arbitrary error string.
	fallback := management.NewInternalError(nodeID, "internal management error")
	c.JSON(fallback.HTTPStatus(), fallback)
}

func bindManagementJSON(c *gin.Context, target any) error {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return management.NewValidationError(map[string]string{"body": "is required"})
	}
	// MaxBytesReader reports an explicit read error once the cap is crossed.
	// A plain LimitReader could turn a valid prefix whose JSON closes exactly at
	// the cap into an apparently complete request and silently accept a larger
	// body with the tail discarded.
	const maxManagementBodyBytes = 1 << 20
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxManagementBodyBytes)
	decoder := json.NewDecoder(c.Request.Body)
	var payload json.RawMessage
	if err := decoder.Decode(&payload); err != nil {
		return management.NewValidationError(map[string]string{"body": "must be valid JSON"})
	}
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return management.NewValidationError(map[string]string{"body": "must be a JSON object"})
	}
	objectDecoder := json.NewDecoder(bytes.NewReader(trimmed))
	objectDecoder.DisallowUnknownFields()
	if err := objectDecoder.Decode(target); err != nil {
		return management.NewValidationError(map[string]string{"body": "must be valid JSON"})
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return management.NewValidationError(map[string]string{"body": "must contain one JSON object"})
	}
	return nil
}

func parsePositivePathID(c *gin.Context) (int64, error) {
	if c == nil {
		return 0, management.NewValidationError(map[string]string{"nodeId": "must be positive"})
	}
	raw := strings.TrimSpace(c.Param("id"))
	if raw == "" {
		raw = strings.TrimSpace(c.Param("nodeId"))
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, management.NewValidationError(map[string]string{"nodeId": "must be positive"})
	}
	return id, nil
}

func nodeIDText(nodeID int64) string {
	if nodeID <= 0 {
		return ""
	}
	return strconv.FormatInt(nodeID, 10)
}

func nodeIDFromContext(c *gin.Context) string {
	if c == nil {
		return ""
	}
	raw := strings.TrimSpace(c.Param("id"))
	if raw == "" {
		raw = strings.TrimSpace(c.Param("nodeId"))
	}
	if id, err := strconv.ParseInt(raw, 10, 64); err == nil && id > 0 {
		return nodeIDText(id)
	}
	return ""
}

func setNodeID(pathID int64, value *int64, field string) error {
	if pathID <= 0 || value == nil {
		return management.NewValidationError(map[string]string{field: "must be positive"})
	}
	if *value == 0 {
		*value = pathID
		return nil
	}
	if *value != pathID {
		return management.NewValidationError(map[string]string{field: "must match the path node"})
	}
	return nil
}

func setTargetNodeID(pathID int64, target *management.OwnershipTarget) error {
	if target == nil {
		return management.NewValidationError(map[string]string{"target": "is required"})
	}
	return setNodeID(pathID, &target.NodeID, "target.nodeId")
}

func querySingle(c *gin.Context, key string) (string, bool, error) {
	values, present := c.Request.URL.Query()[key]
	if !present {
		return "", false, nil
	}
	if len(values) != 1 || strings.TrimSpace(values[0]) == "" {
		return "", true, management.NewValidationError(map[string]string{key: "must contain one non-empty value"})
	}
	return values[0], true, nil
}

func parsePageQuery(c *gin.Context, allowed ...string) (management.PageRequest, error) {
	allowedSet := make(map[string]struct{}, len(allowed)+2)
	for _, key := range allowed {
		allowedSet[key] = struct{}{}
	}
	allowedSet["page"] = struct{}{}
	allowedSet["pageSize"] = struct{}{}
	for key := range c.Request.URL.Query() {
		if _, ok := allowedSet[key]; !ok {
			return management.PageRequest{}, management.NewValidationError(map[string]string{key: "is not supported"})
		}
	}
	page := management.PageRequest{}
	for key, destination := range map[string]*int{"page": &page.Page, "pageSize": &page.PageSize} {
		raw, present, err := querySingle(c, key)
		if err != nil {
			return management.PageRequest{}, err
		}
		if !present {
			continue
		}
		value, parseErr := strconv.Atoi(raw)
		if parseErr != nil || value < 1 {
			return management.PageRequest{}, management.NewValidationError(map[string]string{key: "must be a positive integer"})
		}
		*destination = value
	}
	return page.Normalize(), nil
}

func parseMediaQuery(c *gin.Context, allowed ...string) (management.MediaIdentity, error) {
	allowedSet := make(map[string]struct{}, len(allowed)+4)
	for _, key := range allowed {
		allowedSet[key] = struct{}{}
	}
	for _, key := range []string{"schema", "vhost", "app", "stream"} {
		allowedSet[key] = struct{}{}
	}
	for key := range c.Request.URL.Query() {
		if _, ok := allowedSet[key]; !ok {
			return management.MediaIdentity{}, management.NewValidationError(map[string]string{key: "is not supported"})
		}
	}
	values := make(map[string]string, 4)
	for _, key := range []string{"schema", "vhost", "app", "stream"} {
		raw, present, err := querySingle(c, key)
		if err != nil {
			return management.MediaIdentity{}, err
		}
		if !present {
			values[key] = ""
			continue
		}
		values[key] = raw
	}
	media := management.MediaIdentity{Schema: values["schema"], Vhost: values["vhost"], App: values["app"], Stream: values["stream"]}
	if err := media.Validate(); err != nil {
		return management.MediaIdentity{}, err
	}
	return media, nil
}

// parseMediaPageQuery validates and decodes the shared media identity and
// pagination query as one contract. Keeping the two allow-lists in this
// helper prevents a legal media+page request from being rejected by the
// second parser as an unknown field.
func parseMediaPageQuery(c *gin.Context) (management.MediaIdentity, management.PageRequest, error) {
	media, err := parseMediaQuery(c, "page", "pageSize")
	if err != nil {
		return management.MediaIdentity{}, management.PageRequest{}, err
	}
	page, err := parsePageQuery(c, "schema", "vhost", "app", "stream")
	if err != nil {
		return management.MediaIdentity{}, management.PageRequest{}, err
	}
	return media, page, nil
}

func parseOptionalIntQuery(c *gin.Context, key string) (*int, error) {
	raw, present, err := querySingle(c, key)
	if err != nil || !present {
		return nil, err
	}
	value, parseErr := strconv.Atoi(raw)
	if parseErr != nil {
		return nil, management.NewValidationError(map[string]string{key: "must be an integer"})
	}
	return &value, nil
}

func parseOptionalBoolQuery(c *gin.Context, key string) (*bool, error) {
	raw, present, err := querySingle(c, key)
	if err != nil || !present {
		return nil, err
	}
	value, parseErr := strconv.ParseBool(raw)
	if parseErr != nil {
		return nil, management.NewValidationError(map[string]string{key: "must be true or false"})
	}
	return &value, nil
}

func markManagementAudit(c *gin.Context, action string, nodeID int64, target *management.OwnershipTarget, fingerprint, reason, result string) {
	metadata := make(map[string]any, 6)
	if strings.TrimSpace(action) != "" {
		metadata["action"] = boundedSafeText(management.RedactSensitiveText(action), 64)
	}
	if nodeID > 0 {
		metadata["nodeId"] = nodeID
	}
	if target != nil {
		metadata["target"] = safeManagementAuditTarget(*target)
	}
	if strings.TrimSpace(fingerprint) != "" {
		metadata["fingerprint"] = boundedSafeText(management.RedactSensitiveText(fingerprint), 128)
	}
	if strings.TrimSpace(reason) != "" {
		metadata["reason"] = boundedSafeText(management.RedactSensitiveText(reason), 256)
	}
	if strings.TrimSpace(result) != "" {
		metadata["result"] = boundedSafeText(management.RedactSensitiveText(result), 128)
	}
	middleware.MarkSensitiveOperation(c, metadata)
}

func safeManagementAuditTarget(target management.OwnershipTarget) management.OwnershipTarget {
	target.Media.Schema = safeManagementAuditText(target.Media.Schema, 128)
	target.Media.Vhost = safeManagementAuditText(target.Media.Vhost, 255)
	target.Media.App = safeManagementAuditText(target.Media.App, 255)
	target.Media.Stream = safeManagementAuditText(target.Media.Stream, 255)
	return target
}

func safeManagementAuditText(value string, maxRunes int) string {
	return boundedSafeText(management.RedactSensitiveText(value), maxRunes)
}

func boundedSafeText(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	if maxRunes <= 0 {
		return ""
	}
	if utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	if maxRunes <= 3 {
		return string([]rune(value)[:maxRunes])
	}
	return string([]rune(value)[:maxRunes-3]) + "..."
}

func managementActionResult(ok bool) string {
	if ok {
		return "success"
	}
	return "failed"
}

// boundedBatchActionResult records only a capped per-item outcome vocabulary;
// it intentionally omits target names, URLs and upstream response payloads.
func boundedBatchActionResult(results []string) string {
	const maxItems = 32
	if len(results) == 0 {
		return "empty"
	}
	limit := len(results)
	if limit > maxItems {
		limit = maxItems
	}
	value := strings.Join(results[:limit], ",")
	if len(results) > limit {
		value += ",truncated"
	}
	return boundedSafeText(value, 128)
}
