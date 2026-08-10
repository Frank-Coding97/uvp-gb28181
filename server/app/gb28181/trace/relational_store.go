package trace

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// RelationalStore persists SIP trace events through the application's active
// GORM connection. It does not own or close that shared connection.
type RelationalStore struct {
	db *gorm.DB
}

func NewRelationalStore(db *gorm.DB) (*RelationalStore, error) {
	if db == nil {
		return nil, ErrTraceStoreUnavailable
	}
	return &RelationalStore{db: db}, nil
}

func (s *RelationalStore) InsertBatch(ctx context.Context, events []StoredEvent) error {
	if len(events) == 0 {
		return nil
	}
	rows := make([]gbmodels.GbSipTraceMessage, len(events))
	for i, event := range events {
		rows[i] = gbmodels.GbSipTraceMessage{
			EventID: event.EventID, OccurredAt: event.OccurredAt.UTC(), Direction: string(event.Direction),
			Transport: event.Transport, LocalAddr: event.LocalAddr, RemoteAddr: event.RemoteAddr,
			DeviceID: event.DeviceID, Method: event.Method, StatusCode: event.StatusCode,
			CallID: event.CallID, CSeq: event.CSeq, CSeqMethod: event.CSeqMethod,
			FromURI: event.FromURI, ToURI: event.ToURI, UserAgent: event.UserAgent,
			Malformed: event.Malformed, ParseError: event.ParseError,
			PayloadNonce:      append([]byte(nil), event.Payload.Nonce...),
			PayloadCiphertext: append([]byte(nil), event.Payload.Ciphertext...),
			PayloadAlgorithm:  event.Payload.Algorithm, PayloadKeyVersion: event.Payload.KeyVersion,
			PayloadDigestSHA256: event.Payload.DigestSHA256,
		}
	}
	if err := s.db.WithContext(ctx).Create(&rows).Error; err != nil {
		return fmt.Errorf("insert relational SIP trace batch: %w", err)
	}
	return nil
}
