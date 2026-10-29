package recording

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbplayback "uvplatform.com/uvp-gb28181/app/gb28181/playback"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/gb28181/recordcache"
	"uvplatform.com/uvp-gb28181/app/gb28181/recordquery"
	"uvplatform.com/uvp-gb28181/app/openapi/adapters"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

const (
	ScopeRecordQuery         = "device:record-query"
	ScopeRecordQueryOptions  = "device:record-query:options"
	ScopePlaybackCreate      = "device:playback-create"
	ScopePlaybackRead        = "device:playback-read"
	ScopePlaybackAction      = "device:playback-action"
	ScopePlaybackStop        = "device:playback-stop"
	ScopeDownloadCreate      = "device:download-create"
	ScopeDownloadRead        = "device:download-read"
	ScopeDownloadCancel      = "device:download-cancel"
	RecordQueryAdapterKey    = "device.record-query.v1"
	RecordQueryOptionsKey    = "device.record-query-options.v1"
	PlaybackCreateAdapterKey = "device.playback-create.v1"
	PlaybackReadAdapterKey   = "device.playback-read.v1"
	PlaybackActionAdapterKey = "device.playback-action.v1"
	PlaybackStopAdapterKey   = "device.playback-stop.v1"
	DownloadCreateAdapterKey = "device.download-create.v1"
	DownloadReadAdapterKey   = "device.download-read.v1"
	DownloadCancelAdapterKey = "device.download-cancel.v1"
)

type Target struct {
	Authorization    gbplayback.AuthorizationSnapshot
	DeviceCode       string
	ChannelCode      string
	DeviceName       string
	ChannelName      string
	DeviceIP         string
	DevicePort       int
	Transport        string
	StreamTransport  string
	PreferredNodeID  int64
	OwnerDeptID      uint
	DeviceOnline     bool
	ChannelOnline    bool
	EffectiveVersion string
}

type TargetResolver interface {
	Resolve(context.Context, resource.DepartmentScope, string, string) (Target, error)
}

type RecordQuery interface {
	Query(context.Context, recordquery.QueryRequest) (recordquery.QueryResult, error)
}

type PlaybackService interface {
	Create(context.Context, gbplayback.CreateRequest) (gbplayback.CreateResult, error)
	PeekForOwner(string, string) (*gbplayback.Session, bool)
	GetForOwner(string, string) (*gbplayback.Session, bool)
	Action(context.Context, string, string, gbplayback.ActionRequest) (*gbplayback.Session, error)
	StopForOwner(context.Context, string, string, string) error
}

type Config struct {
	Location        *time.Location
	MaxRangeHours   int
	MaxPageSize     int
	MaxCursors      int
	CursorTTL       time.Duration
	DefaultProtocol string
	Secure          bool
	Now             func() time.Time
}

type Request struct {
	ClientID       int64
	IdempotencyKey string
	ResourceScope  resource.DepartmentScope
	DeviceID       string
	ChannelID      string
	SessionID      string
	Body           []byte
	Query          map[string]string
}

type RecordItem struct {
	RecordKey  string    `json:"recordKey"`
	DeviceID   string    `json:"deviceId"`
	Name       string    `json:"name,omitempty"`
	StartTime  time.Time `json:"startTime"`
	EndTime    time.Time `json:"endTime"`
	Secrecy    int       `json:"secrecy"`
	Type       string    `json:"type,omitempty"`
	RecorderID string    `json:"recorderId,omitempty"`
	FileSize   *int64    `json:"fileSize,omitempty"`
}

type RecordPage struct {
	Items           []RecordItem `json:"items"`
	TotalRecords    int          `json:"totalRecords"`
	PageSize        int          `json:"pageSize"`
	NextCursor      string       `json:"nextCursor,omitempty"`
	ProtocolVersion string       `json:"protocolVersion"`
	Status          string       `json:"status"`
	Incomplete      bool         `json:"incomplete"`
	DeclaredTotal   int          `json:"declaredTotal"`
	ReceivedCount   int          `json:"receivedCount"`
	StartedAt       time.Time    `json:"startedAt"`
	FinishedAt      time.Time    `json:"finishedAt"`
}

type PlaybackView struct {
	SessionID       string            `json:"sessionId"`
	DeviceID        string            `json:"deviceId"`
	ChannelID       string            `json:"channelId"`
	Status          string            `json:"status"`
	Mode            string            `json:"mode"`
	ProtocolVersion string            `json:"protocolVersion"`
	Protocol        string            `json:"protocol,omitempty"`
	URL             string            `json:"url,omitempty"`
	MediaURLs       map[string]string `json:"mediaUrls,omitempty"`
	HasAudio        bool              `json:"hasAudio"`
	StartTime       time.Time         `json:"startTime"`
	EndTime         time.Time         `json:"endTime"`
	PlayFrom        time.Time         `json:"playFrom"`
	PositionSeconds float64           `json:"positionSeconds"`
	Scale           float64           `json:"scale"`
	CreatedAt       time.Time         `json:"createdAt"`
	Deadline        time.Time         `json:"deadline"`
	Existing        bool              `json:"existing,omitempty"`
}

type Dispatcher struct {
	targets   TargetResolver
	queries   RecordQuery
	snapshots SnapshotResolver
	playback  PlaybackService
	config    Config
	cursorMu  sync.Mutex
	cursors   map[string]recordCursor
}

