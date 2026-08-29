package zlm

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Statistic is the object-instance snapshot returned by ZLM getStatistic.
// The values are unsigned counters in ZLM, so keeping them as uint64 avoids
// truncating a long-running server's counters on 32-bit platforms.
type Statistic struct {
	MediaSource           uint64 `json:"MediaSource"`
	MultiMediaSourceMuxer uint64 `json:"MultiMediaSourceMuxer"`
	TcpServer             uint64 `json:"TcpServer"`
	TcpSession            uint64 `json:"TcpSession"`
	UdpServer             uint64 `json:"UdpServer"`
	UdpSession            uint64 `json:"UdpSession"`
	TcpClient             uint64 `json:"TcpClient"`
	Socket                uint64 `json:"Socket"`
	FrameImp              uint64 `json:"FrameImp"`
	Frame                 uint64 `json:"Frame"`
	Buffer                uint64 `json:"Buffer"`
	BufferRaw             uint64 `json:"BufferRaw"`
	BufferLikeString      uint64 `json:"BufferLikeString"`
	BufferList            uint64 `json:"BufferList"`
	RtpPacket             uint64 `json:"RtpPacket"`
	RtmpPacket            uint64 `json:"RtmpPacket"`
}

// ServerStatistic is kept as a descriptive alias for callers that prefer to
// distinguish this object snapshot from application-level statistics.
type ServerStatistic = Statistic

// SessionFilter limits getAllSession results. A zero-valued filter means no
// filtering, matching ZLM's API semantics.
type SessionFilter struct {
	LocalPort int
	PeerIP    string
}

// Session is one network session returned by ZLM getAllSession.
type Session struct {
	ID         string `json:"id"`
	PeerIP     string `json:"peer_ip"`
	PeerPort   int    `json:"peer_port"`
	LocalIP    string `json:"local_ip"`
	LocalPort  int    `json:"local_port"`
	Identifier string `json:"identifier"`
	Type       string `json:"type"`
	TypeID     string `json:"typeid"`
}

// ZLMSession is a descriptive alias for callers that want to make the source
// of a session explicit.
type ZLMSession = Session

// CapabilityState describes whether a ZLM API is known to be available.
// Unknown is deliberately different from Unsupported: a timeout or malformed
// response must not make a capability look absent.
type CapabilityState string

const (
	CapabilitySupported   CapabilityState = "supported"
	CapabilityUnsupported CapabilityState = "unsupported"
	CapabilityUnknown     CapabilityState = "unknown"
)

const (
	CapabilityGetStatistic  = "getStatistic"
	CapabilityGetAllSession = "getAllSession"
	CapabilityGetAPIList    = "getApiList"
)

// CapabilityProfile is derived from one successful getApiList response. APIs
// keeps the original ZLM paths for diagnostics, while the named fields make
// the management layer independent of ZLM's path spelling.
type CapabilityProfile struct {
	APIs          []string          `json:"apis"`
	GetStatistic  CapabilityState   `json:"getStatistic"`
	GetAllSession CapabilityState   `json:"getAllSession"`
	GetAPIList    CapabilityState   `json:"getApiList"`
	Reasons       map[string]string `json:"reasons,omitempty"`
	ProbedAt      time.Time         `json:"probedAt"`
}

// Status returns the state for a known API name or path. Unknown API names
// are treated as unsupported only after a successful API-list response has
// supplied a complete list; callers cannot accidentally turn an unavailable
// probe into an empty list.
func (p CapabilityProfile) Status(api string) CapabilityState {
	normalized := normalizeRuntimeAPIName(api)
	switch strings.ToLower(normalized) {
	case CapabilityGetStatistic:
		return capabilityStateOrUnknown(p.GetStatistic)
	case CapabilityGetAllSession:
		return capabilityStateOrUnknown(p.GetAllSession)
	case CapabilityGetAPIList:
		return capabilityStateOrUnknown(p.GetAPIList)
	default:
		for _, listed := range p.APIs {
			if strings.EqualFold(normalizeRuntimeAPIName(listed), normalized) {
				return CapabilitySupported
			}
		}
		if p.APIs != nil {
			return CapabilityUnsupported
		}
		return CapabilityUnknown
	}
}

