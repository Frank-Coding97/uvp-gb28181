package play

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/openapi/auth"
	"uvplatform.com/uvp-gb28181/app/openapi/models"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

// LiveView describes this client's current authorization and its observed
// viewer state. A lease is not evidence that the shared stream is still live;
// no fresh media-server check is implied by this read-only ledger snapshot.
type LiveView struct {
	DeviceID           string     `json:"deviceId"`
	ChannelID          string     `json:"channelId"`
	StreamID           string     `json:"streamId"`
	App                string     `json:"app"`
	Protocol           string     `json:"protocol"`
	AuthorizationState string     `json:"authorizationState"`
	ViewerState        string     `json:"viewerState"`
	LastSeenAt         *time.Time `json:"lastSeenAt"`
	CreatedAt          time.Time  `json:"createdAt"`
	IssuedAt           time.Time  `json:"issuedAt"`
	ExpiresAt          time.Time  `json:"expiresAt"`
}

type LivePage struct {
	Items    []LiveView `json:"items"`
	Page     int        `json:"page"`
	PageSize int        `json:"pageSize"`
	Total    int        `json:"total"`
}

type liveReadRow struct {
	models.PlayGrant `gorm:"embedded"`
	ViewerState      string
	LastSeenAt       *time.Time
}

func (d *Dispatcher) Read(ctx context.Context, request auth.PlayRequest) (any, error) {
	if !validGBID(request.DeviceID) || !validGBID(request.ChannelID) {
		return nil, auth.ErrPlayInvalid
	}
	query, err := d.liveReadQuery(ctx, request)
	if err != nil {
		return nil, err
	}
	var rows []liveReadRow
	if err = query.Order("g.created_at DESC, g.grant_id DESC").Limit(1).Scan(&rows).Error; err != nil {
		return nil, auth.ErrPlayUnavailable
	}
	if len(rows) != 1 {
		return nil, auth.ErrPlayNotFound
	}
	return liveView(rows[0]), nil
}

func (d *Dispatcher) List(ctx context.Context, request auth.PlayListRequest) (any, error) {
	page, size := request.Page, request.PageSize
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = 20
	}
	if page < 1 || size < 1 || size > 100 || page-1 > int(^uint(0)>>1)/size {
		return nil, auth.ErrPlayInvalid
	}
	query, err := d.liveReadQuery(ctx, request.PlayRequest)
	if err != nil {
		return nil, err
	}
	// The quota bounds active leases per client. Deduplicate after ordering so
	// repeated explicit starts for one shared channel remain one public item.
	var rows []liveReadRow
	if err = query.Order("g.created_at DESC, g.grant_id DESC").Scan(&rows).Error; err != nil {
		return nil, auth.ErrPlayUnavailable
	}
	items := make([]LiveView, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		key := *row.DeviceID + "\x00" + *row.ChannelID
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		items = append(items, liveView(row))
	}
	result := LivePage{Items: []LiveView{}, Page: page, PageSize: size, Total: len(items)}
	start := (page - 1) * size
	if start < len(items) {
		end := start + size
		if end > len(items) {
			end = len(items)
		}
		result.Items = items[start:end]
	}
	return result, nil
}