func NewDispatcher(targets TargetResolver, queries RecordQuery, snapshots SnapshotResolver, playback PlaybackService, config Config) *Dispatcher {
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.MaxPageSize <= 0 {
		config.MaxPageSize = 100
	}
	if config.MaxCursors <= 0 {
		config.MaxCursors = 512
	}
	if config.CursorTTL <= 0 {
		config.CursorTTL = 5 * time.Minute
	}
	return &Dispatcher{targets: targets, queries: queries, snapshots: snapshots, playback: playback, config: config, cursors: make(map[string]recordCursor)}
}

type SnapshotResolver interface {
	Resolve(recordquery.ResolveRequest) (recordquery.Snapshot, error)
}

func NewGormTargetResolver(db *gorm.DB) TargetResolver {
	return gormTargetResolver{db: db}
}

type gormTargetResolver struct{ db *gorm.DB }

func (r gormTargetResolver) Resolve(ctx context.Context, scope resource.DepartmentScope, deviceID, channelID string) (Target, error) {
	if r.db == nil {
		return Target{}, adapters.ErrUnavailable
	}
	channel, err := resource.New(r.db).GetChannelInScope(ctx, scope, deviceID, channelID)
	if err != nil {
		if errors.Is(err, resource.ErrResourceNotFound) {
			return Target{}, adapters.ErrNotFound
		}
		return Target{}, adapters.ErrUnavailable
	}
	departmentIDs, err := resource.ResolveDepartmentIDs(ctx, r.db, scope)
	if err != nil {
		if errors.Is(err, resource.ErrInvalidDepartmentScope) {
			return Target{}, adapters.ErrInvalidRequest
		}
		return Target{}, adapters.ErrUnavailable
	}
	channelPK := channelIDPK(ctx, r.db, channel, deviceID, departmentIDs)
	if channelPK == 0 {
		return Target{}, adapters.ErrNotFound
	}
	target, err := recordcache.NewGormRepo(r.db).LoadTarget(ctx, channelPK, deviceID)
	if err != nil {
		if errors.Is(err, recordcache.ErrTaskNotFound) {
			return Target{}, adapters.ErrNotFound
		}
		return Target{}, adapters.ErrUnavailable
	}
	allowed := false
	for _, deptID := range departmentIDs {
		if deptID == target.OwnerDeptID {
			allowed = true
			break
		}
	}
	if !allowed || target.OwnerDeptID == 0 || target.DeviceCode != deviceID || target.ChannelCode != channelID || target.ChannelName != channel.Name {
		return Target{}, adapters.ErrNotFound
	}
	var versions []struct{ EffectiveVersion string }
	versionResult := r.db.WithContext(ctx).Table("gb_device").Select("effective_version").Where("device_id = ? AND owner_dept_id = ? AND deleted_at IS NULL", deviceID, target.OwnerDeptID).Limit(2).Find(&versions)
	if versionResult.Error != nil || len(versions) != 1 {
		return Target{}, adapters.ErrUnavailable
	}
	return Target{
		Authorization: target.Authorization(), DeviceCode: target.DeviceCode, ChannelCode: target.ChannelCode,
		DeviceName: target.DeviceName, ChannelName: target.ChannelName, DeviceIP: target.DeviceIP, DevicePort: target.DevicePort,
		Transport: target.DeviceTransport, StreamTransport: target.StreamTransport, PreferredNodeID: target.PreferredNodeID,
		OwnerDeptID: target.OwnerDeptID, DeviceOnline: target.DeviceStatus == 1, ChannelOnline: target.ChannelStatus == 1, EffectiveVersion: versions[0].EffectiveVersion,
	}, nil
}

// channelIDPK re-reads the scoped channel's internal key only after its external
// identifier pair has been validated by the resource boundary.
func channelIDPK(ctx context.Context, db *gorm.DB, channel resource.Channel, deviceID string, departmentIDs []uint) uint {
	var row struct{ ID uint }
	result := db.WithContext(ctx).Table("gb_channel").Select("id").Where("device_id = ? AND channel_id = ? AND owner_dept_id IN ? AND deleted_at IS NULL", deviceID, channel.ChannelID, departmentIDs).Limit(2).Find(&row)
	if result.Error != nil || result.RowsAffected != 1 {
		return 0
	}
	return row.ID
}

func (d *Dispatcher) Handle(ctx context.Context, scope string, request Request) (any, error) {
	if d == nil || d.targets == nil || request.ClientID <= 0 || !validScope(request.ResourceScope) {
		return nil, adapters.ErrInvalidRequest
	}
	switch scope {
	case ScopeRecordQuery:
		return d.search(ctx, request)
	case ScopeRecordQueryOptions:
		return d.recordQueryOptions(ctx, request)
	case ScopePlaybackCreate:
		return d.createPlayback(ctx, request)
	case ScopePlaybackRead:
		return d.readPlayback(ctx, request)
	case ScopePlaybackAction:
		return d.actionPlayback(ctx, request)
	case ScopePlaybackStop:
		return d.stopPlayback(ctx, request)
	case ScopeDownloadCreate:
		return d.createDownload(ctx, request)
	case ScopeDownloadRead:
		return d.readDownload(ctx, request)
	case ScopeDownloadCancel:
		return d.cancelDownload(ctx, request)
	default:
		return nil, adapters.ErrUnavailable
	}
}