func capabilityStateOrUnknown(state CapabilityState) CapabilityState {
	if state == "" {
		return CapabilityUnknown
	}
	return state
}

// Supports is a convenience predicate for capability-gated actions.
func (p CapabilityProfile) Supports(api string) bool {
	return p.Status(api) == CapabilitySupported
}

// RuntimeErrorKind is the stable classification for runtime-client failures.
type RuntimeErrorKind string

const (
	RuntimeErrorCapabilityUnsupported RuntimeErrorKind = "capability_unsupported"
	RuntimeErrorNodeFailure           RuntimeErrorKind = "node_failure"
	RuntimeErrorCapabilityUnknown     RuntimeErrorKind = "capability_unknown"
)

var (
	// ErrCapabilityUnsupported means that ZLM explicitly reported the target
	// API is unavailable. It is not returned for timeouts or malformed data.
	ErrCapabilityUnsupported = errors.New("zlm capability unsupported")
	// ErrNodeFailure covers transport, timeout, malformed-response and other
	// node-side failures for which capability cannot be established.
	ErrNodeFailure = errors.New("zlm node failure")
	// ErrCapabilityUnknown is used when a capability profile cannot be probed.
	ErrCapabilityUnknown = errors.New("zlm capability unknown")
)

// RuntimeError preserves a machine-readable classification while keeping the
// user-facing text redacted. Cause remains available to errors.Is/As callers.
type RuntimeError struct {
	API     string           `json:"api"`
	Kind    RuntimeErrorKind `json:"kind"`
	Code    int              `json:"code,omitempty"`
	Message string           `json:"message"`
	Cause   error            `json:"-"`
}

func (e *RuntimeError) Error() string {
	if e == nil {
		return "<nil>"
	}
	message := e.Message
	if message == "" && e.Cause != nil {
		message = e.Cause.Error()
	}
	if message == "" {
		message = string(e.Kind)
	}
	if e.API == "" {
		if e.Code != 0 {
			return fmt.Sprintf("ZLM %s (code=%d): %s", e.Kind, e.Code, message)
		}
		return fmt.Sprintf("ZLM %s: %s", e.Kind, message)
	}
	if e.Code != 0 {
		return fmt.Sprintf("ZLM %s (%s code=%d): %s", e.API, e.Kind, e.Code, message)
	}
	return fmt.Sprintf("ZLM %s (%s): %s", e.API, e.Kind, message)
}

func (e *RuntimeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *RuntimeError) Is(target error) bool {
	if e == nil {
		return target == nil
	}
	switch {
	case target == ErrCapabilityUnsupported:
		return e.Kind == RuntimeErrorCapabilityUnsupported
	case target == ErrNodeFailure:
		return e.Kind == RuntimeErrorNodeFailure
	case target == ErrCapabilityUnknown:
		return e.Kind == RuntimeErrorCapabilityUnknown
	}
	if e.Cause != nil {
		return errors.Is(e.Cause, target)
	}
	return false
}

var runtimeURLPattern = regexp.MustCompile(`(?i)(?:https?|rtmp|rtsp|srt|ws|wss)://[^\s"'<>]+`)

// redactRuntimeText removes credentials and query strings from URL-like
// fragments before an error is exposed to callers. c.call already masks the
// API secret; doing this second pass also covers custom transport errors and
// ZLM messages that include a source URL.
func (c *Client) redactRuntimeText(message string) string {
	if c == nil {
		return message
	}
	if c.secret != "" {
		message = strings.ReplaceAll(message, c.secret, "***")
		message = strings.ReplaceAll(message, url.QueryEscape(c.secret), "***")
		message = strings.ReplaceAll(message, url.PathEscape(c.secret), "***")
	}
	return runtimeURLPattern.ReplaceAllStringFunc(message, func(raw string) string {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Host == "" {
			return "[redacted-url]"
		}
		parsed.User = nil
		parsed.RawQuery = ""
		parsed.Fragment = ""
		return parsed.String()
	})
}

