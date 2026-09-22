package auth

import (
	"context"
	"net/url"
	"strconv"
	"unicode/utf8"

	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/openapi/resource"
)

// Only validated immutable values cross into a worker. No Gin context, shared
// query map, personnel principal, or arbitrary owner parameter is accepted.
type metadataInput struct {
	owner                                       uint
	dataScope                                   int8
	scope, deviceID, channelID, keyword, status string
	page, size                                  int
}

func (m metadataInput) resourceType() string {
	if m.channelID != "" || m.scope == "channel:list" {
		return "channel"
	}
	return "device"
}
func (m metadataInput) resourceID() string {
	if m.channelID != "" {
		return m.deviceID + ":" + m.channelID
	}
	return m.deviceID
}
func parseMetadata(q gatewayRequest, owner uint, dataScopes ...int8) (metadataInput, error) {
	dataScope := resource.DataScopeDepartment
	if len(dataScopes) == 1 {
		dataScope = dataScopes[0]
	}
	m := metadataInput{owner: owner, dataScope: dataScope, scope: q.scope, deviceID: q.deviceID, channelID: q.channelID, page: 1, size: 20}
	values, err := url.ParseQuery(q.rawQuery)
	if err != nil {
		return m, err
	}
	list := q.scope == "device:list" || q.scope == "channel:list"
	for key, items := range values {
		if !list || len(items) != 1 || (key != "page" && key != "pageSize" && key != "keyword" && key != "status") {
			return m, ErrDenied
		}
		if key != "keyword" && items[0] == "" {
			return m, ErrDenied
		}
	}
	for key, target := range map[string]*int{"page": &m.page, "pageSize": &m.size} {
		if value := values.Get(key); value != "" {
			for _, r := range value {
				if r < '0' || r > '9' {
					return m, ErrDenied
				}
			}
			n, err := strconv.Atoi(value)
			if err != nil || n <= 0 {
				return m, ErrDenied
			}
			*target = n
		}
	}
	if m.size > 100 || m.page-1 > int(^uint(0)>>1)/m.size {
		return m, ErrDenied
	}
	m.keyword, m.status = values.Get("keyword"), values.Get("status")
	if !utf8.ValidString(m.keyword) || utf8.RuneCountInString(m.keyword) > 100 {
		return m, ErrDenied
	}
	if m.status != "" && m.status != "online" && m.status != "offline" && m.status != "unknown" {
		return m, ErrDenied
	}
	return m, nil
}
func checkMetadata(ctx context.Context, db *gorm.DB, m metadataInput) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if m.owner == 0 {
		return resource.ErrResourceNotFound
	}
	scope := resource.DepartmentScope{OwnerDeptID: m.owner, DataScope: m.dataScope}
	if _, err := resource.ResolveDepartmentIDs(ctx, db, scope); err != nil {
		return err
	}
	svc := resource.New(db)
	if m.channelID != "" {
		_, err := svc.GetChannelInScope(ctx, scope, m.deviceID, m.channelID)
		return err
	}
	if m.deviceID != "" {
		_, err := svc.GetDeviceInScope(ctx, scope, m.deviceID)
		return err
	}
	return nil
}
func readMetadata(ctx context.Context, db *gorm.DB, m metadataInput) (any, error) {
	svc := resource.New(db)
	scope := resource.DepartmentScope{OwnerDeptID: m.owner, DataScope: m.dataScope}
	switch m.scope {
	case "device:list":
		return svc.ListDevicesInScope(ctx, scope, resource.DeviceListOptions{Page: m.page, PageSize: m.size, Keyword: m.keyword, Status: m.status})
	case "device:detail":
		return svc.GetDeviceInScope(ctx, scope, m.deviceID)
	case "device:status":
		return svc.GetDeviceStatusInScope(ctx, scope, m.deviceID)
	case "channel:list":
		return svc.ListChannelsInScope(ctx, scope, m.deviceID, resource.ChannelListOptions{Page: m.page, PageSize: m.size, Keyword: m.keyword, Status: m.status})
	case "channel:detail":
		return svc.GetChannelInScope(ctx, scope, m.deviceID, m.channelID)
	case "channel:status":
		return svc.GetChannelStatusInScope(ctx, scope, m.deviceID, m.channelID)
	default:
		return nil, ErrDenied
	}
}
