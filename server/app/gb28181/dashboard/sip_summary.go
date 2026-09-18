package dashboard

import (
	"context"
	"sort"
	"time"

	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

type SIPMinutePoint struct {
	BucketStart time.Time `json:"bucketStart"`
	Requests    uint64    `json:"requests"`
	Success     uint64    `json:"success"`
	Failure     uint64    `json:"failure"`
}

type SIPMetricSummary struct {
	RPM           uint64           `json:"rpm"`
	TodayRequests uint64           `json:"todayRequests"`
	Transactions  uint64           `json:"transactions"`
	Success       uint64           `json:"success"`
	Failure       uint64           `json:"failure"`
	Series        []SIPMinutePoint `json:"series"`
	Status        SectionStatus    `json:"status"`
	Coverage      Coverage         `json:"coverage"`
	AsOf          time.Time        `json:"asOf"`
}

type SIPMetricQuery struct {
	db       *gorm.DB
	location *time.Location
}

func NewSIPMetricQuery(db *gorm.DB, location *time.Location) *SIPMetricQuery {
	if location == nil {
		location = time.Local
	}
	return &SIPMetricQuery{db: db, location: location}
}

func (query *SIPMetricQuery) Summary(ctx context.Context, now time.Time) (SIPMetricSummary, error) {
	localNow := now.In(query.location)
	dayStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, query.location)
	var rows []gbmodels.GbSipMetricMinute
	if err := query.db.WithContext(ctx).Where("bucket_start >= ? AND bucket_start <= ?", dayStart, localNow).Order("bucket_start ASC").Find(&rows).Error; err != nil {
		return SIPMetricSummary{}, err
	}
	result := SIPMetricSummary{Series: []SIPMinutePoint{}, Status: StatusEmpty, Coverage: CoverageNotStarted, AsOf: now}
	if len(rows) == 0 {
		return result, nil
	}
	points := map[int64]*SIPMinutePoint{}
	for _, row := range rows {
		result.TodayRequests += row.RequestCount
		result.Transactions += row.TransactionCount
		result.Success += row.TransactionSuccess
		result.Failure += row.TransactionFailure
		bucket := row.BucketStart.Unix()
		point := points[bucket]
		if point == nil {
			point = &SIPMinutePoint{BucketStart: row.BucketStart}
			points[bucket] = point
		}
		point.Requests += row.RequestCount
		point.Success += row.TransactionSuccess
		point.Failure += row.TransactionFailure
	}
	for _, point := range points {
		result.Series = append(result.Series, *point)
	}
	sort.Slice(result.Series, func(i, j int) bool { return result.Series[i].BucketStart.Before(result.Series[j].BucketStart) })
	latestMinute := localNow.Truncate(time.Minute)
	for _, point := range result.Series {
		if point.BucketStart.Unix() == latestMinute.Unix() {
			result.RPM = point.Requests
		}
	}
	result.Status, result.Coverage = StatusOK, CoverageComplete
	if result.Series[0].BucketStart.After(dayStart.Add(time.Minute)) {
		result.Status, result.Coverage = StatusPartial, CoveragePartial
	}
	var gaps int64
	if err := query.db.WithContext(ctx).Model(&gbmodels.GbSipMetricGap{}).Where("started_at <= ? AND ended_at >= ?", localNow, dayStart).Count(&gaps).Error; err != nil {
		return SIPMetricSummary{}, err
	}
	if gaps > 0 {
		result.Status, result.Coverage = StatusPartial, CoveragePartial
	}
	return result, nil
}
