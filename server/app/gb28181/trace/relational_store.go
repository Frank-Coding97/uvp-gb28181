package trace

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	gbconfig "uvplatform.com/uvp-gb28181/app/gb28181/config"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// RelationalStore persists SIP trace events through the application's active
// GORM connection. It does not own or close that shared connection.
type RelationalStore struct {
	db            *gorm.DB
	retentionDays int
}

func NewRelationalStore(db *gorm.DB) (*RelationalStore, error) {
	return NewRelationalStoreWithRetention(db, gbconfig.DefaultSIPTraceRetentionDays)
}

func NewRelationalStoreWithRetention(db *gorm.DB, retentionDays int) (*RelationalStore, error) {
	if db == nil {
		return nil, ErrTraceStoreUnavailable
	}
	if retentionDays < gbconfig.MinSIPTraceRetentionDays || retentionDays > gbconfig.MaxSIPTraceRetentionDays {
		retentionDays = gbconfig.DefaultSIPTraceRetentionDays
	}
	return &RelationalStore{db: db, retentionDays: retentionDays}, nil
}

func (s *RelationalStore) rawMessageRetention() time.Duration {
	return time.Duration(s.retentionDays) * 24 * time.Hour
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
			FromID: event.FromID, ToID: event.ToID, BusinessCode: string(event.BusinessCode), BusinessType: event.BusinessType, BusinessConfidence: event.BusinessConfidence,
			Malformed: event.Malformed, ParseError: event.ParseError,
			PayloadNonce:      append([]byte(nil), event.Payload.Nonce...),
			PayloadCiphertext: append([]byte(nil), event.Payload.Ciphertext...),
			PayloadAlgorithm:  event.Payload.Algorithm, PayloadKeyVersion: event.Payload.KeyVersion,
			PayloadDigestSHA256: event.Payload.DigestSHA256,
		}
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := correlateEmptyBodyResponses(ctx, tx, events, rows); err != nil {
			return err
		}
		return tx.Create(&rows).Error
	}); err != nil {
		return fmt.Errorf("insert relational SIP trace batch: %w", err)
	}
	return nil
}

const responseCorrelationWindow = 2 * time.Minute

type responseTransactionKey struct {
	callID     string
	cseq       uint32
	method     string
	deviceID   string
	localAddr  string
	remoteAddr string
	direction  string
}

func responseKey(event StoredEvent) (responseTransactionKey, bool) {
	if event.StatusCode == 0 || !event.ResponseBodyEmpty || event.CallID == "" || event.CSeq == 0 ||
		event.CSeqMethod == "" || event.DeviceID == "" || event.LocalAddr == "" || event.RemoteAddr == "" {
		return responseTransactionKey{}, false
	}
	requestDirection := DirectionInbound
	if event.Direction == DirectionInbound {
		requestDirection = DirectionOutbound
	} else if event.Direction != DirectionOutbound {
		return responseTransactionKey{}, false
	}
	return responseTransactionKey{
		callID: event.CallID, cseq: event.CSeq, method: strings.ToUpper(event.CSeqMethod), deviceID: event.DeviceID,
		localAddr: event.LocalAddr, remoteAddr: event.RemoteAddr, direction: string(requestDirection),
	}, true
}

func requestMatchesResponse(row gbmodels.GbSipTraceMessage, key responseTransactionKey, response StoredEvent) bool {
	return row.StatusCode == 0 && row.CallID == key.callID && row.CSeq == key.cseq &&
		strings.EqualFold(row.CSeqMethod, key.method) && strings.EqualFold(row.Method, key.method) &&
		row.DeviceID == key.deviceID && row.LocalAddr == key.localAddr && row.RemoteAddr == key.remoteAddr &&
		row.Direction == key.direction && !row.OccurredAt.After(response.OccurredAt) &&
		!row.OccurredAt.Before(response.OccurredAt.Add(-responseCorrelationWindow))
}

