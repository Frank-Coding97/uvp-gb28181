package dashboard

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"gorm.io/gorm"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

const (
	PlayOutcomeStarted    = "started"
	PlayOutcomeSuccess    = "success"
	PlayOutcomeFailure    = "failure"
	playAttemptStaleAfter = 5 * time.Minute
)

type PlayAttemptStore struct {
	db    *gorm.DB
	clock func() time.Time
}

func NewPlayAttemptStore(db *gorm.DB) *PlayAttemptStore {
	return &PlayAttemptStore{db: db, clock: time.Now}
}
func (store *PlayAttemptStore) SetClock(clock func() time.Time) { store.clock = clock }

func (store *PlayAttemptStore) Begin(ctx context.Context, userID uint, deviceID, channelID string) (string, error) {
	correlationID, err := newAttemptCorrelationID()
	if err != nil {
		return "", err
	}
	record := gbmodels.GbPlayAttempt{CorrelationID: correlationID, UserID: userID, DeviceCode: deviceID, ChannelCode: channelID, Outcome: PlayOutcomeStarted, StartedAt: store.clock()}
	if err := store.db.WithContext(ctx).Create(&record).Error; err != nil {
		return "", err
	}
	return correlationID, nil
}

func (store *PlayAttemptStore) Finish(ctx context.Context, correlationID, outcome, failureStage string, nodeID int64, reused bool) error {
	if correlationID == "" {
		return nil
	}
	finishedAt := store.clock()
	return store.db.WithContext(ctx).Model(&gbmodels.GbPlayAttempt{}).Where("correlation_id = ? AND outcome = ?", correlationID, PlayOutcomeStarted).Updates(map[string]any{
		"outcome": outcome, "failure_stage": failureStage, "node_id": nodeID, "reused": reused, "finished_at": finishedAt, "updated_at": finishedAt,
	}).Error
}

/**
 * PlayRatePoint 是「点播成功率」卡片背后那条迷你趋势线上的一个点。
 *
 * ⛔ 为什么由后端给：成功率是**后端算出来的比率**，前端唯一能做的"趋势"是把自己
 *    每 5 秒看到的当前值攒起来 —— 最多 24 个样本 = 2 分钟，一刷新页面就归零，
 *    而且 24H 滚动成功率在 2 分钟内几乎不动，画出来必然是一条水平线（用户 2026-11-16 报的就是这个）。
 *
 * 桶宽 = 1 小时、固定 24 个桶（含空桶 Attempts=0 / Rate=nil）。空桶不给 0%：
 * "这段时间没人点播"和"点播全失败了"是两件事，前端把它们连起来看就会误判。
 */
type PlayRatePoint struct {
	BucketStart time.Time `json:"bucketStart"`
	Attempts    uint64    `json:"attempts"`
	Success     uint64    `json:"success"`
	Rate        *float64  `json:"rate"`
}

type PlaySuccessSummary struct {
	Attempts     uint64          `json:"attempts"`
	Success      uint64          `json:"success"`
	Failure      uint64          `json:"failure"`
	Started      uint64          `json:"started"`
	StaleStarted uint64          `json:"staleStarted"`
	Rate         *float64        `json:"rate"`
	Series       []PlayRatePoint `json:"series"`
	Status       SectionStatus   `json:"status"`
	Coverage     Coverage        `json:"coverage"`
	AsOf         time.Time       `json:"asOf"`
}

func (store *PlayAttemptStore) Last24Hours(ctx context.Context, now time.Time) (PlaySuccessSummary, error) {
	return store.last24Hours(ctx, now, func() *gorm.DB {
		return store.db.WithContext(ctx).Model(&gbmodels.GbPlayAttempt{})
	})
}

func (store *PlayAttemptStore) Last24HoursScoped(ctx context.Context, now time.Time, scope QueryScope) (PlaySuccessSummary, error) {
	return store.last24Hours(ctx, now, func() *gorm.DB {
		query := store.db.WithContext(ctx).
			Table("gb_play_attempt AS attempt").
			Joins("JOIN gb_device ON gb_device.device_id = attempt.device_code AND gb_device.deleted_at IS NULL")
		if scope != nil {
			query = scope(query)
		}
		return query
	})
}

/**
 * newQuery 是**构造函数**而不是一个现成的 *gorm.DB。
 *
 * ⛔ GORM 的链式调用会就地修改并复用同一个 Statement：把同一个 query 变量喂给第二次 Scan，
 *    第一次的 Select / Where / Join 会全部叠进来（实测炸成 "JOIN gb_device" 重复两遍、
 *    owner_dept_id 变成 ambiguous）。汇总查询和趋势查询必须各拿一个新的。
 */
