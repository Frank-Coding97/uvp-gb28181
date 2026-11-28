package dashboard

import (
	"context"
	"time"

	"gorm.io/gorm"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

type QueryScope func(*gorm.DB) *gorm.DB

type OnlineRateSummary struct {
	Total   uint64  `json:"total"`
	Online  uint64  `json:"online"`
	Offline uint64  `json:"offline"`
	Rate    float64 `json:"rate"`
}

/**
 * TrafficTodayPoint 是「今日媒体流量」卡片迷你趋势线上的一个点（一小时一桶）。
 *
 * ⛔ 这里给的是**每小时增量**，不是累计值。累计值只会单调上升、形状永远是"往上爬"，
 *    用户 2026-11-16 报的"一直是平的、没有波动"正是拿累计值画的：8 小时里涨了 0.3GB，
 *    归一化后就是一条直线。要看波动必须给增量。
 */
type TrafficTodayPoint struct {
	BucketStart     time.Time `json:"bucketStart"`
	UpstreamBytes   uint64    `json:"upstreamBytes"`
	DownstreamBytes uint64    `json:"downstreamBytes"`
}

type TrafficTodaySummary struct {
	UpstreamBytes   uint64 `json:"upstreamBytes"`
	DownstreamBytes uint64 `json:"downstreamBytes"`
	// Series 只用小时表（gb_device_traffic_hourly）；日表 gb_device_traffic_daily 是累计台账，
	// 拆不出小时内分布。
	Series   []TrafficTodayPoint `json:"series"`
	Status   SectionStatus       `json:"status"`
	Coverage Coverage            `json:"coverage"`
}

type AssetSummary struct {
	Devices  OnlineRateSummary   `json:"devices"`
	Channels OnlineRateSummary   `json:"channels"`
	Traffic  TrafficTodaySummary `json:"traffic"`
	AsOf     time.Time           `json:"asOf"`
}

type AssetOnlineSummary struct {
	Devices  OnlineRateSummary `json:"devices"`
	Channels OnlineRateSummary `json:"channels"`
	AsOf     time.Time         `json:"asOf"`
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
	online, err := service.OnlineSummary(ctx, now, deviceScope, channelScope)
	if err != nil {
		return AssetSummary{}, err
	}
	traffic, err := service.TrafficSummary(ctx, now, trafficScope)
	if err != nil {
		return AssetSummary{}, err
	}
	return AssetSummary{Devices: online.Devices, Channels: online.Channels, Traffic: traffic, AsOf: now}, nil
}

func (service *AssetSummaryService) OnlineSummary(ctx context.Context, now time.Time, deviceScope, channelScope QueryScope) (AssetOnlineSummary, error) {
	devices, err := aggregateOnline(service.db.WithContext(ctx).Model(&gbmodels.GbDevice{}), gbmodels.DeviceStatusOnline, deviceScope)
	if err != nil {
		return AssetOnlineSummary{}, err
	}
	channels, err := aggregateOnline(service.db.WithContext(ctx).Model(&gbmodels.GbChannel{}), gbmodels.ChannelStatusOnline, channelScope)
	if err != nil {
		return AssetOnlineSummary{}, err
	}
	return AssetOnlineSummary{Devices: devices, Channels: channels, AsOf: now}, nil
}

func (service *AssetSummaryService) TrafficSummary(ctx context.Context, now time.Time, trafficScope QueryScope) (TrafficTodaySummary, error) {
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
		return TrafficTodaySummary{}, err
	}
	trafficSummary := TrafficTodaySummary{UpstreamBytes: traffic.UpstreamBytes, DownstreamBytes: traffic.DownstreamBytes, Status: StatusOK, Coverage: CoverageComplete}
	if err := service.fillTrafficSeries(ctx, &trafficSummary, dayStart, localNow, trafficScope); err != nil {
		return TrafficTodaySummary{}, err
	}
	var gaps int64
	if err := service.db.WithContext(ctx).Model(&gbmodels.GbDeviceTrafficGap{}).Where("started_at <= ? AND (ended_at IS NULL OR ended_at >= ?)", localNow, dayStart).Count(&gaps).Error; err != nil {
		return TrafficTodaySummary{}, err
	}
	if gaps > 0 {
		trafficSummary.Status, trafficSummary.Coverage = StatusPartial, CoveragePartial
	}
	return trafficSummary, nil
}

/**
 * fillTrafficSeries 填今日每小时的上行 / 下行**增量**（数据源是 gb_device_traffic_hourly）。
 *
 * ⛔ 用小时表而不是日表：日表是按天结算的累计台账，天然拆不出小时内分布。
 * ⛔ 小时桶要补齐（当日 00:00 到现在逐小时一个点，缺的填 0）：只发有数据的小时，
 *    前端就会把「凌晨没人看」两个小时直接连成一条直线，等于又造一个假趋势。
 */
func (service *AssetSummaryService) fillTrafficSeries(ctx context.Context, target *TrafficTodaySummary, dayStart, localNow time.Time, trafficScope QueryScope) error {
	var rows []struct {
		StatHour        time.Time
		UpstreamBytes   uint64
		DownstreamBytes uint64
	}
	query := service.db.WithContext(ctx).Table("gb_device_traffic_hourly AS traffic").
		Select("traffic.stat_hour AS stat_hour, COALESCE(SUM(traffic.upstream_bytes),0) AS upstream_bytes, COALESCE(SUM(traffic.downstream_bytes),0) AS downstream_bytes").
		Where("traffic.stat_hour >= ? AND traffic.stat_hour <= ?", dayStart, localNow).
		Group("traffic.stat_hour").
		Order("traffic.stat_hour ASC")
	if trafficScope != nil {
		query = trafficScope(query)
	}
	if err := query.Scan(&rows).Error; err != nil {
		return err
	}
	byHour := make(map[int64]TrafficTodayPoint, len(rows))
	for _, row := range rows {
		start := service.trafficHourStart(row.StatHour)
		point := byHour[start.Unix()]
		point.BucketStart = start
		point.UpstreamBytes += row.UpstreamBytes
		point.DownstreamBytes += row.DownstreamBytes
		byHour[start.Unix()] = point
	}
	series := make([]TrafficTodayPoint, 0, 24)
	for start := dayStart; !start.After(localNow); start = start.Add(time.Hour) {
		point := byHour[start.Unix()]
		point.BucketStart = start
		series = append(series, point)
	}
	target.Series = series
	return nil
}

func (service *AssetSummaryService) trafficHourStart(value time.Time) time.Time {
	at := value.In(service.location)
	return time.Date(at.Year(), at.Month(), at.Day(), at.Hour(), 0, 0, 0, service.location)
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
