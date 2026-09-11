package trace

import (
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/trace/diagnosis"
)

const (
	RawMessageRetention    = 7 * 24 * time.Hour
	MaxSessionQueryRange   = 31 * 24 * time.Hour
	DefaultSessionPageSize = 50
	MaxSessionPageSize     = 200
)

type SessionFilter struct {
	From              time.Time
	To                time.Time
	DeviceID          string
	DeviceIDs         []string
	CallID            string
	Keyword           string
	Anomaly           bool
	DiagnosisCategory diagnosis.Category
	DiagnosisCode     diagnosis.Code
	Limit             int
}

type SessionDiagnosis struct {
	ObservedAt     time.Time          `json:"observedAt"`
	CorrelationKey string             `json:"correlationKey,omitempty"`
	Category       diagnosis.Category `json:"category"`
	Code           diagnosis.Code     `json:"code"`
	Stage          diagnosis.Stage    `json:"stage"`
	Source         diagnosis.Source   `json:"source"`
	DeviceID       string             `json:"deviceId,omitempty"`
	ChannelID      string             `json:"channelId,omitempty"`
	CallID         string             `json:"callId,omitempty"`
	CSeq           uint32             `json:"cseq,omitempty"`
	Method         string             `json:"method,omitempty"`
	StatusCode     uint16             `json:"statusCode,omitempty"`
	StreamID       string             `json:"streamId,omitempty"`
}

type SessionDerivedState struct {
	OriginalAvailable bool      `json:"originalAvailable"`
	OriginalExpiresAt time.Time `json:"originalExpiresAt"`
	MissingResponse   bool      `json:"missingResponse"`
	Anomaly           bool      `json:"anomaly"`
}

type SessionSummary struct {
	Day                time.Time         `json:"day"`
	DeviceID           string            `json:"deviceId"`
	CallID             string            `json:"callId"`
	FirstAt            time.Time         `json:"firstAt"`
	LastAt             time.Time         `json:"lastAt"`
	MessageCount       uint64            `json:"messageCount"`
	InboundCount       uint64            `json:"inboundCount"`
	OutboundCount      uint64            `json:"outboundCount"`
	Methods            []string          `json:"methods"`
	FinalStatus        uint16            `json:"finalStatus"`
	FirstMethod        string            `json:"firstMethod"`
	FromURI            string            `json:"fromUri,omitempty"`
	ToURI              string            `json:"toUri,omitempty"`
	FromID             string            `json:"fromId,omitempty"`
	ToID               string            `json:"toId,omitempty"`
	BusinessCode       BusinessCode      `json:"businessCode"`
	BusinessType       string            `json:"businessType"`
	BusinessConfidence string            `json:"businessConfidence,omitempty"`
	SourceAddr         string            `json:"sourceAddr,omitempty"`
	DestinationAddr    string            `json:"destinationAddr,omitempty"`
	RequestCount       uint64            `json:"requestCount"`
	FinalResponseCount uint64            `json:"finalResponseCount"`
	Diagnosis          *SessionDiagnosis `json:"diagnosis,omitempty"`
	diagnoses          []SessionDiagnosis
	SessionDerivedState
}

func deriveSessionState(lastAt time.Time, finalStatus uint16, requestCount, finalResponseCount uint64, now time.Time) SessionDerivedState {
	expiresAt := lastAt.Add(RawMessageRetention)
	missing := requestCount > finalResponseCount
	return SessionDerivedState{OriginalAvailable: now.Before(expiresAt), OriginalExpiresAt: expiresAt, MissingResponse: missing, Anomaly: missing || finalStatus >= 300}
}