func (store *PlayAttemptStore) last24Hours(ctx context.Context, now time.Time, newQuery func() *gorm.DB) (PlaySuccessSummary, error) {
	var rows []struct {
		Outcome    string
		Count      uint64
		StaleCount uint64
	}
	windowStart := now.Add(-24 * time.Hour)
	if err := newQuery().Select("outcome, COUNT(*) AS count, COALESCE(SUM(CASE WHEN outcome = ? AND started_at <= ? THEN 1 ELSE 0 END), 0) AS stale_count", PlayOutcomeStarted, now.Add(-playAttemptStaleAfter)).
		Where("started_at >= ? AND started_at <= ?", windowStart, now).
		Where("outcome IN ?", []string{PlayOutcomeSuccess, PlayOutcomeFailure, PlayOutcomeStarted}).
		Group("outcome").Scan(&rows).Error; err != nil {
		return PlaySuccessSummary{}, err
	}
	result := PlaySuccessSummary{Status: StatusEmpty, Coverage: CoverageNotStarted, AsOf: now}
	for _, row := range rows {
		switch row.Outcome {
		case PlayOutcomeSuccess:
			result.Success = row.Count
		case PlayOutcomeFailure:
			result.Failure = row.Count
		default:
			result.Started = row.Count
			result.StaleStarted = row.StaleCount
		}
	}
	result.Attempts = result.Success + result.Failure
	if result.Attempts > 0 {
		rate := float64(result.Success) / float64(result.Attempts)
		result.Rate = &rate
		result.Status, result.Coverage = StatusOK, CoverageComplete
	}
	series, err := store.rateSeries(ctx, now, newQuery())
	if err != nil {
		return PlaySuccessSummary{}, err
	}
	result.Series = series
	if result.StaleStarted > 0 {
		result.Status, result.Coverage = StatusPartial, CoveragePartial
	}
	return result, nil
}

// playRateBuckets：一天按小时切成 24 桶，与卡片上「（24H）」的语义对齐。
const playRateBuckets = 24

/**
 * rateSeries 把 24 小时内的终态点播记录分到整点桶里，算出每桶的尝试数 / 成功数 / 成功率。
 *
 * ⛔ 不按各库的日期函数分桶（MySQL DATE_FORMAT、SQLite strftime、PG date_trunc 各不相同，
 *    这个仓同时支持三种库），只回捞两列在 Go 里分桶 —— 跨库安全，规则也只有一处。
 * ⛔ 空桶必须留着（Attempts=0、Rate=nil）：漏掉它前端就不知道"这一段本来就没活动"，
 *    只能把左右两点直接连起来，看起来像"一直很稳"，又是一个假趋势。
 */
func (store *PlayAttemptStore) rateSeries(ctx context.Context, now time.Time, query *gorm.DB) ([]PlayRatePoint, error) {
	var rows []struct {
		StartedAt time.Time
		Outcome   string
	}
	windowStart := now.Add(-playRateBuckets * time.Hour)
	if err := query.Select("started_at, outcome").
		Where("started_at >= ? AND started_at <= ?", windowStart, now).
		Where("outcome IN ?", []string{PlayOutcomeSuccess, PlayOutcomeFailure}).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	local := now.Location()
	hourStart := func(value time.Time) time.Time {
		at := value.In(local)
		return time.Date(at.Year(), at.Month(), at.Day(), at.Hour(), 0, 0, 0, local)
	}
	// 右对齐到整点：最后一桶 = 当前这一小时（还没走完，但用户要看到"正在发生"）。
	start := hourStart(now).Add(time.Hour - playRateBuckets*time.Hour)
	success := make([]uint64, playRateBuckets)
	attempts := make([]uint64, playRateBuckets)
	for _, row := range rows {
		offset := int(hourStart(row.StartedAt).Sub(start) / time.Hour)
		if offset < 0 || offset >= playRateBuckets {
			continue
		}
		attempts[offset]++
		if row.Outcome == PlayOutcomeSuccess {
			success[offset]++
		}
	}
	series := make([]PlayRatePoint, 0, playRateBuckets)
	for index := 0; index < playRateBuckets; index++ {
		point := PlayRatePoint{
			BucketStart: start.Add(time.Duration(index) * time.Hour),
			Attempts:    attempts[index],
			Success:     success[index],
		}
		if attempts[index] > 0 {
			rate := float64(success[index]) / float64(attempts[index])
			point.Rate = &rate
		}
		series = append(series, point)
	}
	return series, nil
}

func newAttemptCorrelationID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}
