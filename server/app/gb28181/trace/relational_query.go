package trace

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/trace/diagnosis"
)

func validateMessageFilter(filter MessageFilter) error {
	if filter.From.IsZero() || filter.To.IsZero() || !filter.From.Before(filter.To) || filter.To.Sub(filter.From) > MaxTraceQueryRange {
		return ErrTraceTimeRangeRequired
	}
	if filter.StatusMin != 0 || filter.StatusMax != 0 {
		min, max := filter.StatusMin, filter.StatusMax
		if min == 0 {
			min = SIPStatusMin
		}
		if max == 0 {
			max = SIPStatusMax
		}
		if min > max || min < SIPStatusMin || max > SIPStatusMax {
			return errors.New("invalid SIP trace status code range")
		}
	}
	return nil
}

func applyTraceKeywordFilter(query *gorm.DB, keyword, traceTable string) *gorm.DB {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return query
	}
	like := "%" + strings.ToLower(keyword) + "%"
	predicate := fmt.Sprintf(`
		LOWER(%[1]s.call_id) LIKE ? OR LOWER(%[1]s.device_id) LIKE ? OR EXISTS (
			SELECT 1 FROM gb_device
			WHERE gb_device.device_id = %[1]s.device_id
			  AND (LOWER(gb_device.name) LIKE ? OR LOWER(gb_device.alias) LIKE ?)
		)`, traceTable)
	return query.Where(predicate, like, like, like, like)
}

func applyMessageFilter(db *gorm.DB, filter MessageFilter) (*gorm.DB, error) {
	if err := validateMessageFilter(filter); err != nil {
		return nil, err
	}
	db = db.Where("occurred_at >= ? AND occurred_at < ?", filter.From.UTC(), filter.To.UTC())
	if len(filter.DeviceIDs) > 0 {
		db = db.Where("device_id IN ?", filter.DeviceIDs)
	} else if filter.DeviceID != "" {
		db = db.Where("device_id = ?", filter.DeviceID)
	}
	if filter.CallID != "" {
		db = db.Where("call_id = ?", filter.CallID)
	}
	if filter.Direction != "" {
		db = db.Where("direction = ?", string(filter.Direction))
	}
	if filter.Method != "" {
		db = db.Where("method = ?", filter.Method)
	}
	if filter.StatusCode != 0 {
		db = db.Where("status_code = ?", filter.StatusCode)
	}
	if filter.StatusMin != 0 || filter.StatusMax != 0 {
		min, max := filter.StatusMin, filter.StatusMax
		if min == 0 {
			min = SIPStatusMin
		}
		if max == 0 {
			max = SIPStatusMax
		}
		db = db.Where("status_code >= ? AND status_code <= ?", min, max)
	}
	db = applyTraceKeywordFilter(db, filter.Keyword, "gb_sip_trace_message")
	return db, nil
}

func messageSummary(row gbmodels.GbSipTraceMessage) MessageSummary {
	return MessageSummary{
		EventID: row.EventID, OccurredAt: row.OccurredAt.UTC(), Direction: Direction(row.Direction),
		Transport: row.Transport, LocalAddr: row.LocalAddr, RemoteAddr: row.RemoteAddr,
		DeviceID: row.DeviceID, Method: row.Method, StatusCode: row.StatusCode, CallID: row.CallID,
		CSeq: row.CSeq, CSeqMethod: row.CSeqMethod, FromURI: row.FromURI, ToURI: row.ToURI,
		UserAgent: row.UserAgent, Malformed: row.Malformed, ParseError: row.ParseError,
	}
}

func (s *RelationalStore) ListMessages(ctx context.Context, filter MessageFilter) (MessagePage, error) {
	query, err := applyMessageFilter(s.db.WithContext(ctx).Model(&gbmodels.GbSipTraceMessage{}), filter)
	if err != nil {
		return MessagePage{}, err
	}
	if filter.Cursor != "" {
		cursor, err := DecodeMessageCursor(filter.Cursor)
		if err != nil {
			return MessagePage{}, err
		}
		query = query.Where("occurred_at < ? OR (occurred_at = ? AND event_id < ?)", cursor.OccurredAt, cursor.OccurredAt, cursor.EventID)
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = DefaultMessagePageSize
	}
	if limit > MaxMessagePageSize {
		limit = MaxMessagePageSize
	}
	var rows []gbmodels.GbSipTraceMessage
	if err := query.Order("occurred_at DESC").Order("event_id DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
		return MessagePage{}, fmt.Errorf("query relational SIP trace messages: %w", err)
	}
	items := make([]MessageSummary, len(rows))
	for i, row := range rows {
		items[i] = messageSummary(row)
	}
	page := MessagePage{Items: items}
	if len(items) > limit {
		last := items[limit-1]
		page.Items = items[:limit]
		page.NextCursor = EncodeMessageCursor(MessageCursor{OccurredAt: last.OccurredAt, EventID: last.EventID})
	}
	return page, nil
}

