package devicequery

import (
	"context"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

// AlarmFilter applies the same predicates to the result page and its total.
// AlarmFrom/AlarmTo refer to device-reported alarm time, with inclusive bounds.
type AlarmFilter struct {
	resource.DeviceListOptions
	SourceCode string
	AlarmFrom  *time.Time
	AlarmTo    *time.Time
	Priority   *int
	Method     *int
	AlarmType  *int
	alarmID    uint64
}

func (f AlarmFilter) validate() error {
	if f.Status != "" || utf8.RuneCountInString(f.Keyword) > 100 || utf8.RuneCountInString(f.SourceCode) > 20 {
		return resource.ErrInvalidListOptions
	}
	if f.AlarmFrom != nil && f.AlarmTo != nil && f.AlarmFrom.After(*f.AlarmTo) {
		return resource.ErrInvalidListOptions
	}
	return nil
}

func (f AlarmFilter) apply(query *gorm.DB) *gorm.DB {
	if f.alarmID != 0 {
		query = query.Where("a.id = ?", f.alarmID)
	}
	if f.SourceCode != "" {
		query = query.Where("a.source_code = ?", f.SourceCode)
	}
	if f.Keyword != "" {
		keyword := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(f.Keyword)
		query = query.Where("a.description LIKE ? ESCAPE '!'", "%"+keyword+"%")
	}
	if f.AlarmFrom != nil {
		query = query.Where("a.alarm_time >= ?", *f.AlarmFrom)
	}
	if f.AlarmTo != nil {
		query = query.Where("a.alarm_time <= ?", *f.AlarmTo)
	}
	for _, predicate := range []struct {
		column string
		value  *int
	}{{"a.priority", f.Priority}, {"a.method", f.Method}, {"a.alarm_type", f.AlarmType}} {
		if predicate.value != nil {
			query = query.Where(predicate.column+" = ?", *predicate.value)
		}
	}
	return query
}

func (s *Service) GetDeviceAlarmInScope(ctx context.Context, scope resource.DepartmentScope, deviceCode, alarmID string) (DeviceAlarm, error) {
	id, err := strconv.ParseUint(alarmID, 10, 64)
	if err != nil || id == 0 || strconv.FormatUint(id, 10) != alarmID {
		return DeviceAlarm{}, resource.ErrInvalidListOptions
	}
	page, err := s.SearchDeviceAlarmsInScope(ctx, scope, deviceCode, AlarmFilter{DeviceListOptions: resource.DeviceListOptions{Page: 1, PageSize: 1}, alarmID: id})
	if err != nil {
		return DeviceAlarm{}, err
	}
	if len(page.Items) != 1 {
		return DeviceAlarm{}, resource.ErrResourceNotFound
	}
	return page.Items[0], nil
}
