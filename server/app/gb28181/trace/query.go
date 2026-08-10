package trace

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	DefaultMessagePageSize        = 100
	MaxMessagePageSize            = 500
	MaxTraceQueryRange            = 8 * 24 * time.Hour
	SIPStatusMin           uint16 = 100
	SIPStatusMax           uint16 = 699
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
	DeviceIDs  []string
	CallID     string
	Keyword    string
	Direction  Direction
	Method     string
	StatusCode uint16
	StatusMin  uint16
	StatusMax  uint16
	Cursor     string
	Limit      int
}

type MessageCursor struct {
	OccurredAt time.Time `json:"occurredAt"`
	EventID    string    `json:"eventId"`
}

type MessageSummary struct {
	EventID    string    `json:"eventId"`
	OccurredAt time.Time `json:"occurredAt"`
	Direction  Direction `json:"direction"`
	Transport  string    `json:"transport"`
	LocalAddr  string    `json:"localAddr"`
	RemoteAddr string    `json:"remoteAddr"`
	DeviceID   string    `json:"deviceId"`
	Method     string    `json:"method"`
	StatusCode uint16    `json:"statusCode"`
	CallID     string    `json:"callId"`
	CSeq       uint32    `json:"cseq"`
	CSeqMethod string    `json:"cseqMethod"`
	FromURI    string    `json:"fromUri,omitempty"`
	ToURI      string    `json:"toUri,omitempty"`
	UserAgent  string    `json:"userAgent,omitempty"`
	Malformed  bool      `json:"malformed"`
	ParseError string    `json:"parseError,omitempty"`
}

type MessagePage struct {
	Items      []MessageSummary `json:"items"`
	NextCursor string           `json:"nextCursor,omitempty"`
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
