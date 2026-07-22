package trace

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	RawMessageRetention    = 7 * 24 * time.Hour
	MaxSessionQueryRange   = 31 * 24 * time.Hour
	DefaultSessionPageSize = 50
	MaxSessionPageSize     = 200
)

type SessionFilter struct {
	From      time.Time
	To        time.Time
	DeviceID  string
	DeviceIDs []string
	CallID    string
	Keyword   string
	Anomaly   bool
	Limit     int
}

type SessionDerivedState struct {
	OriginalAvailable bool      `json:"originalAvailable"`
	OriginalExpiresAt time.Time `json:"originalExpiresAt"`
	MissingResponse   bool      `json:"missingResponse"`
	Anomaly           bool      `json:"anomaly"`
}

type SessionSummary struct {
	Day                time.Time `json:"day"`
	DeviceID           string    `json:"deviceId"`
	CallID             string    `json:"callId"`
	FirstAt            time.Time `json:"firstAt"`
	LastAt             time.Time `json:"lastAt"`
	MessageCount       uint64    `json:"messageCount"`
	InboundCount       uint64    `json:"inboundCount"`
	OutboundCount      uint64    `json:"outboundCount"`
	Methods            []string  `json:"methods"`
	FinalStatus        uint16    `json:"finalStatus"`
	FirstMethod        string    `json:"firstMethod"`
	FromURI            string    `json:"fromUri,omitempty"`
	ToURI              string    `json:"toUri,omitempty"`
	SourceAddr         string    `json:"sourceAddr,omitempty"`
	DestinationAddr    string    `json:"destinationAddr,omitempty"`
	RequestCount       uint64    `json:"requestCount"`
	FinalResponseCount uint64    `json:"finalResponseCount"`
	SessionDerivedState
}

func deriveSessionState(lastAt time.Time, finalStatus uint16, requestCount, finalResponseCount uint64, now time.Time) SessionDerivedState {
	expiresAt := lastAt.Add(RawMessageRetention)
	missing := requestCount > finalResponseCount
	return SessionDerivedState{
		OriginalAvailable: now.Before(expiresAt),
		OriginalExpiresAt: expiresAt,
		MissingResponse:   missing,
		Anomaly:           missing || finalStatus >= 300,
	}
}

func buildSessionListQuery(table string, filter SessionFilter) (string, []any, error) {
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
	if filter.CallID != "" {
		where = append(where, "call_id = ?")
		args = append(args, filter.CallID)
	}
	if filter.Keyword != "" {
		where = append(where,
			"(positionCaseInsensitive(call_id, ?) > 0 OR positionCaseInsensitive(device_id, ?) > 0)")
		args = append(args, filter.Keyword, filter.Keyword)
	}
	having := ""
	if filter.Anomaly {
		having = " HAVING (final_status >= 300 OR request_count > final_response_count)"
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = DefaultSessionPageSize
	}
	if limit > MaxSessionPageSize {
		limit = MaxSessionPageSize
	}
	args = append(args, limit)
	query := fmt.Sprintf(`SELECT day, device_id, call_id,
min(first_at) AS first_at,
max(last_at) AS last_at,
sum(message_count) AS message_count,
sum(inbound_count) AS inbound_count,
sum(outbound_count) AS outbound_count,
groupUniqArrayMerge(methods_state) AS methods,
argMaxMerge(final_status_state) AS final_status,
argMinMerge(first_method_state) AS first_method,
argMinMerge(from_uri_state) AS from_uri,
argMinMerge(to_uri_state) AS to_uri,
argMinMerge(source_addr_state) AS source_addr,
argMinMerge(destination_addr_state) AS destination_addr,
sum(request_count) AS request_count,
sum(final_response_count) AS final_response_count
FROM %s WHERE %s
GROUP BY day, device_id, call_id%s
ORDER BY last_at DESC LIMIT ?`, table, strings.Join(where, " AND "), having)
	return query, args, nil
}

func (s *ClickHouseStore) ListSessions(ctx context.Context, filter SessionFilter) ([]SessionSummary, error) {
	query, args, err := buildSessionListQuery(s.database+".sip_trace_session_day", filter)
	if err != nil {
		return nil, err
	}
	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query ClickHouse SIP trace sessions: %w", err)
	}
	defer rows.Close()
	sessions := make([]SessionSummary, 0)
	for rows.Next() {
		var session SessionSummary
		if err := rows.Scan(
			&session.Day, &session.DeviceID, &session.CallID, &session.FirstAt, &session.LastAt,
			&session.MessageCount, &session.InboundCount, &session.OutboundCount, &session.Methods,
			&session.FinalStatus,
			&session.FirstMethod, &session.FromURI, &session.ToURI, &session.SourceAddr, &session.DestinationAddr,
			&session.RequestCount, &session.FinalResponseCount,
		); err != nil {
			return nil, fmt.Errorf("scan ClickHouse SIP trace session: %w", err)
		}
		session.Day = session.Day.UTC()
		session.FirstAt = session.FirstAt.UTC()
		session.LastAt = session.LastAt.UTC()
		session.SessionDerivedState = deriveSessionState(
			session.LastAt, session.FinalStatus, session.RequestCount, session.FinalResponseCount, time.Now().UTC(),
		)
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ClickHouse SIP trace sessions: %w", err)
	}
	return sessions, nil
}