func (s *RelationalStore) GetMessage(ctx context.Context, eventID string) (StoredMessage, error) {
	var row gbmodels.GbSipTraceMessage
	err := s.db.WithContext(ctx).First(&row, "event_id = ?", eventID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return StoredMessage{}, ErrTraceMessageNotFound
	}
	if err != nil {
		return StoredMessage{}, fmt.Errorf("query relational SIP trace message: %w", err)
	}
	return StoredMessage{
		MessageSummary: messageSummary(row),
		Payload: EncryptedPayload{
			Nonce: append([]byte(nil), row.PayloadNonce...), Ciphertext: append([]byte(nil), row.PayloadCiphertext...),
			Algorithm: row.PayloadAlgorithm, KeyVersion: row.PayloadKeyVersion, DigestSHA256: row.PayloadDigestSHA256,
		},
	}, nil
}

type sessionAccumulator struct {
	summary SessionSummary
	methods map[string]struct{}
}

type sessionCandidate struct {
	Day      time.Time
	DeviceID string
	CallID   string
	LastAt   time.Time
}

type databaseTime time.Time

func (value *databaseTime) Scan(raw any) error {
	if at, ok := raw.(time.Time); ok {
		*value = databaseTime(at)
		return nil
	}
	var text string
	switch raw := raw.(type) {
	case string:
		text = raw
	case []byte:
		text = string(raw)
	default:
		return fmt.Errorf("unsupported database time value %T", raw)
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	} {
		if at, err := time.Parse(layout, text); err == nil {
			*value = databaseTime(at)
			return nil
		}
	}
	return fmt.Errorf("unsupported database time %q", text)
}

func validateSessionFilter(filter SessionFilter) error {
	if filter.From.IsZero() || filter.To.IsZero() || !filter.From.Before(filter.To) || filter.To.Sub(filter.From) > MaxSessionQueryRange {
		return ErrTraceTimeRangeRequired
	}
	if filter.DiagnosisCode != "" && filter.DiagnosisCategory == "" {
		return errors.New("diagnosis category is required when code is set")
	}
	if filter.DiagnosisCategory != "" {
		if filter.DiagnosisCode == "" {
			if filter.DiagnosisCategory != diagnosis.CategoryRegisterFailure && filter.DiagnosisCategory != diagnosis.CategoryPlayStuck {
				return errors.New("invalid diagnosis category")
			}
		} else if err := diagnosis.ValidateCategoryCode(filter.DiagnosisCategory, filter.DiagnosisCode); err != nil {
			return err
		}
	}
	return nil
}

func sessionQueryRange(filter SessionFilter) (time.Time, time.Time) {
	return filter.From.UTC(), filter.To.UTC()
}

func applySessionFilter(query *gorm.DB, filter SessionFilter, from, to time.Time) *gorm.DB {
	query = query.
		Where("occurred_at >= ? AND occurred_at < ?", from, to)
	if len(filter.DeviceIDs) > 0 {
		query = query.Where("device_id IN ?", filter.DeviceIDs)
	} else if filter.DeviceID != "" {
		query = query.Where("device_id = ?", filter.DeviceID)
	}
	if filter.CallID != "" {
		query = query.Where("call_id = ?", filter.CallID)
	}
	query = applyTraceKeywordFilter(query, filter.Keyword, "gb_sip_trace_message")
	return query
}