func (c *Client) runtimeNodeError(api string, cause error) error {
	if cause == nil {
		cause = ErrNodeFailure
	}
	return &RuntimeError{
		API:     api,
		Kind:    RuntimeErrorNodeFailure,
		Message: c.redactRuntimeText(cause.Error()),
		Cause:   cause,
	}
}

func (c *Client) runtimeUnknownError(api string, cause error) error {
	if cause == nil {
		cause = ErrCapabilityUnknown
	}
	return &RuntimeError{
		API:     api,
		Kind:    RuntimeErrorCapabilityUnknown,
		Message: c.redactRuntimeText(cause.Error()),
		Cause:   cause,
	}
}

func (c *Client) runtimeResponseError(api string, code int, message string) error {
	kind := RuntimeErrorNodeFailure
	if runtimeCapabilityMissing(code, message) {
		kind = RuntimeErrorCapabilityUnsupported
	}
	return &RuntimeError{
		API:     api,
		Kind:    kind,
		Code:    code,
		Message: c.redactRuntimeText(strings.TrimSpace(message)),
	}
}

func runtimeCapabilityMissing(code int, message string) bool {
	if code == 404 || code == -404 {
		return true
	}
	message = strings.ToLower(strings.TrimSpace(message))
	for _, phrase := range []string{
		"api not found",
		"api does not exist",
		"api doesn't exist",
		"no such api",
		"unknown api",
		"unsupported api",
		"api not support",
		"api unsupported",
	} {
		if strings.Contains(message, phrase) {
			return true
		}
	}
	return false
}

// GetStatistic retrieves ZLM's object-instance counters.
func (c *Client) GetStatistic(ctx context.Context) (Statistic, error) {
	var response struct {
		baseResp
		Data *Statistic `json:"data"`
	}
	if err := c.call(ctx, CapabilityGetStatistic, nil, &response); err != nil {
		return Statistic{}, c.runtimeNodeError(CapabilityGetStatistic, err)
	}
	if response.Code != 0 {
		return Statistic{}, c.runtimeResponseError(CapabilityGetStatistic, response.Code, response.Msg)
	}
	if response.Data == nil {
		return Statistic{}, c.runtimeNodeError(CapabilityGetStatistic, errors.New("响应缺少 data"))
	}
	return *response.Data, nil
}

// GetAllSessions retrieves network sessions, optionally filtered by local
// port and peer address. The filter is typed so arbitrary ZLM query keys cannot
// leak through this client API.
func (c *Client) GetAllSessions(ctx context.Context, filter SessionFilter) ([]Session, error) {
	params := make(map[string]string, 2)
	if filter.LocalPort != 0 {
		params["local_port"] = strconv.Itoa(filter.LocalPort)
	}
	if strings.TrimSpace(filter.PeerIP) != "" {
		params["peer_ip"] = filter.PeerIP
	}

	var response struct {
		baseResp
		Data *[]Session `json:"data"`
	}
	if err := c.call(ctx, CapabilityGetAllSession, params, &response); err != nil {
		return nil, c.runtimeNodeError(CapabilityGetAllSession, err)
	}
	if response.Code != 0 {
		return nil, c.runtimeResponseError(CapabilityGetAllSession, response.Code, response.Msg)
	}
	if response.Data == nil {
		return nil, c.runtimeNodeError(CapabilityGetAllSession, errors.New("响应缺少 data"))
	}
	return *response.Data, nil
}

// GetAllSession is a singular-name compatibility wrapper for code that follows
// ZLM's endpoint spelling. New code should prefer GetAllSessions.
func (c *Client) GetAllSession(ctx context.Context, filter SessionFilter) ([]Session, error) {
	return c.GetAllSessions(ctx, filter)
}