// RecordQueryOptions describes how a caller must shape a device record search:
// which clock and timezone the device answers in, how wide a window it accepts,
// and which record types it can report. It is read-only and never opens a SIP
// session, so it works for offline channels too (Online reports that state).
type RecordQueryOptions struct {
	Device         RecordQueryOptionDevice  `json:"device"`
	Channel        RecordQueryOptionChannel `json:"channel"`
	Timezone       string                   `json:"timezone"`
	ServerNow      string                   `json:"serverNow"`
	MaxRangeHours  int                      `json:"maxRangeHours"`
	SupportedTypes []string                 `json:"supportedTypes"`
}

type RecordQueryOptionDevice struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Online bool   `json:"online"`
}

type RecordQueryOptionChannel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// recordQueryOptions resolves the caller's scope and reports the accepted
// search window. Deliberately no device-offline rejection: knowing the window
// before going online is what lets a caller time its retry.
func (d *Dispatcher) recordQueryOptions(ctx context.Context, request Request) (RecordQueryOptions, error) {
	if d.targets == nil || d.config.Location == nil || d.config.MaxRangeHours <= 0 {
		return RecordQueryOptions{}, adapters.ErrUnavailable
	}
	if len(request.Body) != 0 || len(request.Query) != 0 {
		return RecordQueryOptions{}, adapters.ErrInvalidRequest
	}
	target, err := d.resolveTarget(ctx, request)
	if err != nil {
		return RecordQueryOptions{}, err
	}
	return RecordQueryOptions{
		Device:         RecordQueryOptionDevice{ID: target.DeviceCode, Name: target.DeviceName, Online: target.DeviceOnline && target.ChannelOnline},
		Channel:        RecordQueryOptionChannel{ID: target.ChannelCode, Name: target.ChannelName},
		Timezone:       d.config.Location.String(),
		ServerNow:      d.config.Now().In(d.config.Location).Format(time.RFC3339),
		MaxRangeHours:  d.config.MaxRangeHours,
		SupportedTypes: []string{"all", "manual", "alarm"},
	}, nil
}

func (d *Dispatcher) search(ctx context.Context, request Request) (RecordPage, error) {
	if d.queries == nil || d.config.Location == nil || d.config.MaxRangeHours <= 0 {
		return RecordPage{}, adapters.ErrUnavailable
	}
	cursor, requestedPageSize, hasCursor, queryErr := parsePageQuery(request.Query, d.config.MaxPageSize)
	if queryErr != nil {
		return RecordPage{}, adapters.ErrInvalidRequest
	}
	if hasCursor {
		if len(request.Body) != 0 {
			return RecordPage{}, adapters.ErrInvalidRequest
		}
		return d.continueSearch(ctx, request, cursor, requestedPageSize)
	}
	var input recordQueryInput
	if err := decodeJSON(request.Body, &input); err != nil {
		return RecordPage{}, adapters.ErrInvalidRequest
	}
	pageSize := requestedPageSize
	if pageSize == 0 {
		pageSize = d.config.MaxPageSize
	}
	if pageSize < 1 || pageSize > d.config.MaxPageSize {
		return RecordPage{}, adapters.ErrInvalidRequest
	}
	start, err := time.Parse(time.RFC3339Nano, input.StartTime)
	if err != nil {
		return RecordPage{}, adapters.ErrInvalidRequest
	}
	end, err := time.Parse(time.RFC3339Nano, input.EndTime)
	// 同 HTTP 接口策略：跨度只作为常规值展示，不在这里拒绝。
	if err != nil || !end.After(start) {
		return RecordPage{}, adapters.ErrInvalidRequest
	}
	target, err := d.resolveTarget(ctx, request)
	if err != nil {
		return RecordPage{}, err
	}
	if !target.DeviceOnline || !target.ChannelOnline {
		return RecordPage{}, adapters.ErrDeviceOffline
	}
	if target.EffectiveVersion != "2016" && target.EffectiveVersion != "2022" {
		return RecordPage{}, adapters.ErrCapabilityNotSupported
	}
	destination, transport, err := targetEndpoint(target)
	if err != nil {
		return RecordPage{}, err
	}
	d.invalidateCursors(request.ClientID, uint(target.Authorization.ChannelPK))
	result, err := d.queries.Query(ctx, recordquery.QueryRequest{
		OwnerClientID: request.ClientID, ChannelID: uint(target.Authorization.ChannelPK),
		DeviceCode: target.DeviceCode, ChannelCode: target.ChannelCode,
		Destination: destination, Transport: transport,
		ProtocolVersion: protocol.Version(target.EffectiveVersion),
		StartTime:       start.In(d.config.Location), EndTime: end.In(d.config.Location), Type: manscdpType(input.Type), Secrecy: input.Secrecy, RecorderID: input.RecorderID,
	})
	if err != nil {
		return RecordPage{}, mapQueryError(err)
	}
	items := make([]RecordItem, 0, len(result.Records))
	for _, record := range result.Records {
		startTime, startErr := recordquery.ParseWallClock(record.StartTime, d.config.Location)
		endTime, endErr := recordquery.ParseWallClock(record.EndTime, d.config.Location)
		if startErr != nil || endErr != nil || !endTime.After(startTime) || record.RecordKey == "" {
			continue
		}
		items = append(items, RecordItem{
			RecordKey: record.RecordKey, DeviceID: target.ChannelCode, Name: record.Name,
			StartTime: startTime, EndTime: endTime, Secrecy: record.Secrecy, Type: string(record.Type),
			RecorderID: record.RecorderID, FileSize: record.FileSize,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].StartTime.Equal(items[j].StartTime) {
			return items[i].StartTime.Before(items[j].StartTime)
		}
		if !items[i].EndTime.Equal(items[j].EndTime) {
			return items[i].EndTime.Before(items[j].EndTime)
		}
		return items[i].RecordKey < items[j].RecordKey
	})
	page := RecordPage{TotalRecords: len(items), PageSize: pageSize, ProtocolVersion: target.EffectiveVersion, Status: string(result.Status), Incomplete: result.Incomplete,
		DeclaredTotal: result.DeclaredTotal, ReceivedCount: len(items), StartedAt: result.StartedAt, FinishedAt: result.FinishedAt}
	if len(items) > pageSize {
		checker, ok := d.queries.(queryGenerationChecker)
		if !ok || result.QueryID == "" || !checker.IsClientQueryCurrent(request.ClientID, uint(target.Authorization.ChannelPK), result.QueryID) {
			return RecordPage{}, adapters.ErrUnavailable
		}
		state := recordCursor{
			clientID: request.ClientID, resourceScope: request.ResourceScope, deviceID: target.DeviceCode, channelID: target.ChannelCode,
			channelPK: uint(target.Authorization.ChannelPK), ownerDeptID: target.OwnerDeptID, authorization: target.Authorization,
			queryID: result.QueryID, items: items, page: page, offset: pageSize, pageSize: pageSize,
			expiresAt: d.config.Now().Add(d.config.CursorTTL),
		}
		page.NextCursor, err = d.storeCursor(state)
		if err != nil {
			return RecordPage{}, adapters.ErrUnavailable
		}
	}
	page.Items = cloneRecordItems(items[:minInt(len(items), pageSize)])
	return page, nil
}

