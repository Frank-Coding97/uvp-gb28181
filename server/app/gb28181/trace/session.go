package trace

import "time"

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
	return SessionDerivedState{OriginalAvailable: now.Before(expiresAt), OriginalExpiresAt: expiresAt, MissingResponse: missing, Anomaly: missing || finalStatus >= 300}
}