// GetAPIList retrieves the complete API path list advertised by ZLM.
func (c *Client) GetAPIList(ctx context.Context) ([]string, error) {
	var response struct {
		baseResp
		Data *[]string `json:"data"`
	}
	if err := c.call(ctx, CapabilityGetAPIList, nil, &response); err != nil {
		return nil, c.runtimeNodeError(CapabilityGetAPIList, err)
	}
	if response.Code != 0 {
		return nil, c.runtimeResponseError(CapabilityGetAPIList, response.Code, response.Msg)
	}
	if response.Data == nil {
		return nil, c.runtimeNodeError(CapabilityGetAPIList, errors.New("响应缺少 data"))
	}
	// Preserve a valid empty array as non-nil. CapabilityProfile uses nil to
	// mean "probe failed/unknown" and a non-nil empty slice to mean "probed,
	// but no APIs were advertised".
	apis := make([]string, len(*response.Data))
	for i, api := range *response.Data {
		apis[i] = c.redactRuntimeText(api)
	}
	return apis, nil
}

// GetApiList is a compatibility alias for callers using the endpoint's
// original camel-case spelling.
func (c *Client) GetApiList(ctx context.Context) ([]string, error) {
	return c.GetAPIList(ctx)
}

// GetCapabilityProfile builds a capability profile from getApiList. A failed
// probe returns an all-unknown profile plus the redacted probe error; callers
// must not treat the profile as an empty API list.
func (c *Client) GetCapabilityProfile(ctx context.Context) (CapabilityProfile, error) {
	profile := CapabilityProfile{
		GetStatistic:  CapabilityUnknown,
		GetAllSession: CapabilityUnknown,
		GetAPIList:    CapabilityUnknown,
		Reasons:       make(map[string]string, 3),
		ProbedAt:      time.Now(),
	}
	apis, err := c.GetAPIList(ctx)
	if err != nil {
		if errors.Is(err, ErrCapabilityUnsupported) {
			profile.GetAPIList = CapabilityUnsupported
		}
		unknownErr := c.runtimeUnknownError(CapabilityGetAPIList, err)
		reason := unknownErr.Error()
		profile.Reasons[CapabilityGetAPIList] = reason
		profile.Reasons[CapabilityGetStatistic] = reason
		profile.Reasons[CapabilityGetAllSession] = reason
		return profile, unknownErr
	}

	profile.APIs = make([]string, len(apis))
	copy(profile.APIs, apis)
	profile.GetAPIList = CapabilitySupported
	profile.GetStatistic = stateFromRuntimeAPIList(apis, CapabilityGetStatistic)
	profile.GetAllSession = stateFromRuntimeAPIList(apis, CapabilityGetAllSession)
	return profile, nil
}

// ProbeCapabilities is the shorter verb-oriented alias used by health and
// node-probing callers.
func (c *Client) ProbeCapabilities(ctx context.Context) (CapabilityProfile, error) {
	return c.GetCapabilityProfile(ctx)
}

func stateFromRuntimeAPIList(apis []string, target string) CapabilityState {
	for _, api := range apis {
		if strings.EqualFold(normalizeRuntimeAPIName(api), target) {
			return CapabilitySupported
		}
	}
	return CapabilityUnsupported
}

func normalizeRuntimeAPIName(api string) string {
	api = strings.TrimSpace(api)
	if space := strings.IndexByte(api, ' '); space >= 0 {
		api = strings.TrimSpace(api[space+1:])
	}
	api = strings.Trim(api, "/")
	apiLower := strings.ToLower(api)
	if strings.HasPrefix(apiLower, "index/api/") {
		api = api[len("index/api/"):]
	}
	if slash := strings.LastIndexByte(api, '/'); slash >= 0 {
		api = api[slash+1:]
	}
	return api
}
