package trace

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	DefaultMessagePageSize = 100
	MaxMessagePageSize     = 500
	MaxTraceQueryRange     = 8 * 24 * time.Hour
	// MaxTraceQueryRows 是「不传 limit ⇒ 按时间窗全量返回」时的保险丝。
	// ⛔ 它不是页大小上限：命中行数一旦超过它，必须显式报 ErrTraceQueryTooBroad
	// 让调用方缩小时间窗，**不能静默截断** —— 静默截断会让用户以为看到的就是全部。
	// 取值依据：单行摘要态约 300B（payload 列在会话聚合时已被 Omit 掉），
	// 20 万行 ≈ 60MB，是单次查询还能扛住的量级。
	MaxTraceQueryRows        = 200000
	SIPStatusMin      uint16 = 100
	SIPStatusMax      uint16 = 699
)

var (
	ErrTraceTimeRangeRequired = errors.New("SIP trace time range is required")
	ErrInvalidMessageCursor   = errors.New("invalid SIP trace message cursor")
	ErrTraceMessageNotFound   = errors.New("SIP trace message not found")
	ErrTraceQueryTooBroad     = errors.New("SIP trace query returns too many rows")
)

type MessageFilter struct {
	From         time.Time
	To           time.Time
	DeviceID     string
	DeviceIDs    []string
	CallID       string
	Keyword      string
	Direction    Direction
	Method       string
	StatusCode   uint16
	StatusMin    uint16
	StatusMax    uint16
	BusinessCode BusinessCode
	Cursor       string
	Limit        int
}

type MessageCursor struct {
	OccurredAt time.Time `json:"occurredAt"`
	EventID    string    `json:"eventId"`
}

type MessageSummary struct {
	EventID            string       `json:"eventId"`
	OccurredAt         time.Time    `json:"occurredAt"`
	Direction          Direction    `json:"direction"`
	Transport          string       `json:"transport"`
	LocalAddr          string       `json:"localAddr"`
	RemoteAddr         string       `json:"remoteAddr"`
	DeviceID           string       `json:"deviceId"`
	Method             string       `json:"method"`
	StatusCode         uint16       `json:"statusCode"`
	CallID             string       `json:"callId"`
	CSeq               uint32       `json:"cseq"`
	CSeqMethod         string       `json:"cseqMethod"`
	FromURI            string       `json:"fromUri,omitempty"`
	ToURI              string       `json:"toUri,omitempty"`
	FromID             string       `json:"fromId,omitempty"`
	ToID               string       `json:"toId,omitempty"`
	BusinessCode       BusinessCode `json:"businessCode"`
	BusinessType       string       `json:"businessType"`
	BusinessConfidence string       `json:"businessConfidence,omitempty"`
	UserAgent          string       `json:"userAgent,omitempty"`
	Malformed          bool         `json:"malformed"`
	ParseError         string       `json:"parseError,omitempty"`
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