type queryGenerationChecker interface {
	IsClientQueryCurrent(int64, uint, string) bool
}

type recordCursor struct {
	clientID      int64
	resourceScope resource.DepartmentScope
	deviceID      string
	channelID     string
	channelPK     uint
	ownerDeptID   uint
	authorization gbplayback.AuthorizationSnapshot
	queryID       string
	items         []RecordItem
	page          RecordPage
	offset        int
	pageSize      int
	expiresAt     time.Time
}

func (d *Dispatcher) continueSearch(ctx context.Context, request Request, cursor string, pageSize int) (RecordPage, error) {
	if cursor == "" || pageSize < 0 || pageSize > d.config.MaxPageSize {
		return RecordPage{}, adapters.ErrInvalidRequest
	}
	state, ok := d.lookupCursor(cursor)
	if !ok || state.clientID != request.ClientID || state.deviceID != request.DeviceID || state.channelID != request.ChannelID || state.resourceScope != request.ResourceScope {
		return RecordPage{}, adapters.ErrNotFound
	}
	if pageSize != 0 && pageSize != state.pageSize {
		return RecordPage{}, adapters.ErrInvalidRequest
	}
	target, err := d.resolveTarget(ctx, request)
	if err != nil {
		return RecordPage{}, err
	}
	if target.Authorization != state.authorization || target.OwnerDeptID != state.ownerDeptID ||
		target.DeviceCode != state.deviceID || target.ChannelCode != state.channelID || target.Authorization.ChannelPK != int64(state.channelPK) {
		return RecordPage{}, adapters.ErrNotFound
	}
	checker, ok := d.queries.(queryGenerationChecker)
	if !ok || !checker.IsClientQueryCurrent(state.clientID, state.channelPK, state.queryID) {
		return RecordPage{}, adapters.ErrNotFound
	}
	if !d.consumeCursor(cursor) {
		return RecordPage{}, adapters.ErrNotFound
	}
	page := state.page
	end := minInt(len(state.items), state.offset+state.pageSize)
	page.Items = cloneRecordItems(state.items[state.offset:end])
	page.NextCursor = ""
	if end < len(state.items) {
		state.offset = end
		state.expiresAt = d.config.Now().Add(d.config.CursorTTL)
		page.NextCursor, err = d.storeCursor(state)
		if err != nil {
			return RecordPage{}, adapters.ErrUnavailable
		}
	}
	return page, nil
}

func (d *Dispatcher) storeCursor(state recordCursor) (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	d.cursorMu.Lock()
	d.pruneCursorsLocked(d.config.Now())
	for len(d.cursors) >= d.config.MaxCursors {
		var oldest string
		var expires time.Time
		first := true
		for key, candidate := range d.cursors {
			if first || candidate.expiresAt.Before(expires) {
				oldest, expires, first = key, candidate.expiresAt, false
			}
		}
		if first {
			break
		}
		delete(d.cursors, oldest)
	}
	d.cursors[token] = state
	d.cursorMu.Unlock()
	return token, nil
}

func (d *Dispatcher) lookupCursor(token string) (recordCursor, bool) {
	d.cursorMu.Lock()
	defer d.cursorMu.Unlock()
	d.pruneCursorsLocked(d.config.Now())
	state, ok := d.cursors[token]
	return state, ok
}

func (d *Dispatcher) consumeCursor(token string) bool {
	d.cursorMu.Lock()
	defer d.cursorMu.Unlock()
	state, ok := d.cursors[token]
	if !ok || !d.config.Now().Before(state.expiresAt) {
		delete(d.cursors, token)
		return false
	}
	delete(d.cursors, token)
	return true
}

