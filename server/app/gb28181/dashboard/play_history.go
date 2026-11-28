package dashboard

import (
	"context"
	"sort"
	"time"
)

type PlayHistoryPoint struct {
	BucketStart time.Time `json:"bucketStart"`
	Success     uint64    `json:"success"`
	Failure     uint64    `json:"failure"`
	Started     uint64    `json:"started"`
	Rate        *float64  `json:"rate"`
}

type PlayHistorySummary struct {
	Attempts     uint64   `json:"attempts"`
	Success      uint64   `json:"success"`
	Failure      uint64   `json:"failure"`
	Started      uint64   `json:"started"`
	StaleStarted uint64   `json:"staleStarted"`
	Rate         *float64 `json:"rate"`
}

type CountDistribution struct {
	Key   string  `json:"key"`
	Count uint64  `json:"count"`
	Rate  float64 `json:"rate"`
}

type PlayHistory struct {
	Range         HistoryRange        `json:"range"`
	From          time.Time           `json:"from"`
	To            time.Time           `json:"to"`
	BucketSeconds int64               `json:"bucketSeconds"`
	Timezone      string              `json:"timezone"`
	Points        []PlayHistoryPoint  `json:"points"`
	Summary       PlayHistorySummary  `json:"summary"`
	FailureStages []CountDistribution `json:"failureStages"`
	Reuse         []CountDistribution `json:"reuse"`
	Status        SectionStatus       `json:"status"`
	Coverage      Coverage            `json:"coverage"`
}

func (store *PlayAttemptStore) HistoryScoped(ctx context.Context, window HistoryWindow, scope QueryScope) (PlayHistory, error) {
	result := PlayHistory{
		Range: window.Range, From: window.From, To: window.To,
		BucketSeconds: int64(window.Bucket.Seconds()), Timezone: window.Timezone,
		Points: []PlayHistoryPoint{}, FailureStages: []CountDistribution{}, Reuse: []CountDistribution{},
		Status: StatusEmpty, Coverage: CoverageNotStarted,
	}
	type attemptRow struct {
		Outcome      string
		FailureStage string
		Reused       bool
		StartedAt    time.Time
	}
	var rows []attemptRow
	dbQuery := store.db.WithContext(ctx).
		Table("gb_play_attempt AS attempt").
		Joins("JOIN gb_device ON gb_device.device_id = attempt.device_code AND gb_device.deleted_at IS NULL").
		Select("attempt.outcome", "attempt.failure_stage", "attempt.reused", "attempt.started_at").
		Where("attempt.started_at >= ? AND attempt.started_at <= ?", window.From, window.To)
	if scope != nil {
		dbQuery = scope(dbQuery)
	}
	if err := dbQuery.Order("attempt.started_at ASC").Scan(&rows).Error; err != nil {
		return PlayHistory{}, err
	}

	pointTotals := make(map[int64]PlayHistoryPoint)
	failureStages := make(map[string]uint64)
	reuse := map[string]uint64{"new": 0, "reused": 0}
	for _, row := range rows {
		start := bucketStart(row.StartedAt, window)
		point := pointTotals[start.Unix()]
		point.BucketStart = start
		switch row.Outcome {
		case PlayOutcomeSuccess:
			point.Success++
			result.Summary.Success++
		case PlayOutcomeFailure:
			point.Failure++
			result.Summary.Failure++
			stage := row.FailureStage
			if stage == "" {
				stage = "unknown"
			}
			failureStages[stage]++
		case PlayOutcomeStarted:
			point.Started++
			result.Summary.Started++
			if !row.StartedAt.After(window.To.Add(-playAttemptStaleAfter)) {
				result.Summary.StaleStarted++
			}
		}
		if row.Outcome == PlayOutcomeSuccess || row.Outcome == PlayOutcomeFailure {
			if row.Reused {
				reuse["reused"]++
			} else {
				reuse["new"]++
			}
		}
		pointTotals[start.Unix()] = point
	}
	result.Summary.Attempts = result.Summary.Success + result.Summary.Failure
	if result.Summary.Attempts > 0 {
		rate := float64(result.Summary.Success) / float64(result.Summary.Attempts)
		result.Summary.Rate = &rate
	}
	for start := bucketStart(window.From, window); !start.After(bucketStart(window.To, window)); start = start.Add(window.Bucket) {
		point := pointTotals[start.Unix()]
		point.BucketStart = start
		terminal := point.Success + point.Failure
		if terminal > 0 {
			rate := float64(point.Success) / float64(terminal)
			point.Rate = &rate
		}
		result.Points = append(result.Points, point)
	}
	if len(result.Points) > window.MaxPoints {
		result.Points = result.Points[len(result.Points)-window.MaxPoints:]
	}
	for key, count := range failureStages {
		result.FailureStages = append(result.FailureStages, CountDistribution{Key: key, Count: count, Rate: float64(count) / float64(result.Summary.Failure)})
	}
	for key, count := range reuse {
		if result.Summary.Attempts == 0 {
			continue
		}
		result.Reuse = append(result.Reuse, CountDistribution{Key: key, Count: count, Rate: float64(count) / float64(result.Summary.Attempts)})
	}
	sort.Slice(result.FailureStages, func(i, j int) bool { return result.FailureStages[i].Key < result.FailureStages[j].Key })
	sort.Slice(result.Reuse, func(i, j int) bool { return result.Reuse[i].Key < result.Reuse[j].Key })
	if len(rows) > 0 {
		result.Status, result.Coverage = StatusOK, CoverageComplete
	}
	if result.Summary.StaleStarted > 0 {
		result.Status, result.Coverage = StatusPartial, CoveragePartial
	}
	return result, nil
}
