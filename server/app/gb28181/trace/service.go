package trace

import (
	"context"
	"errors"
)

var ErrTraceQueryUnavailable = errors.New("SIP trace query service unavailable")

type QueryRepository interface {
	ListMessages(context.Context, MessageFilter) (MessagePage, error)
	GetMessage(context.Context, string) (StoredMessage, error)
	ListSessions(context.Context, SessionFilter) ([]SessionSummary, error)
	GetSessionStats(context.Context, SessionFilter) (SessionStats, error)
}

type PayloadDecryptor interface {
	Decrypt(EncryptedPayload) ([]byte, error)
}

type MessageDetail struct {
	MessageSummary
	Payload   string `json:"payload"`
	Sensitive bool   `json:"sensitive"`
}

type QueryService struct {
	health     func() HealthSnapshot
	repository QueryRepository
	decryptor  PayloadDecryptor
}

func NewQueryService(health func() HealthSnapshot, repository QueryRepository, decryptor PayloadDecryptor) *QueryService {
	return &QueryService{health: health, repository: repository, decryptor: decryptor}
}

func QueryServiceFromRuntime(runtime Runtime) *QueryService {
	provider, ok := runtime.(interface{ QueryService() *QueryService })
	if !ok {
		return nil
	}
	return provider.QueryService()
}

func (m *Module) QueryService() *QueryService {
	repository, repositoryOK := m.store.(QueryRepository)
	decryptor, decryptorOK := m.cipher.(PayloadDecryptor)
	if !repositoryOK || !decryptorOK {
		return NewQueryService(m.Health, nil, nil)
	}
	return NewQueryService(m.Health, repository, decryptor)
}

func (s *QueryService) Health() HealthSnapshot {
	if s == nil || s.health == nil {
		return DisabledHealth()
	}
	return s.health()
}

func (s *QueryService) ListMessages(ctx context.Context, filter MessageFilter) (MessagePage, error) {
	if s == nil || s.repository == nil {
		return MessagePage{}, ErrTraceQueryUnavailable
	}
	return s.repository.ListMessages(ctx, filter)
}

func (s *QueryService) GetMessage(ctx context.Context, eventID string, sensitive bool, audit DisclosureContext) (MessageDetail, error) {
	if s == nil || s.repository == nil || s.decryptor == nil {
		return MessageDetail{}, ErrTraceQueryUnavailable
	}
	stored, err := s.repository.GetMessage(ctx, eventID)
	if err != nil {
		return MessageDetail{}, err
	}
	raw, err := s.decryptor.Decrypt(stored.Payload)
	if err != nil {
		return MessageDetail{}, err
	}
	display, err := RenderForDisplay(raw, sensitive, audit)
	if err != nil {
		return MessageDetail{}, err
	}
	return MessageDetail{MessageSummary: stored.MessageSummary, Payload: string(display), Sensitive: sensitive}, nil
}

func (s *QueryService) ListSessions(ctx context.Context, filter SessionFilter) ([]SessionSummary, error) {
	if s == nil || s.repository == nil {
		return nil, ErrTraceQueryUnavailable
	}
	return s.repository.ListSessions(ctx, filter)
}

func (s *QueryService) GetSessionStats(ctx context.Context, filter SessionFilter) (SessionStats, error) {
	if s == nil || s.repository == nil {
		return SessionStats{}, ErrTraceQueryUnavailable
	}
	return s.repository.GetSessionStats(ctx, filter)
}