func (d *Dispatcher) liveReadQuery(ctx context.Context, request auth.PlayRequest) (*gorm.DB, error) {
	if !d.Ready() {
		return nil, auth.ErrPlayUnavailable
	}
	if ctx == nil || request.ClientID <= 0 || request.OwnerDeptID == 0 ||
		request.DataScope != resource.DataScopeDepartment && request.DataScope != resource.DataScopeDepartmentAndChildren ||
		request.DeviceID != "" && !validGBID(request.DeviceID) || request.ChannelID != "" && !validGBID(request.ChannelID) {
		return nil, auth.ErrPlayInvalid
	}
	departments, err := resource.ResolveDepartmentIDs(ctx, d.db, resource.DepartmentScope{OwnerDeptID: request.OwnerDeptID, DataScope: request.DataScope})
	if err != nil {
		if errors.Is(err, resource.ErrResourceNotFound) {
			departments = []uint{0}
		} else {
			return nil, auth.ErrPlayUnavailable
		}
	}
	now, err := d.quota.currentTime()
	if err != nil {
		return nil, auth.ErrPlayUnavailable
	}
	query := d.db.WithContext(ctx).Table("gb_openapi_play_grant AS g").
		Select("g.*, COALESCE(v.state, '') AS viewer_state, v.last_seen_at").
		Joins("JOIN sys_openapi_client AS client ON client.id = g.client_id AND client.status = ? AND client.auth_epoch = g.client_epoch", models.StatusActive).
		Joins("JOIN sys_openapi_client_scope AS scope ON scope.client_id = g.client_id AND scope.scope = g.scope AND scope.enabled = ? AND scope.scope_epoch = g.scope_epoch", true).
		Joins("JOIN gb_device AS d ON d.device_id = g.device_id AND d.deleted_at IS NULL AND d.access_epoch = g.device_epoch AND d.cleanup_completed_epoch = d.access_epoch").
		Joins("JOIN sys_department AS dept ON dept.id = d.owner_dept_id AND dept.status = ? AND dept.deleted_at IS NULL", 1).
		Joins("JOIN gb_channel AS c ON c.device_id = d.device_id AND c.channel_id = g.channel_id AND c.owner_dept_id = d.owner_dept_id AND c.deleted_at IS NULL").
		Joins("LEFT JOIN gb_openapi_viewer AS v ON v.grant_id = g.grant_id AND v.node_uuid = g.node_uuid AND v.boot_nonce = g.boot_nonce AND v.schema = g.schema AND v.vhost = g.vhost AND v.app = g.app AND v.stream = g.stream AND v.media_generation = g.media_generation").
		Where("g.client_id = ? AND g.scope = ? AND g.state IN ? AND g.expires_at > ?", request.ClientID, auth.PlayLiveScope, []models.GrantState{models.GrantStatePending, models.GrantStateIssued, models.GrantStateBound}, now).
		Where("client.owner_dept_id = ? AND client.data_scope = ? AND d.owner_dept_id IN ? AND d.owner_dept_id <> 0", request.OwnerDeptID, request.DataScope, departments).
		Where("g.client_epoch > 0 AND g.scope_epoch > 0 AND g.device_epoch > 0").
		Where("NOT EXISTS (SELECT 1 FROM gb_device AS d2 WHERE d2.device_id = d.device_id AND d2.deleted_at IS NULL AND d2.id <> d.id)").
		Where("NOT EXISTS (SELECT 1 FROM gb_channel AS c2 WHERE c2.device_id = c.device_id AND c2.channel_id = c.channel_id AND c2.deleted_at IS NULL AND c2.id <> c.id)")
	if request.DeviceID != "" {
		query = query.Where("g.device_id = ?", request.DeviceID)
	}
	if request.ChannelID != "" {
		query = query.Where("g.channel_id = ?", request.ChannelID)
	}
	return query, nil
}

func liveView(row liveReadRow) LiveView {
	value := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	return LiveView{DeviceID: value(row.DeviceID), ChannelID: value(row.ChannelID), StreamID: value(row.Stream), App: value(row.App), Protocol: value(row.Protocol), AuthorizationState: string(row.State), ViewerState: row.ViewerState, LastSeenAt: row.LastSeenAt, CreatedAt: row.CreatedAt, IssuedAt: row.IssuedAt, ExpiresAt: row.ExpiresAt}
}

func (r *RuntimeRoot) Read(ctx context.Context, request auth.PlayRequest) (any, error) {
	dispatcher := r.current()
	reader, ok := dispatcher.(auth.PlayReader)
	if !ok || !safeReady(dispatcher) {
		return nil, auth.ErrPlayUnavailable
	}
	return reader.Read(ctx, request)
}
func (r *RuntimeRoot) List(ctx context.Context, request auth.PlayListRequest) (any, error) {
	dispatcher := r.current()
	reader, ok := dispatcher.(auth.PlayReader)
	if !ok || !safeReady(dispatcher) {
		return nil, auth.ErrPlayUnavailable
	}
	return reader.List(ctx, request)
}

var _ auth.PlayReader = (*Dispatcher)(nil)
var _ auth.PlayReader = (*RuntimeRoot)(nil)
