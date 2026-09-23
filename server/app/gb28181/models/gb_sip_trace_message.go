package models

import "time"

// GbSipTraceMessage stores one encrypted SIP transport event in the active
// business database. Payload plaintext must never be assigned to this model.
type GbSipTraceMessage struct {
	EventID             string    `gorm:"column:event_id;type:varchar(36);primaryKey"`
	OccurredAt          time.Time `gorm:"column:occurred_at;not null;index:idx_gb_sip_trace_occurred_event,priority:1;index:idx_gb_sip_trace_device_occurred,priority:2;index:idx_gb_sip_trace_call_occurred,priority:2"`
	Direction           string    `gorm:"column:direction;type:varchar(16);not null"`
	Transport           string    `gorm:"column:transport;type:varchar(16);not null"`
	LocalAddr           string    `gorm:"column:local_addr;type:varchar(255);not null"`
	RemoteAddr          string    `gorm:"column:remote_addr;type:varchar(255);not null"`
	DeviceID            string    `gorm:"column:device_id;type:varchar(64);not null;index:idx_gb_sip_trace_device_occurred,priority:1"`
	Method              string    `gorm:"column:method;type:varchar(32);not null"`
	StatusCode          uint16    `gorm:"column:status_code;not null"`
	CallID              string    `gorm:"column:call_id;type:varchar(255);not null;index:idx_gb_sip_trace_call_occurred,priority:1"`
	CSeq                uint32    `gorm:"column:cseq;not null"`
	CSeqMethod          string    `gorm:"column:cseq_method;type:varchar(32);not null"`
	FromURI             string    `gorm:"column:from_uri;type:varchar(512);not null"`
	ToURI               string    `gorm:"column:to_uri;type:varchar(512);not null"`
	FromID              string    `gorm:"column:from_id;type:varchar(64);not null"`
	ToID                string    `gorm:"column:to_id;type:varchar(64);not null"`
	BusinessCode        string    `gorm:"column:business_code;type:varchar(64);not null;index:idx_gb_sip_trace_business_occurred,priority:1"`
	BusinessType        string    `gorm:"column:business_type;type:varchar(64);not null"`
	BusinessConfidence  string    `gorm:"column:business_confidence;type:varchar(16);not null"`
	UserAgent           string    `gorm:"column:user_agent;type:varchar(512);not null"`
	Malformed           bool      `gorm:"column:malformed;not null"`
	ParseError          string    `gorm:"column:parse_error;type:varchar(1024);not null"`
	PayloadNonce        []byte    `gorm:"column:payload_nonce;not null"`
	PayloadCiphertext   []byte    `gorm:"column:payload_ciphertext;not null"`
	PayloadAlgorithm    string    `gorm:"column:payload_algorithm;type:varchar(32);not null"`
	PayloadKeyVersion   string    `gorm:"column:payload_key_version;type:varchar(64);not null"`
	PayloadDigestSHA256 string    `gorm:"column:payload_digest_sha256;type:char(64);not null"`
}

func (GbSipTraceMessage) TableName() string { return "gb_sip_trace_message" }