func (d *Dispatcher) invalidateCursors(clientID int64, channelPK uint) {
	d.cursorMu.Lock()
	for token, state := range d.cursors {
		if state.clientID == clientID && state.channelPK == channelPK {
			delete(d.cursors, token)
		}
	}
	d.cursorMu.Unlock()
}

func (d *Dispatcher) pruneCursorsLocked(now time.Time) {
	for token, state := range d.cursors {
		if !now.Before(state.expiresAt) {
			delete(d.cursors, token)
		}
	}
}

func cloneRecordItems(items []RecordItem) []RecordItem {
	result := make([]RecordItem, len(items))
	copy(result, items)
	return result
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func parsePageQuery(query map[string]string, maxPageSize int) (cursor string, pageSize int, hasCursor bool, err error) {
	for key := range query {
		if key != "cursor" && key != "pageSize" {
			return "", 0, false, errors.New("unsupported query parameter")
		}
	}
	if value, exists := query["cursor"]; exists {
		cursor = strings.TrimSpace(value)
		if cursor == "" {
			return "", 0, false, errors.New("empty cursor")
		}
		hasCursor = true
	}
	if value, exists := query["pageSize"]; exists {
		if value == "" {
			return "", 0, false, errors.New("empty page size")
		}
		pageSize, err = strconv.Atoi(value)
		if err != nil || pageSize < 1 || pageSize > maxPageSize {
			return "", 0, false, errors.New("invalid page size")
		}
	}
	return cursor, pageSize, hasCursor, nil
}

func (d *Dispatcher) createPlayback(ctx context.Context, request Request) (any, error) {
	if d.playback == nil || d.snapshots == nil {
		return nil, adapters.ErrUnavailable
	}
	if strings.TrimSpace(request.IdempotencyKey) == "" {
		return nil, adapters.ErrInvalidRequest
	}
	var input playbackCreateInput
	if err := decodeJSON(request.Body, &input); err != nil {
		return nil, adapters.ErrInvalidRequest
	}
	playFrom, err := time.Parse(time.RFC3339Nano, input.PlayFrom)
	if err != nil || strings.TrimSpace(input.RecordKey) == "" {
		return nil, adapters.ErrInvalidRequest
	}
	target, err := d.resolveTarget(ctx, request)
	if err != nil {
		return nil, err
	}
	if !target.DeviceOnline || !target.ChannelOnline {
		return nil, adapters.ErrDeviceOffline
	}
	if target.EffectiveVersion != "2016" && target.EffectiveVersion != "2022" {
		return nil, adapters.ErrCapabilityNotSupported
	}
	destination, transport, err := targetEndpoint(target)
	if err != nil {
		return nil, err
	}
	snapshot, err := d.snapshots.Resolve(recordquery.ResolveRequest{
		RecordKey: input.RecordKey, OwnerClientID: request.ClientID, ChannelID: uint(target.Authorization.ChannelPK), PlayFrom: playFrom,
	})
	if err != nil {
		return nil, mapQueryError(err)
	}
	if snapshot.DeviceCode != target.DeviceCode || snapshot.ChannelCode != target.ChannelCode {
		return nil, adapters.ErrNotFound
	}
	created, err := d.playback.Create(ctx, gbplayback.CreateRequest{
		Authorization: target.Authorization, OwnerID: clientOwner(request.ClientID), DeviceID: target.DeviceCode,
		ChannelID: strconv.FormatInt(target.Authorization.ChannelPK, 10), SIPChannelID: target.ChannelCode,
		RecordKey: snapshot.RecordKey, IdempotencyKey: request.IdempotencyKey,
		Destination: destination, Transport: transport,
		PreferredNodeID: target.PreferredNodeID, SegmentStart: snapshot.SegmentStart, SegmentEnd: snapshot.SegmentEnd,
		PlayFrom: playFrom, Mode: gbplayback.ModePlayback, TCPMode: strings.Contains(strings.ToUpper(target.StreamTransport), "TCP"),
		DefaultProtocol: d.config.DefaultProtocol, Secure: d.config.Secure, Now: d.config.Now(),
	})
	if err != nil {
		return nil, mapPlaybackError(err)
	}
	if created.Session == nil || !sessionMatches(created.Session, request.ClientID, target) {
		return nil, adapters.ErrUnavailable
	}
	return playbackView(created.Session, target.EffectiveVersion, true), nil
}

func (d *Dispatcher) createDownload(ctx context.Context, request Request) (any, error) {
	if d.playback == nil || d.snapshots == nil || strings.TrimSpace(request.IdempotencyKey) == "" {
		return nil, adapters.ErrInvalidRequest
	}
	var input playbackDownloadInput
	if err := decodeJSON(request.Body, &input); err != nil {
		return nil, adapters.ErrInvalidRequest
	}
	playFrom, err := time.Parse(time.RFC3339Nano, input.PlayFrom)
	if err != nil || strings.TrimSpace(input.RecordKey) == "" || !validDownloadSpeed(input.DownloadSpeed) {
		return nil, adapters.ErrInvalidRequest
	}
	target, err := d.resolveTarget(ctx, request)
	if err != nil {
		return nil, err
	}
	if !target.DeviceOnline || !target.ChannelOnline {
		return nil, adapters.ErrDeviceOffline
	}
	if target.EffectiveVersion != "2016" && target.EffectiveVersion != "2022" {
		return nil, adapters.ErrCapabilityNotSupported
	}
	destination, transport, err := targetEndpoint(target)
	if err != nil {
		return nil, err
	}
	snapshot, err := d.snapshots.Resolve(recordquery.ResolveRequest{RecordKey: input.RecordKey, OwnerClientID: request.ClientID, ChannelID: uint(target.Authorization.ChannelPK), PlayFrom: playFrom})
	if err != nil {
		return nil, mapQueryError(err)
	}
	if snapshot.DeviceCode != target.DeviceCode || snapshot.ChannelCode != target.ChannelCode {
		return nil, adapters.ErrNotFound
	}
	created, err := d.playback.Create(ctx, gbplayback.CreateRequest{
		Authorization: target.Authorization, OwnerID: clientOwner(request.ClientID), DeviceID: target.DeviceCode,
		ChannelID: strconv.FormatInt(target.Authorization.ChannelPK, 10), SIPChannelID: target.ChannelCode,
		RecordKey: snapshot.RecordKey, IdempotencyKey: request.IdempotencyKey, Destination: destination, Transport: transport,
		PreferredNodeID: target.PreferredNodeID, SegmentStart: snapshot.SegmentStart, SegmentEnd: snapshot.SegmentEnd, PlayFrom: playFrom,
		Mode: gbplayback.ModeDownload, DownloadSpeed: input.DownloadSpeed, TCPMode: strings.Contains(strings.ToUpper(target.StreamTransport), "TCP"),
		DefaultProtocol: d.config.DefaultProtocol, Secure: d.config.Secure, Now: d.config.Now(),
	})
	if err != nil {
		return nil, mapPlaybackError(err)
	}
	if created.Session == nil || !sessionMatches(created.Session, request.ClientID, target) {
		return nil, adapters.ErrUnavailable
	}
	return playbackView(created.Session, target.EffectiveVersion, created.Existing), nil
}

func (d *Dispatcher) readDownload(ctx context.Context, request Request) (any, error) {
	if d.playback == nil || strings.TrimSpace(request.SessionID) == "" {
		return nil, adapters.ErrInvalidRequest
	}
	target, err := d.resolveTarget(ctx, request)
	if err != nil {
		return nil, err
	}
	session, ok := d.playback.GetForOwner(request.SessionID, clientOwner(request.ClientID))
	if !ok || session == nil || session.Mode != gbplayback.ModeDownload || !sessionMatches(session, request.ClientID, target) {
		return nil, adapters.ErrNotFound
	}
	return playbackView(session, target.EffectiveVersion, false), nil
}

func (d *Dispatcher) cancelDownload(ctx context.Context, request Request) (any, error) {
	if d.playback == nil || strings.TrimSpace(request.SessionID) == "" || len(request.Body) != 0 {
		return nil, adapters.ErrInvalidRequest
	}
	target, err := d.resolveTarget(ctx, request)
	if err != nil {
		return nil, err
	}
	session, ok := d.playback.PeekForOwner(request.SessionID, clientOwner(request.ClientID))
	if !ok || session == nil || session.Mode != gbplayback.ModeDownload || !sessionMatches(session, request.ClientID, target) {
		return nil, adapters.ErrNotFound
	}
	if err := d.playback.StopForOwner(ctx, request.SessionID, clientOwner(request.ClientID), "openapi-client-download-cancel"); err != nil {
		return nil, mapPlaybackError(err)
	}
	return map[string]any{"sessionId": request.SessionID, "status": "stopped"}, nil
}

func (d *Dispatcher) readPlayback(ctx context.Context, request Request) (any, error) {
	if d.playback == nil {
		return nil, adapters.ErrUnavailable
	}
	if strings.TrimSpace(request.SessionID) == "" {
		return nil, adapters.ErrInvalidRequest
	}
	target, err := d.resolveTarget(ctx, request)
	if err != nil {
		return nil, err
	}
	session, ok := d.playback.PeekForOwner(request.SessionID, clientOwner(request.ClientID))
	if !ok || session == nil {
		return nil, adapters.ErrNotFound
	}
	if !sessionMatches(session, request.ClientID, target) {
		return nil, adapters.ErrNotFound
	}
	session, ok = d.playback.GetForOwner(request.SessionID, clientOwner(request.ClientID))
	if !ok || session == nil || !sessionMatches(session, request.ClientID, target) {
		return nil, adapters.ErrNotFound
	}
	return playbackView(session, target.EffectiveVersion, false), nil
}

func (d *Dispatcher) actionPlayback(ctx context.Context, request Request) (any, error) {
	if d.playback == nil {
		return nil, adapters.ErrUnavailable
	}
	if strings.TrimSpace(request.SessionID) == "" {
		return nil, adapters.ErrInvalidRequest
	}
	var input playbackActionInput
	if err := decodeJSON(request.Body, &input); err != nil {
		return nil, adapters.ErrInvalidRequest
	}
	session, target, err := d.authorizedSession(ctx, request)
	if err != nil {
		return nil, err
	}
	if !sessionMatches(session, request.ClientID, target) {
		return nil, adapters.ErrNotFound
	}
	updated, err := d.playback.Action(ctx, request.SessionID, clientOwner(request.ClientID), gbplayback.ActionRequest{
		Action: input.Action, PositionSeconds: input.PositionSeconds, Scale: input.Scale,
	})
	if err != nil {
		return nil, mapPlaybackError(err)
	}
	if updated == nil || !sessionMatches(updated, request.ClientID, target) {
		return nil, adapters.ErrUnavailable
	}
	return playbackView(updated, target.EffectiveVersion, false), nil
}

func (d *Dispatcher) stopPlayback(ctx context.Context, request Request) (any, error) {
	if d.playback == nil {
		return nil, adapters.ErrUnavailable
	}
	if strings.TrimSpace(request.SessionID) == "" || len(request.Body) != 0 {
		return nil, adapters.ErrInvalidRequest
	}
	session, target, err := d.authorizedSession(ctx, request)
	if err != nil {
		return nil, err
	}
	if !sessionMatches(session, request.ClientID, target) {
		return nil, adapters.ErrNotFound
	}
	if err := d.playback.StopForOwner(ctx, request.SessionID, clientOwner(request.ClientID), "openapi-client-stop"); err != nil {
		return nil, mapPlaybackError(err)
	}
	return map[string]any{"sessionId": request.SessionID, "status": "stopped"}, nil
}

func (d *Dispatcher) authorizedSession(ctx context.Context, request Request) (*gbplayback.Session, Target, error) {
	owner := clientOwner(request.ClientID)
	session, ok := d.playback.PeekForOwner(request.SessionID, owner)
	if !ok || session == nil {
		return nil, Target{}, adapters.ErrNotFound
	}
	target, err := d.resolveTarget(ctx, request)
	if err != nil {
		return nil, Target{}, err
	}
	if !sessionMatches(session, request.ClientID, target) {
		return nil, Target{}, adapters.ErrNotFound
	}
	return session, target, nil
}

func (d *Dispatcher) resolveTarget(ctx context.Context, request Request) (Target, error) {
	if strings.TrimSpace(request.DeviceID) == "" || strings.TrimSpace(request.ChannelID) == "" {
		return Target{}, adapters.ErrNotFound
	}
	target, err := d.targets.Resolve(ctx, request.ResourceScope, request.DeviceID, request.ChannelID)
	if err != nil {
		if errors.Is(err, adapters.ErrNotFound) || errors.Is(err, resource.ErrResourceNotFound) {
			return Target{}, adapters.ErrNotFound
		}
		if errors.Is(err, adapters.ErrInvalidRequest) || errors.Is(err, resource.ErrInvalidDepartmentScope) {
			return Target{}, adapters.ErrInvalidRequest
		}
		return Target{}, adapters.ErrUnavailable
	}
	if target.Authorization.DevicePK <= 0 || target.Authorization.ChannelPK <= 0 || target.Authorization.DeviceEpoch <= 0 ||
		target.Authorization.CleanupCompletedEpoch != target.Authorization.DeviceEpoch || target.DeviceCode != request.DeviceID || target.ChannelCode != request.ChannelID {
		return Target{}, adapters.ErrNotFound
	}
	return target, nil
}

func sessionMatches(session *gbplayback.Session, clientID int64, target Target) bool {
	return session != nil && session.OwnerID == clientOwner(clientID) && session.DeviceID == target.DeviceCode &&
		session.ChannelID == strconv.FormatInt(target.Authorization.ChannelPK, 10) && session.Authorization == target.Authorization
}

func clientOwner(clientID int64) string { return "openapi-client:" + strconv.FormatInt(clientID, 10) }

func targetEndpoint(target Target) (string, string, error) {
	host := strings.TrimSpace(target.DeviceIP)
	if host == "" || target.DevicePort <= 0 || target.DevicePort > 65535 {
		return "", "", adapters.ErrUnavailable
	}
	transport := strings.ToUpper(strings.TrimSpace(target.Transport))
	if transport == "" {
		transport = "UDP"
	}
	if transport != "UDP" && transport != "TCP" {
		return "", "", adapters.ErrUnavailable
	}
	return net.JoinHostPort(host, strconv.Itoa(target.DevicePort)), transport, nil
}

func playbackView(session *gbplayback.Session, protocolVersion string, existing bool) PlaybackView {
	return PlaybackView{
		SessionID: session.ID, DeviceID: session.DeviceID, ChannelID: session.Authorization.ChannelCode,
		Status: string(session.State), Mode: string(session.Mode), ProtocolVersion: protocolVersion, Protocol: session.Protocol, URL: session.URL,
		MediaURLs: session.MediaURLs, HasAudio: session.HasAudio, StartTime: session.SegmentStart, EndTime: session.SegmentEnd,
		PlayFrom: session.PlayFrom, PositionSeconds: session.PositionSeconds, Scale: session.Scale,
		CreatedAt: session.CreatedAt, Deadline: session.Deadline, Existing: existing,
	}
}

func mapQueryError(err error) error {
	switch {
	case errors.Is(err, recordquery.ErrInvalidArgument), errors.Is(err, recordquery.ErrSnapshotBoundary):
		return adapters.ErrInvalidRequest
	case errors.Is(err, recordquery.ErrSnapshotNotFound), errors.Is(err, recordquery.ErrSnapshotExpired), errors.Is(err, recordquery.ErrSnapshotTampered):
		return adapters.ErrNotFound
	case errors.Is(err, recordquery.ErrBusy):
		return adapters.ErrConflict
	default:
		return adapters.ErrUnavailable
	}
}

func mapPlaybackError(err error) error {
	switch {
	case errors.Is(err, gbplayback.ErrPlaybackNotFound):
		return adapters.ErrNotFound
	case errors.Is(err, gbplayback.ErrInvalidSession), errors.Is(err, gbplayback.ErrInvalidTransition):
		return adapters.ErrInvalidRequest
	case errors.Is(err, gbplayback.ErrPlaybackBusy):
		return adapters.ErrConflict
	default:
		return adapters.ErrUnavailable
	}
}

func validScope(scope resource.DepartmentScope) bool {
	return scope.OwnerDeptID > 0 && (scope.DataScope == resource.DataScopeDepartment || scope.DataScope == resource.DataScopeDepartmentAndChildren)
}

func decodeJSON(body []byte, target any) error {
	if len(body) == 0 {
		return io.ErrUnexpectedEOF
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("unexpected trailing JSON")
	}
	return nil
}

type recordQueryInput struct {
	StartTime  string `json:"startTime"`
	EndTime    string `json:"endTime"`
	Type       string `json:"type"`
	Secrecy    int    `json:"secrecy"`
	RecorderID string `json:"recorderId"`
}

type playbackCreateInput struct {
	RecordKey string `json:"recordKey"`
	PlayFrom  string `json:"playFrom"`
}

type playbackDownloadInput struct {
	RecordKey     string `json:"recordKey"`
	PlayFrom      string `json:"playFrom"`
	DownloadSpeed uint32 `json:"downloadSpeed"`
}

func validDownloadSpeed(speed uint32) bool {
	return speed == 1 || speed == 2 || speed == 4 || speed == 8
}

type playbackActionInput struct {
	Action          string  `json:"action"`
	PositionSeconds float64 `json:"positionSeconds"`
	Scale           float64 `json:"scale"`
}

func manscdpType(value string) manscdp.RecordInfoQueryType {
	return manscdp.RecordInfoQueryType(strings.TrimSpace(value))
}

func mapTargetError(err error) error { return err }

func executeTargetRequest(invocation catalogruntime.Invocation) (Request, error) {
	request, ok := invocation.Value.(adapters.Request)
	if !ok {
		return Request{}, adapters.ErrInvalidRequest
	}
	if param := invocation.Params["deviceId"]; param != "" && param != request.DeviceID {
		return Request{}, adapters.ErrInvalidRequest
	}
	if param := invocation.Params["channelId"]; param != "" && param != request.ChannelID {
		return Request{}, adapters.ErrInvalidRequest
	}
	if param := invocation.Params["sessionId"]; param != "" && request.DeviceID == "" {
		return Request{}, adapters.ErrInvalidRequest
	}
	return Request{
		ClientID: request.ClientID, IdempotencyKey: request.IdempotencyKey, ResourceScope: request.ResourceScope,
		DeviceID: request.DeviceID, ChannelID: request.ChannelID, SessionID: invocation.Params["sessionId"], Body: append([]byte(nil), invocation.Body...),
		Query: cloneQuery(invocation.Query),
	}, nil
}

func cloneQuery(query map[string]string) map[string]string {
	if len(query) == 0 {
		return nil
	}
	copy := make(map[string]string, len(query))
	for key, value := range query {
		copy[key] = value
	}
	return copy
}

type operationAdapter struct {
	key        string
	scope      string
	method     string
	dispatcher *Dispatcher
}

func (a *operationAdapter) Key() string {
	if a == nil {
		return ""
	}
	return a.key
}

func (a *operationAdapter) Execute(ctx context.Context, invocation catalogruntime.Invocation) (any, error) {
	if a == nil || a.dispatcher == nil {
		return nil, adapters.ErrUnavailable
	}
	if invocation.Scope != "" && invocation.Scope != a.scope || invocation.Method != "" && !strings.EqualFold(invocation.Method, a.method) {
		return nil, adapters.ErrInvalidRequest
	}
	request, err := executeTargetRequest(invocation)
	if err != nil {
		return nil, err
	}
	if a.method == http.MethodGet && len(invocation.Body) != 0 || a.scope != ScopeRecordQuery && len(invocation.Query) != 0 {
		return nil, adapters.ErrInvalidRequest
	}
	return a.dispatcher.Handle(ctx, a.scope, request)
}

func NewAdapterRegistrations(dispatcher *Dispatcher) []catalogruntime.AdapterRegistration {
	definitions := []struct{ key, scope, method string }{
		{RecordQueryAdapterKey, ScopeRecordQuery, http.MethodPost},
		{RecordQueryOptionsKey, ScopeRecordQueryOptions, http.MethodGet},
		{PlaybackCreateAdapterKey, ScopePlaybackCreate, http.MethodPost},
		{PlaybackReadAdapterKey, ScopePlaybackRead, http.MethodGet},
		{PlaybackActionAdapterKey, ScopePlaybackAction, http.MethodPost},
		{PlaybackStopAdapterKey, ScopePlaybackStop, http.MethodPost},
		{DownloadCreateAdapterKey, ScopeDownloadCreate, http.MethodPost},
		{DownloadReadAdapterKey, ScopeDownloadRead, http.MethodGet},
		{DownloadCancelAdapterKey, ScopeDownloadCancel, http.MethodDelete},
	}
	registrations := make([]catalogruntime.AdapterRegistration, 0, len(definitions))
	for _, definition := range definitions {
		adapter := &operationAdapter{key: definition.key, scope: definition.scope, method: definition.method, dispatcher: dispatcher}
		registrations = append(registrations, catalogruntime.AdapterRegistration{Key: definition.key, ContractVersion: "v1", Adapter: adapter})
	}
	return registrations
}
