package trace

import (
	"context"
	"fmt"
	"strings"
)

// SessionStats 是 SIP 日志页顶部统计卡的聚合数据。
// 一次 SQL 从 session_day 物化视图拿全,避免前端"只统计当前页"造成的失真。
type SessionStats struct {
	Total        uint64 `json:"total"`
	Anomaly      uint64 `json:"anomaly"`
	RegisterFail uint64 `json:"registerFail"` // 注册序列中出现 401 且最终未成功
	InvitePending uint64 `json:"invitePending"` // INVITE 会话最终无 2xx
}

// SessionStatsQuery 从 session_day MV 一次聚合出统计卡片需要的所有计数。
func buildSessionStatsQuery(table string, filter SessionFilter) (string, []any, error) {
	if filter.From.IsZero() || filter.To.IsZero() || !filter.From.Before(filter.To) || filter.To.Sub(filter.From) > MaxSessionQueryRange {
		return "", nil, ErrTraceTimeRangeRequired
	}
	parts := strings.Split(table, ".")
	if len(parts) != 2 || !identifierPattern.MatchString(parts[0]) || !identifierPattern.MatchString(parts[1]) {
		return "", nil, ErrInvalidClickHouseIdentifier
	}
	where := []string{"day >= toDate(?)", "day <= toDate(?)"}
	args := []any{filter.From.UTC(), filter.To.UTC()}
	if filter.DeviceID != "" {
		where = append(where, "device_id = ?")
		args = append(args, filter.DeviceID)
	} else if len(filter.DeviceIDs) > 0 {
		placeholders := make([]string, len(filter.DeviceIDs))
		for i, deviceID := range filter.DeviceIDs {
			placeholders[i] = "?"
			args = append(args, deviceID)
		}
		where = append(where, "device_id IN ("+strings.Join(placeholders, ", ")+")")
	}
	if filter.Keyword != "" {
		where = append(where, "(positionCaseInsensitive(call_id, ?) > 0 OR positionCaseInsensitive(device_id, ?) > 0)")
		args = append(args, filter.Keyword, filter.Keyword)
	}
	// 聚合列的解读:
	// - session_row = 每个 call_id 只算一次
	// - anomaly = final_status >= 300 OR request_count > final_response_count
	// - register_fail = first_method='REGISTER' 且 final_status=401 且 request_count > final_response_count
	// - invite_pending = first_method='INVITE' 且 final_status < 200(最终无 2xx 响应)
	query := fmt.Sprintf(`SELECT
    count() AS total,
    countIf(final_status >= 300 OR request_count > final_response_count) AS anomaly,
    countIf(first_method = 'REGISTER' AND final_status = 401 AND request_count > final_response_count) AS register_fail,
    countIf(first_method = 'INVITE' AND (final_status = 0 OR final_status = 100 OR final_status < 200)) AS invite_pending
FROM (
    SELECT
        day, device_id, call_id,
        argMinMerge(first_method_state) AS first_method,
        argMaxMerge(final_status_state) AS final_status,
        sum(request_count) AS request_count,
        sum(final_response_count) AS final_response_count
    FROM %s WHERE %s
    GROUP BY day, device_id, call_id
)`, table, strings.Join(where, " AND "))
	return query, args, nil
}

func (s *ClickHouseStore) GetSessionStats(ctx context.Context, filter SessionFilter) (SessionStats, error) {
	query, args, err := buildSessionStatsQuery(s.database+".sip_trace_session_day", filter)
	if err != nil {
		return SessionStats{}, err
	}
	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return SessionStats{}, fmt.Errorf("query ClickHouse SIP trace stats: %w", err)
	}
	if rows == nil {
		return SessionStats{}, nil
	}
	defer rows.Close()
	var stats SessionStats
	if rows.Next() {
		if err := rows.Scan(&stats.Total, &stats.Anomaly, &stats.RegisterFail, &stats.InvitePending); err != nil {
			return SessionStats{}, fmt.Errorf("scan ClickHouse SIP trace stats: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return SessionStats{}, fmt.Errorf("iterate ClickHouse SIP trace stats: %w", err)
	}
	return stats, nil
}