func correlateEmptyBodyResponses(ctx context.Context, tx *gorm.DB, events []StoredEvent, rows []gbmodels.GbSipTraceMessage) error {
	responseIndexes := make([]int, 0)
	conditions := make([]string, 0)
	args := make([]any, 0)
	keysByIndex := make(map[int]responseTransactionKey)
	minAt, maxAt := time.Time{}, time.Time{}
	for i, event := range events {
		key, ok := responseKey(event)
		if !ok {
			continue
		}
		responseIndexes = append(responseIndexes, i)
		keysByIndex[i] = key
		conditions = append(conditions, "(call_id = ? AND cseq = ? AND UPPER(cseq_method) = ? AND device_id = ? AND local_addr = ? AND remote_addr = ? AND direction = ?)")
		args = append(args, key.callID, key.cseq, key.method, key.deviceID, key.localAddr, key.remoteAddr, key.direction)
		if minAt.IsZero() || event.OccurredAt.Before(minAt) {
			minAt = event.OccurredAt
		}
		if maxAt.IsZero() || event.OccurredAt.After(maxAt) {
			maxAt = event.OccurredAt
		}
	}
	if len(responseIndexes) == 0 {
		return nil
	}
	var existing []gbmodels.GbSipTraceMessage
	queryArgs := make([]any, 0, len(args)+2)
	queryArgs = append(queryArgs, minAt.Add(-responseCorrelationWindow), maxAt)
	queryArgs = append(queryArgs, args...)
	query := tx.WithContext(ctx).Where("status_code = 0 AND occurred_at >= ? AND occurred_at <= ? AND ("+strings.Join(conditions, " OR ")+")",
		queryArgs...)
	if err := query.Find(&existing).Error; err != nil {
		return fmt.Errorf("find unique SIP response transactions: %w", err)
	}
	for _, responseIndex := range responseIndexes {
		response := events[responseIndex]
		key := keysByIndex[responseIndex]
		matches := make([]gbmodels.GbSipTraceMessage, 0, 2)
		for _, candidate := range existing {
			if requestMatchesResponse(candidate, key, response) {
				matches = append(matches, candidate)
			}
		}
		for requestIndex, candidate := range rows {
			if requestIndex != responseIndex && requestMatchesResponse(candidate, key, response) {
				matches = append(matches, candidate)
			}
		}
		if len(matches) == 0 {
			continue
		}
		business := rowBusiness(matches[0])
		if business.Code == BusinessUnknown || business.Confidence == "none" {
			continue
		}
		conflict := false
		for _, candidate := range matches[1:] {
			other := rowBusiness(candidate)
			if other.Code != business.Code || other.Label != business.Label || other.Confidence == "none" {
				conflict = true
				break
			}
			if other.Confidence == "high" {
				business.Confidence = "high"
			}
		}
		if conflict {
			continue
		}
		rows[responseIndex].BusinessCode = string(business.Code)
		rows[responseIndex].BusinessType = business.Label
		rows[responseIndex].BusinessConfidence = business.Confidence
	}
	return nil
}

func (s *RelationalStore) Prune(ctx context.Context, cutoff time.Time, batchSize int) (int64, error) {
	if batchSize <= 0 {
		batchSize = DefaultTracePruneBatchSize
	}
	var deleted int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ids []string
		if err := tx.Model(&gbmodels.GbSipTraceMessage{}).
			Where("occurred_at < ?", cutoff.UTC()).
			Order("occurred_at ASC").Order("event_id ASC").Limit(batchSize).
			Pluck("event_id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		result := tx.Where("event_id IN ?", ids).Delete(&gbmodels.GbSipTraceMessage{})
		deleted = result.RowsAffected
		return result.Error
	})
	if err != nil {
		return deleted, fmt.Errorf("delete relational SIP trace batch: %w", err)
	}
	return deleted, nil
}
