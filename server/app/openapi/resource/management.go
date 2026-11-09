package resource

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

var ErrInvalidUpdate = errors.New("invalid resource update")

var deviceSortColumns = map[string]string{"deviceId": "d.device_id", "name": "d.name", "status": "d.status", "registerTime": "d.register_time", "keepaliveTime": "d.keepalive_time", "createdAt": "d.created_at"}
var channelSortColumns = map[string]string{"channelId": "c.channel_id", "name": "c.name", "status": "c.status", "createdAt": "c.created_at"}

func validResourceSort(sort, order string, columns map[string]string) bool {
	return (sort == "" || columns[sort] != "") && (order == "" || order == "asc" || order == "desc")
}
func resourceOrder(sort, order string, columns map[string]string, fallback string) string {
	column := columns[sort]
	if column == "" {
		column = fallback
	}
	if order == "desc" {
		column += " DESC"
	} else {
		column += " ASC"
	}
	if columns[sort] != fallback && sort != "" {
		column += ", " + fallback + " ASC"
	}
	return column
}
func validFilterText(values ...string) bool {
	for _, value := range values {
		if !utf8.ValidString(value) || utf8.RuneCountInString(value) > 100 {
			return false
		}
	}
	return true
}
func validStreamTransport(value string) bool {
	return value == "UDP" || value == "TCP-Active" || value == "TCP-Passive"
}

type DeviceStatistics struct {
	Total              int64 `json:"total"`
	Online             int64 `json:"online"`
	Offline            int64 `json:"offline"`
	Unknown            int64 `json:"unknown"`
	ChannelCount       int64 `json:"channelCount"`
	OnlineChannelCount int64 `json:"onlineChannelCount"`
}
type ChannelStatistics struct {
	Total                 int64 `json:"total"`
	Online                int64 `json:"online"`
	Offline               int64 `json:"offline"`
	Unknown               int64 `json:"unknown"`
	Playing               int64 `json:"playing"`
	CloudRecordingEnabled int64 `json:"cloudRecordingEnabled"`
}

const statusStatistics = "COUNT(*) AS total, COALESCE(SUM(CASE WHEN %s.status = 1 THEN 1 ELSE 0 END), 0) AS online, COALESCE(SUM(CASE WHEN %s.status = 0 THEN 1 ELSE 0 END), 0) AS offline, COALESCE(SUM(CASE WHEN %s.status IS NULL OR %s.status NOT IN (0, 1) THEN 1 ELSE 0 END), 0) AS unknown"

func (s *Service) GetDeviceStatisticsInScope(ctx context.Context, scope DepartmentScope, options DeviceListOptions) (DeviceStatistics, error) {
	var result DeviceStatistics
	options, err := normalizeDeviceOptions(options)
	if err != nil {
		return result, err
	}
	ids, err := ResolveDepartmentIDs(ctx, s.db, scope)
	if err != nil {
		return result, err
	}
	query, err := s.deviceQuery(ctx, ids, options)
	if err != nil {
		return result, err
	}
	query = s.withDeviceCounts(ctx, query, ids)
	selection := strings.ReplaceAll(statusStatistics, "%s", "d") + ", COALESCE(SUM(channel_counts.channel_count),0) AS channel_count, COALESCE(SUM(channel_counts.online_channel_count),0) AS online_channel_count"
	err = query.Select(selection).Scan(&result).Error
	return result, err
}
func (s *Service) GetChannelStatisticsInScope(ctx context.Context, scope DepartmentScope, deviceID string, options ChannelListOptions) (ChannelStatistics, error) {
	var result ChannelStatistics
	options, err := normalizeChannelOptions(options)
	if err != nil {
		return result, err
	}
	if _, err = s.GetDeviceInScope(ctx, scope, deviceID); err != nil {
		return result, err
	}
	ids, err := ResolveDepartmentIDs(ctx, s.db, scope)
	if err != nil {
		return result, err
	}
	query, err := s.channelQuery(ctx, ids, deviceID, options)
	if err != nil {
		return result, err
	}
	selection := strings.ReplaceAll(statusStatistics, "%s", "c") + ", COALESCE(SUM(CASE WHEN c.stream_id <> '' THEN 1 ELSE 0 END),0) AS playing, COALESCE(SUM(CASE WHEN c.cloud_recording_enabled = ? THEN 1 ELSE 0 END),0) AS cloud_recording_enabled"
	err = query.Select(selection, true).Scan(&result).Error
	return result, err
}