func (s *RelationalStore) listSessionCandidates(
	ctx context.Context,
	filter SessionFilter,
	from time.Time,
	to time.Time,
	limit int,
) ([]sessionCandidate, error) {
	candidates := make([]sessionCandidate, 0, limit)
	for day := from; day.Before(to); day = day.AddDate(0, 0, 1) {
		dayEnd := day.AddDate(0, 0, 1)
		if dayEnd.After(to) {
			dayEnd = to
		}
		var rows []struct {
			DeviceID string       `gorm:"column:device_id"`
			CallID   string       `gorm:"column:call_id"`
			LastAt   databaseTime `gorm:"column:last_at"`
		}
		query := applySessionFilter(
			s.db.WithContext(ctx).Model(&gbmodels.GbSipTraceMessage{}),
			filter,
			day,
			dayEnd,
		)
		if err := query.
			Select("device_id, call_id, MAX(occurred_at) AS last_at").
			Group("device_id, call_id").
			Order("MAX(occurred_at) DESC").Order("call_id DESC").
			Limit(limit).
			Scan(&rows).Error; err != nil {
			return nil, fmt.Errorf("query relational SIP trace session candidates: %w", err)
		}
		for _, row := range rows {
			candidates = append(candidates, sessionCandidate{
				Day: day, DeviceID: row.DeviceID, CallID: row.CallID, LastAt: time.Time(row.LastAt).UTC(),
			})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].LastAt.Equal(candidates[j].LastAt) {
			return candidates[i].CallID > candidates[j].CallID
		}
		return candidates[i].LastAt.After(candidates[j].LastAt)
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates, nil
}

func applySessionCandidates(query *gorm.DB, candidates []sessionCandidate) *gorm.DB {
	var scope *gorm.DB
	for _, candidate := range candidates {
		condition := "device_id = ? AND call_id = ? AND occurred_at >= ? AND occurred_at < ?"
		args := []any{candidate.DeviceID, candidate.CallID, candidate.Day, candidate.Day.AddDate(0, 0, 1)}
		if scope == nil {
			scope = query.Session(&gorm.Session{NewDB: true}).Where(condition, args...)
		} else {
			scope = scope.Or(condition, args...)
		}
	}
	return query.Where(scope)
}

func sessionIdentity(day time.Time, deviceID, callID string) string {
	return day.UTC().Format("2006-01-02") + "\x00" + deviceID + "\x00" + callID
}

func applyDiagnosisBaseFilter(query *gorm.DB, filter SessionFilter) *gorm.DB {
	query = query.Where("state = ? AND observed_at >= ? AND observed_at < ?",
		string(diagnosis.StateActive), filter.From.UTC(), filter.To.UTC())
	if len(filter.DeviceIDs) > 0 {
		query = query.Where("device_id IN ?", filter.DeviceIDs)
	} else if filter.DeviceID != "" {
		query = query.Where("device_id = ?", filter.DeviceID)
	}
	if filter.CallID != "" {
		query = query.Where("call_id = ?", filter.CallID)
	}
	query = applyTraceKeywordFilter(query, filter.Keyword, "gb_sip_trace_session_diagnosis")
	if filter.DiagnosisCategory != "" {
		query = query.Where("category = ?", string(filter.DiagnosisCategory))
	}
	if filter.DiagnosisCode != "" {
		query = query.Where("code = ?", string(filter.DiagnosisCode))
	}
	return query
}

func (s *RelationalStore) activeSessionDiagnoses(ctx context.Context, filter SessionFilter) ([]gbmodels.GbSipTraceSessionDiagnosis, error) {
	var rows []gbmodels.GbSipTraceSessionDiagnosis
	query := applyDiagnosisBaseFilter(
		s.db.WithContext(ctx).Model(&gbmodels.GbSipTraceSessionDiagnosis{}), filter,
	)
	if err := query.Order("observed_at ASC").Order("id ASC").Find(&rows).Error; err != nil {
		// 诊断表查询失败只有在未请求诊断条件时才允许降级(原始报文列表
		// 不该被 additive 的诊断表故障藏起来);显式按类别/代码筛选时,
		// 静默返回空会把数据库故障伪装成"没有诊断结果"
		if filter.DiagnosisCategory == "" && filter.DiagnosisCode == "" {
			return nil, nil
		}
		return nil, err
	}
	return rows, nil
}

func diagnosisSummary(row gbmodels.GbSipTraceSessionDiagnosis) SessionDiagnosis {
	return SessionDiagnosis{
		ObservedAt: row.ObservedAt.UTC(), CorrelationKey: row.CorrelationKey,
		Category: diagnosis.Category(row.Category), Code: diagnosis.Code(row.Code),
		Stage: diagnosis.Stage(row.Stage), Source: diagnosis.Source(row.Source),
		DeviceID: row.DeviceID, ChannelID: row.ChannelID, CallID: row.CallID,
		CSeq: row.CSeq, Method: row.Method, StatusCode: row.StatusCode, StreamID: row.StreamID,
	}
}

func matchesDiagnosisFilter(item SessionDiagnosis, filter SessionFilter) bool {
	return (filter.DiagnosisCategory == "" || item.Category == filter.DiagnosisCategory) &&
		(filter.DiagnosisCode == "" || item.Code == filter.DiagnosisCode)
}

func reconstructedRegisterDiagnosis(session SessionSummary) *SessionDiagnosis {
	if session.FirstMethod != "REGISTER" || session.FinalStatus < 400 || session.FinalStatus == 401 {
		return nil
	}
	return &SessionDiagnosis{
		ObservedAt: session.LastAt, Category: diagnosis.CategoryRegisterFailure,
		Code: diagnosis.CodeUndetermined, Stage: diagnosis.StageRegister,
		Source: diagnosis.SourceReconstructed, DeviceID: session.DeviceID,
		CallID: session.CallID, Method: "REGISTER", StatusCode: session.FinalStatus,
	}
}

// loadSessionRows 执行报文查询并返回按时间排序的原始行
func (s *RelationalStore) loadSessionRows(query *gorm.DB) ([]gbmodels.GbSipTraceMessage, error) {
	var rows []gbmodels.GbSipTraceMessage
	if err := query.Order("occurred_at ASC").Order("event_id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("query relational SIP trace sessions: %w", err)
	}
	return rows, nil
}

// summarizeRows 把原始报文行聚合为会话摘要
func summarizeRows(rows []gbmodels.GbSipTraceMessage, retention time.Duration) []SessionSummary {
	accumulators := make(map[string]*sessionAccumulator)
	for _, row := range rows {
		at := row.OccurredAt.UTC()
		day := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
		key := sessionIdentity(day, row.DeviceID, row.CallID)
		acc := accumulators[key]
		if acc == nil {
			source, destination := row.LocalAddr, row.RemoteAddr
			if row.Direction == string(DirectionInbound) {
				source, destination = row.RemoteAddr, row.LocalAddr
			}
			acc = &sessionAccumulator{summary: SessionSummary{
				Day: day, DeviceID: row.DeviceID, CallID: row.CallID, FirstAt: at, LastAt: at,
				FirstMethod: row.Method, FromURI: row.FromURI, ToURI: row.ToURI,
				SourceAddr: source, DestinationAddr: destination,
			}, methods: make(map[string]struct{})}
			accumulators[key] = acc
		}
		acc.summary.LastAt = at
		acc.summary.MessageCount++
		if row.Direction == string(DirectionInbound) {
			acc.summary.InboundCount++
		} else if row.Direction == string(DirectionOutbound) {
			acc.summary.OutboundCount++
		}
		if row.Method != "" {
			acc.methods[row.Method] = struct{}{}
		}
		if row.StatusCode >= 200 {
			acc.summary.FinalStatus = row.StatusCode
			acc.summary.FinalResponseCount++
		}
		if row.StatusCode == 0 && row.Method != "" && row.Method != "ACK" {
			acc.summary.RequestCount++
		}
	}
	now := time.Now().UTC()
	sessions := make([]SessionSummary, 0, len(accumulators))
	for _, acc := range accumulators {
		for method := range acc.methods {
			acc.summary.Methods = append(acc.summary.Methods, method)
		}
		sort.Strings(acc.summary.Methods)
		expiresAt := acc.summary.LastAt.Add(retention)
		missing := acc.summary.RequestCount > acc.summary.FinalResponseCount
		acc.summary.SessionDerivedState = SessionDerivedState{
			OriginalAvailable: now.Before(expiresAt), OriginalExpiresAt: expiresAt,
			MissingResponse: missing,
		}
		sessions = append(sessions, acc.summary)
	}
	return sessions
}

func attachSessionDiagnoses(sessions []SessionSummary, rows []gbmodels.GbSipTraceSessionDiagnosis, filter SessionFilter) []SessionSummary {
	bySession := make(map[string][]SessionDiagnosis)
	for _, row := range rows {
		key := sessionIdentity(row.SessionDay, row.DeviceID, row.CallID)
		bySession[key] = append(bySession[key], diagnosisSummary(row))
	}
	filtered := make([]SessionSummary, 0, len(sessions))
	for i := range sessions {
		session := &sessions[i]
		session.diagnoses = append(session.diagnoses, bySession[sessionIdentity(session.Day, session.DeviceID, session.CallID)]...)
		if reconstructed := reconstructedRegisterDiagnosis(*session); reconstructed != nil {
			hasRegisterDiagnosis := false
			for _, item := range session.diagnoses {
				if item.Category == diagnosis.CategoryRegisterFailure {
					hasRegisterDiagnosis = true
					break
				}
			}
			if !hasRegisterDiagnosis {
				session.diagnoses = append(session.diagnoses, *reconstructed)
			}
		}
		matching := make([]SessionDiagnosis, 0, len(session.diagnoses))
		for _, item := range session.diagnoses {
			if matchesDiagnosisFilter(item, filter) {
				matching = append(matching, item)
			}
		}
		if (filter.DiagnosisCategory != "" || filter.DiagnosisCode != "") && len(matching) == 0 {
			continue
		}
		if len(matching) > 0 {
			latest := matching[len(matching)-1]
			session.Diagnosis = &latest
		}
		missing := session.RequestCount > session.FinalResponseCount
		statusAnomaly := session.FinalStatus >= 300
		if session.FirstMethod == "REGISTER" && session.FinalStatus == 401 && !missing && len(session.diagnoses) == 0 {
			statusAnomaly = false
		}
		session.MissingResponse = missing
		session.Anomaly = missing || statusAnomaly || len(session.diagnoses) > 0
		if !filter.Anomaly || session.Anomaly {
			filtered = append(filtered, *session)
		}
	}
	return filtered
}

func (s *RelationalStore) reduceSessions(ctx context.Context, filter SessionFilter, candidateLimit int) ([]SessionSummary, error) {
	if err := validateSessionFilter(filter); err != nil {
		return nil, err
	}
	diagnosisRows, err := s.activeSessionDiagnoses(ctx, filter)
	if err != nil {
		return nil, err
	}
	from, to := sessionQueryRange(filter)
	query := applySessionFilter(s.db.WithContext(ctx).Model(&gbmodels.GbSipTraceMessage{}), filter, from, to)
	if candidateLimit > 0 {
		candidates, err := s.listSessionCandidates(ctx, filter, from, to, candidateLimit)
		if err != nil {
			return nil, err
		}
		if len(candidates) == 0 {
			return []SessionSummary{}, nil
		}
		query = applySessionCandidates(query, candidates)
	}
	rows, err := s.loadSessionRows(query)
	if err != nil {
		return nil, err
	}
	sessions := summarizeRows(rows, s.rawMessageRetention())
	sessions = attachSessionDiagnoses(sessions, diagnosisRows, filter)
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].LastAt.Equal(sessions[j].LastAt) {
			return sessions[i].CallID > sessions[j].CallID
		}
		return sessions[i].LastAt.After(sessions[j].LastAt)
	})
	return sessions, nil
}

