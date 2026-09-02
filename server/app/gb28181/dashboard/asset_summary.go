package dashboard

import (
	"context"
	"time"

	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

type QueryScope func(*gorm.DB) *gorm.DB

type OnlineRateSummary struct {
	Total   uint64  `json:"total"`
	Online  uint64  `json:"online"`
	Offline uint64  `json:"offline"`
	Rate    float64 `json:"rate"`
}

type TrafficTodaySummary struct {
	UpstreamBytes   uint64        `json:"upstreamBytes"`
	DownstreamBytes uint64        `json:"downstreamBytes"`
	Status          SectionStatus `json:"status"`
	Coverage        Coverage      `json:"coverage"`
}

type AssetSummary struct {
	Devices  OnlineRateSummary   `json:"devices"`
	Channels OnlineRateSummary   `json:"channels"`
	Traffic  TrafficTodaySummary `json:"traffic"`
	AsOf     time.Time           `json:"asOf"`
}

type AssetSummaryService struct {
	db       *gorm.DB
	location *time.Location
}

func NewAssetSummaryService(db *gorm.DB, location *time.Location) *AssetSummaryService {
	if location == nil {
		location = time.Local
	}
	return &AssetSummaryService{db: db, location: location}
}

func (service *AssetSummaryService) Summary(ctx context.Context, now time.Time, deviceScope, channelScope, trafficScope QueryScope) (AssetSummary, error) {
	devices, err := aggregateOnline(service.db.WithContext(ctx).Model(&gbmodels.GbDevice{}), gbmodels.DeviceStatusOnline, deviceScope)
	if err != nil {
		return AssetSummary{}, err
	}
	channels, err := aggregateOnline(service.db.WithContext(ctx).Model(&gbmodels.GbChannel{}), gbmodels.ChannelStatusOnline, channelScope)
	if err != nil {
		return AssetSummary{}, err
	}
	localNow := now.In(service.location)
	dayStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, service.location)
	type trafficRow struct {
		UpstreamBytes   uint64
		DownstreamBytes uint64
	}
	var traffic trafficRow
	trafficQuery := service.db.WithContext(ctx).Table("gb_device_traffic_daily AS traffic").Select("COALESCE(SUM(traffic.upstream_bytes),0) AS upstream_bytes, COALESCE(SUM(traffic.downstream_bytes),0) AS downstream_bytes").Where("traffic.stat_date = ?", dayStart)
	if trafficScope != nil {
		trafficQuery = trafficScope(trafficQuery)
	}
	if err := trafficQuery.Scan(&traffic).Error; err != nil {
		return AssetSummary{}, err
	}
	trafficSummary := TrafficTodaySummary{UpstreamBytes: traffic.UpstreamBytes, DownstreamBytes: traffic.DownstreamBytes, Status: StatusOK, Coverage: CoverageComplete}
	var gaps int64
	if err := service.db.WithContext(ctx).Model(&gbmodels.GbDeviceTrafficGap{}).Where("started_at <= ? AND (ended_at IS NULL OR ended_at >= ?)", localNow, dayStart).Count(&gaps).Error; err != nil {
		return AssetSummary{}, err
	}
	if gaps > 0 {
		trafficSummary.Status, trafficSummary.Coverage = StatusPartial, CoveragePartial
	}
	return AssetSummary{Devices: devices, Channels: channels, Traffic: trafficSummary, AsOf: now}, nil
}

func aggregateOnline(query *gorm.DB, onlineStatus int8, scope QueryScope) (OnlineRateSummary, error) {
	if scope != nil {
		query = scope(query)
	}
	var row struct {
		Total  uint64
		Online uint64
	}
	if err := query.Select("COUNT(*) AS total, COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END),0) AS online", onlineStatus).Scan(&row).Error; err != nil {
		return OnlineRateSummary{}, err
	}
	result := OnlineRateSummary{Total: row.Total, Online: row.Online, Offline: row.Total - row.Online}
	if row.Total > 0 {
		result.Rate = float64(row.Online) / float64(row.Total)
	}
	return result, nil
}