type DeviceUpdate struct {
	Alias            *string `json:"alias"`
	ProtocolOverride *string `json:"protocolOverride"`
}
type ChannelUpdate struct {
	Alias           *string  `json:"alias"`
	PTZType         *int8    `json:"ptzType"`
	AudioEnabled    *bool    `json:"audioEnabled"`
	OnDemandLive    *bool    `json:"onDemandLive"`
	Longitude       *float64 `json:"longitude"`
	Latitude        *float64 `json:"latitude"`
	StreamTransport *string  `json:"streamTransport"`
}

func validAlias(alias *string) bool {
	return alias == nil || utf8.ValidString(*alias) && utf8.RuneCountInString(*alias) <= 255
}

func (s *Service) UpdateDeviceInScope(ctx context.Context, scope DepartmentScope, deviceID string, input DeviceUpdate) (Device, error) {
	if err := input.Validate(); err != nil {
		return Device{}, err
	}
	if s == nil || s.db == nil {
		return Device{}, ErrResourceUnavailable
	}
	var result Device
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		scoped := New(tx)
		if !validGBCode(deviceID) {
			return ErrResourceNotFound
		}
		ids, err := ResolveDepartmentIDs(ctx, tx, scope)
		if err != nil {
			return err
		}
		query, err := scoped.deviceQuery(ctx, ids, DeviceListOptions{})
		if err != nil {
			return err
		}
		var rows []gbmodels.GbDevice
		if err = query.Select("d.*").Where("d.device_id = ?", deviceID).Clauses(clause.Locking{Strength: "UPDATE"}).Limit(2).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) != 1 {
			return ErrResourceNotFound
		}
		device := rows[0]
		updates := map[string]any{}
		if input.Alias != nil && *input.Alias != device.Alias {
			updates["alias"] = *input.Alias
		}
		if input.ProtocolOverride != nil && *input.ProtocolOverride != device.ProtocolOverride {
			history := ""
			if device.EffectiveVersionSource == gbmodels.ProtocolVersionSourceRegister || device.EffectiveVersionSource == gbmodels.ProtocolVersionSourceHistory {
				history = device.EffectiveVersion
			}
			resolution := protocol.Resolve(protocol.ResolveInput{Override: *input.ProtocolOverride, Register: device.ReportedVersion, History: history})
			updates["protocol_override"] = *input.ProtocolOverride
			updates["effective_version"] = string(resolution.Profile.Version)
			updates["effective_version_source"] = string(resolution.Source)
			updates["effective_version_at"] = time.Now()
		}
		if len(updates) > 0 {
			if err = tx.Model(&gbmodels.GbDevice{}).Where("id = ? AND owner_dept_id = ?", device.ID, device.OwnerDeptID).Updates(updates).Error; err != nil {
				return err
			}
		}
		result, err = scoped.GetDeviceInScope(ctx, scope, deviceID)
		return err
	})
	return result, err
}