func (s *RelationalStore) ListSessions(ctx context.Context, filter SessionFilter) ([]SessionSummary, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = DefaultSessionPageSize
	}
	if limit > MaxSessionPageSize {
		limit = MaxSessionPageSize
	}
	candidateLimit := limit
	if filter.Anomaly || filter.DiagnosisCategory != "" || filter.DiagnosisCode != "" {
		candidateLimit = 0
	}
	sessions, err := s.reduceSessions(ctx, filter, candidateLimit)
	if err != nil {
		return nil, err
	}
	if len(sessions) > limit {
		sessions = sessions[:limit]
	}
	return sessions, nil
}

func (s *RelationalStore) GetSessionStats(ctx context.Context, filter SessionFilter) (SessionStats, error) {
	filter.Limit = 0
	filter.Anomaly = false
	// 统计给候选上限:会话窗口允许 31 天,无 LIMIT 的全窗报文载入会
	// 让慢查询和内存激增拖垮诊断页。上限内统计精确,超出按候选截断
	const maxStatsCandidates = 1000
	sessions, err := s.reduceSessions(ctx, filter, maxStatsCandidates)
	if err != nil {
		return SessionStats{}, err
	}
	var stats SessionStats
	stats.Truncated = len(sessions) >= maxStatsCandidates
	stats.Total = uint64(len(sessions))
	for _, session := range sessions {
		if session.Anomaly {
			stats.Anomaly++
		}
		categories := make(map[diagnosis.Category]struct{})
		for _, item := range session.diagnoses {
			categories[item.Category] = struct{}{}
		}
		if _, ok := categories[diagnosis.CategoryRegisterFailure]; ok {
			stats.RegisterFail++
		}
		if _, ok := categories[diagnosis.CategoryPlayStuck]; ok {
			stats.PlayStuck++
		}
	}
	stats.InvitePending = stats.PlayStuck
	return stats, nil
}
