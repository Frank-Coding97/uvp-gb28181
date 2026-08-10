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
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		like := "%" + strings.ToLower(keyword) + "%"
		db = db.Where("LOWER(call_id) LIKE ? OR LOWER(device_id) LIKE ?", like, like)
	}
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

func validateSessionFilter(filter SessionFilter) error {
	if filter.From.IsZero() || filter.To.IsZero() || !filter.From.Before(filter.To) || filter.To.Sub(filter.From) > MaxSessionQueryRange {
		return ErrTraceTimeRangeRequired
	}
	return nil
}

func (s *RelationalStore) reduceSessions(ctx context.Context, filter SessionFilter) ([]SessionSummary, error) {
	if err := validateSessionFilter(filter); err != nil {
		return nil, err
	}
	from := filter.From.UTC()
	from = time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	to := filter.To.UTC()
	to = time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC).Add(24 * time.Hour)
	query := s.db.WithContext(ctx).Model(&gbmodels.GbSipTraceMessage{}).
		Where("occurred_at >= ? AND occurred_at < ?", from, to)
	if len(filter.DeviceIDs) > 0 {
		query = query.Where("device_id IN ?", filter.DeviceIDs)
	} else if filter.DeviceID != "" {
		query = query.Where("device_id = ?", filter.DeviceID)
	}
	if filter.CallID != "" {
		query = query.Where("call_id = ?", filter.CallID)
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		like := "%" + strings.ToLower(keyword) + "%"
		query = query.Where("LOWER(call_id) LIKE ? OR LOWER(device_id) LIKE ?", like, like)
	}
	var rows []gbmodels.GbSipTraceMessage
	if err := query.Order("occurred_at ASC").Order("event_id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("query relational SIP trace sessions: %w", err)
	}
	accumulators := make(map[string]*sessionAccumulator)
	for _, row := range rows {
		at := row.OccurredAt.UTC()
		day := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
		key := day.Format("2006-01-02") + "\x00" + row.DeviceID + "\x00" + row.CallID
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
		expiresAt := acc.summary.LastAt.Add(s.rawMessageRetention())
		missing := acc.summary.RequestCount > acc.summary.FinalResponseCount
		acc.summary.SessionDerivedState = SessionDerivedState{
			OriginalAvailable: now.Before(expiresAt), OriginalExpiresAt: expiresAt,
			MissingResponse: missing, Anomaly: missing || acc.summary.FinalStatus >= 300,
		}
		if !filter.Anomaly || acc.summary.Anomaly {
			sessions = append(sessions, acc.summary)
		}
	}
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].LastAt.Equal(sessions[j].LastAt) {
			return sessions[i].CallID > sessions[j].CallID
		}
		return sessions[i].LastAt.After(sessions[j].LastAt)
	})
	return sessions, nil
}

func (s *RelationalStore) ListSessions(ctx context.Context, filter SessionFilter) ([]SessionSummary, error) {
	sessions, err := s.reduceSessions(ctx, filter)
	if err != nil {
		return nil, err
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = DefaultSessionPageSize
	}
	if limit > MaxSessionPageSize {
		limit = MaxSessionPageSize
	}
	if len(sessions) > limit {
		sessions = sessions[:limit]
	}
	return sessions, nil
}

func (s *RelationalStore) GetSessionStats(ctx context.Context, filter SessionFilter) (SessionStats, error) {
	filter.Limit = 0
	filter.Anomaly = false
	sessions, err := s.reduceSessions(ctx, filter)
	if err != nil {
		return SessionStats{}, err
	}
	var stats SessionStats
	stats.Total = uint64(len(sessions))
	for _, session := range sessions {
		if session.Anomaly {
			stats.Anomaly++
		}
		if session.FirstMethod == "REGISTER" && session.FinalStatus == 401 && session.RequestCount > session.FinalResponseCount {
			stats.RegisterFail++
		}
		if session.FirstMethod == "INVITE" && session.FinalStatus < 200 {
			stats.InvitePending++
		}
	}
	return stats, nil
}