func (s *Service) UpdateChannelInScope(ctx context.Context, scope DepartmentScope, deviceID, channelID string, input ChannelUpdate) (Channel, error) {
	if err := input.Validate(); err != nil {
		return Channel{}, err
	}
	if s == nil || s.db == nil {
		return Channel{}, ErrResourceUnavailable
	}
	var result Channel
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		scoped := New(tx)
		if !validGBCode(deviceID) || !validGBCode(channelID) {
			return ErrResourceNotFound
		}
		ids, err := ResolveDepartmentIDs(ctx, tx, scope)
		if err != nil {
			return err
		}
		query, err := scoped.channelQuery(ctx, ids, deviceID, ChannelListOptions{})
		if err != nil {
			return err
		}
		var rows []gbmodels.GbChannel
		if err = query.Select("c.*").Where("c.channel_id = ?", channelID).Clauses(clause.Locking{Strength: "UPDATE"}).Limit(2).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) != 1 {
			return ErrResourceNotFound
		}
		channel := rows[0]
		updates := map[string]any{}
		if input.Alias != nil && *input.Alias != channel.Alias {
			updates["alias"] = *input.Alias
		}
		if input.PTZType != nil && *input.PTZType != channel.PTZType {
			updates["ptz_type"] = *input.PTZType
		}
		if input.AudioEnabled != nil && *input.AudioEnabled != channel.AudioEnabled {
			updates["audio_enabled"] = *input.AudioEnabled
		}
		if input.OnDemandLive != nil && *input.OnDemandLive != channel.OnDemandLive {
			updates["on_demand_live"] = *input.OnDemandLive
		}
		if input.StreamTransport != nil && *input.StreamTransport != channel.StreamTransport {
			updates["stream_transport"] = *input.StreamTransport
		}
		if input.Longitude != nil && (*input.Longitude != channel.Longitude || *input.Latitude != channel.Latitude || *input.Longitude == 0 && channel.PositionSource != "" || *input.Longitude != 0 && channel.PositionSource != "manual") {
			updates["longitude"], updates["latitude"] = *input.Longitude, *input.Latitude
			if *input.Longitude == 0 {
				updates["position_source"], updates["position_updated_at"] = "", nil
			} else {
				updates["position_source"], updates["position_updated_at"] = "manual", time.Now()
			}
		}
		if len(updates) > 0 {
			if err = tx.Model(&gbmodels.GbChannel{}).Where("id = ? AND owner_dept_id = ?", channel.ID, channel.OwnerDeptID).Updates(updates).Error; err != nil {
				return err
			}
		}
		result, err = scoped.GetChannelInScope(ctx, scope, deviceID, channelID)
		return err
	})
	return result, err
}

func (input DeviceUpdate) Validate() error {
	if !validAlias(input.Alias) || input.Alias == nil && input.ProtocolOverride == nil {
		return ErrInvalidUpdate
	}
	if input.ProtocolOverride != nil && *input.ProtocolOverride != "auto" && *input.ProtocolOverride != "2016" && *input.ProtocolOverride != "2022" {
		return ErrInvalidUpdate
	}
	return nil
}

func (input ChannelUpdate) Validate() error {
	if input.Alias == nil && input.PTZType == nil && input.AudioEnabled == nil && input.OnDemandLive == nil && input.Longitude == nil && input.Latitude == nil && input.StreamTransport == nil {
		return ErrInvalidUpdate
	}
	if !validAlias(input.Alias) || input.PTZType != nil && (*input.PTZType < 0 || *input.PTZType > 7) || input.StreamTransport != nil && !validStreamTransport(*input.StreamTransport) {
		return ErrInvalidUpdate
	}
	if (input.Longitude == nil) != (input.Latitude == nil) {
		return ErrInvalidUpdate
	}
	if input.Longitude != nil {
		lng, lat := *input.Longitude, *input.Latitude
		if math.IsNaN(lng) || math.IsNaN(lat) || math.IsInf(lng, 0) || math.IsInf(lat, 0) || lng < -180 || lng > 180 || lat < -90 || lat > 90 || (lng == 0) != (lat == 0) {
			return ErrInvalidUpdate
		}
	}
	return nil
}

func (options DeviceListOptions) Validate() error {
	_, err := normalizeDeviceOptions(options)
	return err
}
func (options ChannelListOptions) Validate() error {
	_, err := normalizeChannelOptions(options)
	return err
}
