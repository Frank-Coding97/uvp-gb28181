package trace

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	DefaultMessagePageSize = 100
	MaxMessagePageSize     = 500
	MaxTraceQueryRange     = 8 * 24 * time.Hour
)

var (
	ErrTraceTimeRangeRequired = errors.New("SIP trace time range is required")
	ErrInvalidMessageCursor   = errors.New("invalid SIP trace message cursor")
	ErrTraceMessageNotFound   = errors.New("SIP trace message not found")
)

type MessageFilter struct {
	From       time.Time
	To         time.Time
	DeviceID   string
	CallID     string
	Direction  Direction
	Method     string
	StatusCode uint16
	Cursor     string
	Limit      int
}

type MessageCursor struct {
	OccurredAt time.Time `json:"occurredAt"`
	EventID    string    `json:"eventId"`
}

type MessageSummary struct {
	EventID    string
	OccurredAt time.Time
	Direction  Direction
	Transport  string
	LocalAddr  string
	RemoteAddr string
	DeviceID   string
	Method     string
	StatusCode uint16
	CallID     string
	CSeq       uint32
	CSeqMethod string
	Malformed  bool
	ParseError string
}

type MessagePage struct {
	Items      []MessageSummary
	NextCursor string
}

type StoredMessage struct {
	MessageSummary
	Payload EncryptedPayload
}

func EncodeMessageCursor(cursor MessageCursor) string {
	encoded, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func DecodeMessageCursor(encoded string) (MessageCursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return MessageCursor{}, ErrInvalidMessageCursor
	}
	var cursor MessageCursor
	if err := json.Unmarshal(data, &cursor); err != nil || cursor.OccurredAt.IsZero() {
		return MessageCursor{}, ErrInvalidMessageCursor
	}
	if _, err := uuid.Parse(cursor.EventID); err != nil {
		return MessageCursor{}, ErrInvalidMessageCursor
	}
	cursor.OccurredAt = cursor.OccurredAt.UTC()
	return cursor, nil
}

func buildMessageListQuery(table string, filter MessageFilter) (string, []any, error) {
	if filter.From.IsZero() || filter.To.IsZero() || !filter.From.Before(filter.To) || filter.To.Sub(filter.From) > MaxTraceQueryRange {
		return "", nil, ErrTraceTimeRangeRequired
	}
	parts := strings.Split(table, ".")
	if len(parts) != 2 || !identifierPattern.MatchString(parts[0]) || !identifierPattern.MatchString(parts[1]) {
		return "", nil, ErrInvalidClickHouseIdentifier
	}
	clauses := []string{"occurred_at >= ?", "occurred_at < ?"}
	args := []any{filter.From.UTC(), filter.To.UTC()}
	appendFilter := func(clause string, value any, present bool) {
		if present {
			clauses = append(clauses, clause)
			args = append(args, value)
		}
	}
	appendFilter("device_id = ?", filter.DeviceID, filter.DeviceID != "")
	appendFilter("call_id = ?", filter.CallID, filter.CallID != "")
	appendFilter("direction = ?", string(filter.Direction), filter.Direction != "")
	appendFilter("method = ?", filter.Method, filter.Method != "")
	appendFilter("status_code = ?", filter.StatusCode, filter.StatusCode != 0)
	if filter.Cursor != "" {
		cursor, err := DecodeMessageCursor(filter.Cursor)
		if err != nil {
			return "", nil, err
		}
		clauses = append(clauses, "(occurred_at, event_id) < (?, toUUID(?))")
		args = append(args, cursor.OccurredAt, cursor.EventID)
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = DefaultMessagePageSize
	}
	if limit > MaxMessagePageSize {
		limit = MaxMessagePageSize
	}
	args = append(args, limit+1)
	query := fmt.Sprintf(`SELECT toString(event_id), occurred_at, direction, transport,
local_addr, remote_addr, device_id, method, status_code, call_id, cseq, cseq_method,
malformed, parse_error FROM %s WHERE %s
ORDER BY occurred_at DESC, event_id DESC LIMIT ?`, table, strings.Join(clauses, " AND "))
	return query, args, nil
}

func (s *ClickHouseStore) ListMessages(ctx context.Context, filter MessageFilter) (MessagePage, error) {
	query, args, err := buildMessageListQuery(s.fullTable, filter)
	if err != nil {
		return MessagePage{}, err
	}
	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return MessagePage{}, fmt.Errorf("query ClickHouse SIP trace messages: %w", err)
	}
	defer rows.Close()
	limit := filter.Limit
	if limit <= 0 {
		limit = DefaultMessagePageSize
	}
	if limit > MaxMessagePageSize {
		limit = MaxMessagePageSize
	}
	items := make([]MessageSummary, 0, limit+1)
	for rows.Next() {
		var item MessageSummary
		var direction string
		var malformed uint8
		if err := rows.Scan(
			&item.EventID, &item.OccurredAt, &direction, &item.Transport, &item.LocalAddr,
			&item.RemoteAddr, &item.DeviceID, &item.Method, &item.StatusCode, &item.CallID,
			&item.CSeq, &item.CSeqMethod, &malformed, &item.ParseError,
		); err != nil {
			return MessagePage{}, fmt.Errorf("scan ClickHouse SIP trace message: %w", err)
		}
		item.Direction = Direction(direction)
		item.Malformed = malformed != 0
		item.OccurredAt = item.OccurredAt.UTC()
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return MessagePage{}, fmt.Errorf("iterate ClickHouse SIP trace messages: %w", err)
	}
	page := MessagePage{Items: items}
	if len(items) > limit {
		last := items[limit-1]
		page.Items = items[:limit]
		page.NextCursor = EncodeMessageCursor(MessageCursor{OccurredAt: last.OccurredAt, EventID: last.EventID})
	}
	return page, nil
}

func (s *ClickHouseStore) GetMessage(ctx context.Context, eventID string) (StoredMessage, error) {
	if _, err := uuid.Parse(eventID); err != nil {
		return StoredMessage{}, ErrTraceMessageNotFound
	}
	query := fmt.Sprintf(`SELECT toString(event_id), occurred_at, direction, transport,
local_addr, remote_addr, device_id, method, status_code, call_id, cseq, cseq_method,
malformed, parse_error, nonce, ciphertext, algorithm, key_version, digest_sha256
FROM %s WHERE event_id = toUUID(?) LIMIT 1`, s.fullTable)
	rows, err := s.conn.Query(ctx, query, eventID)
	if err != nil {
		return StoredMessage{}, fmt.Errorf("query ClickHouse SIP trace message: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return StoredMessage{}, fmt.Errorf("query ClickHouse SIP trace message: %w", err)
		}
		return StoredMessage{}, ErrTraceMessageNotFound
	}
	var message StoredMessage
	var direction string
	var malformed uint8
	if err := rows.Scan(
		&message.EventID, &message.OccurredAt, &direction, &message.Transport,
		&message.LocalAddr, &message.RemoteAddr, &message.DeviceID, &message.Method,
		&message.StatusCode, &message.CallID, &message.CSeq, &message.CSeqMethod,
		&malformed, &message.ParseError, &message.Payload.Nonce, &message.Payload.Ciphertext,
		&message.Payload.Algorithm, &message.Payload.KeyVersion, &message.Payload.DigestSHA256,
	); err != nil {
		return StoredMessage{}, fmt.Errorf("scan ClickHouse SIP trace message: %w", err)
	}
	message.Direction = Direction(direction)
	message.Malformed = malformed != 0
	message.OccurredAt = message.OccurredAt.UTC()
	return message, nil
}
