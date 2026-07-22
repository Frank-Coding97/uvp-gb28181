package talk

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

var (
	ErrLeaseConflict        = errors.New("通道对讲已被占用")
	ErrResourceConflict     = errors.New("对讲媒体资源已被占用")
	ErrInvalidTransition    = errors.New("非法对讲状态转换")
	ErrInvalidTerminalState = errors.New("非法对讲终态")
	ErrEmptyPublishToken    = errors.New("发布令牌不能为空")
)

var nonterminalStates = []models.TalkSessionState{
	models.TalkSessionReserved,
	models.TalkSessionPublishing,
	models.TalkSessionInviting,
	models.TalkSessionActive,
	models.TalkSessionStopping,
}

type TransitionPatch struct {
	NodeID       *int64
	App          *string
	SourceStream *string
	RecvStream   *string
	SSRC         *string
	CallID       *string
	PublishID    *string
	LocalPort    *int
	LocalTag     *string
	RemoteTag    *string
	RemoteURI    *string
	CSeq         *uint
	ExpiresAt    *time.Time
	StartedAt    *time.Time
	EndedAt      *time.Time
	Error        *string
}

type GormRepo struct {
	db *gorm.DB
}

func NewGormRepo(db *gorm.DB) *GormRepo {
	return &GormRepo{db: db}
}

func (r *GormRepo) Create(ctx context.Context, session *models.GbTalkSession, publishToken string) error {
	if strings.TrimSpace(publishToken) == "" {
		return ErrEmptyPublishToken
	}
	row := *session
	row.ID = 0
	row.PublishTokenHash = hashPublishToken(publishToken)
	row.TokenConsumedAt = nil
	setActiveKeys(&row)
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return classifyCreateError(&row, err)
	}
	*session = row
	return nil
}

func (r *GormRepo) FindBySession(ctx context.Context, sessionID string) (*models.GbTalkSession, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, nil
	}
	return r.findOne(ctx, "session_id = ?", sessionID)
}

func (r *GormRepo) FindBySource(ctx context.Context, nodeID int64, app, sourceStream string) (*models.GbTalkSession, error) {
	if strings.TrimSpace(sourceStream) == "" {
		return nil, nil
	}
	return r.findOne(ctx, "node_id = ? AND app = ? AND source_stream = ?", nodeID, app, sourceStream)
}

func (r *GormRepo) FindByRecv(ctx context.Context, nodeID int64, recvStream string) (*models.GbTalkSession, error) {
	if strings.TrimSpace(recvStream) == "" {
		return nil, nil
	}
	return r.findOne(ctx, "node_id = ? AND recv_stream = ?", nodeID, recvStream)
}

func (r *GormRepo) FindByCallID(ctx context.Context, callID string) (*models.GbTalkSession, error) {
	if strings.TrimSpace(callID) == "" {
		return nil, nil
	}
	return r.findOne(ctx, "call_id = ?", callID)
}

func (r *GormRepo) findOne(ctx context.Context, query string, args ...any) (*models.GbTalkSession, error) {
	var session models.GbTalkSession
	result := r.db.WithContext(ctx).Where(query, args...).Order("id DESC").Limit(1).Find(&session)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return &session, nil
}

func (r *GormRepo) Transition(ctx context.Context, sessionID string, from, to models.TalkSessionState, patch TransitionPatch) (bool, error) {
	if !canTransition(from, to) {
		return false, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, from, to)
	}
	updates := transitionUpdates(to, patch)
	result := r.db.WithContext(ctx).Model(&models.GbTalkSession{}).
		Where("session_id = ? AND state = ?", sessionID, from).
		Updates(updates)
	if result.Error != nil {
		if isUniqueConstraint(result.Error) {
			return false, fmt.Errorf("%w: %v", ErrResourceConflict, result.Error)
		}
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (r *GormRepo) ConsumeToken(ctx context.Context, sessionID, publishToken string, now time.Time) (bool, error) {
	if strings.TrimSpace(publishToken) == "" {
		return false, nil
	}
	result := r.db.WithContext(ctx).Model(&models.GbTalkSession{}).
		Where("session_id = ? AND publish_token_hash = ? AND token_consumed_at IS NULL", sessionID, hashPublishToken(publishToken)).
		Where("expires_at > ? AND state IN ?", now, nonterminalStates).
		Update("token_consumed_at", now)
	return result.RowsAffected > 0, result.Error
}

func (r *GormRepo) ConsumeTokenForPublish(ctx context.Context, sessionID, publishToken, publishID string, now time.Time) (bool, error) {
	if strings.TrimSpace(publishToken) == "" || strings.TrimSpace(publishID) == "" {
		return false, nil
	}
	session, err := r.FindBySession(ctx, sessionID)
	if err != nil || session == nil || session.ExpiresAt.Compare(now) <= 0 || !isNonterminal(session.State) {
		return false, err
	}
	want := hashPublishToken(publishToken)
	if subtle.ConstantTimeCompare([]byte(session.PublishTokenHash), []byte(want)) != 1 {
		return false, nil
	}
	if session.TokenConsumedAt != nil {
		return session.PublishID == publishID, nil
	}
	result := r.db.WithContext(ctx).Model(&models.GbTalkSession{}).
		Where("session_id = ? AND token_consumed_at IS NULL AND publish_id = '' AND expires_at > ? AND state IN ?", sessionID, now, nonterminalStates).
		Updates(map[string]any{"token_consumed_at": now, "publish_id": publishID})
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected > 0 {
		return true, nil
	}
	session, err = r.FindBySession(ctx, sessionID)
	return err == nil && session != nil && session.PublishID == publishID, err
}

func (r *GormRepo) ListNonterminal(ctx context.Context) ([]models.GbTalkSession, error) {
	var sessions []models.GbTalkSession
	err := r.db.WithContext(ctx).
		Where("state IN ?", nonterminalStates).
		Order("id").
		Find(&sessions).Error
	return sessions, err
}

func (r *GormRepo) ListExpired(ctx context.Context, now time.Time) ([]models.GbTalkSession, error) {
	var sessions []models.GbTalkSession
	err := r.db.WithContext(ctx).
		Where("state IN ? AND expires_at <= ?", nonterminalStates, now).
		Order("expires_at, id").
		Find(&sessions).Error
	return sessions, err
}

func (r *GormRepo) FinishAndReleaseLease(ctx context.Context, sessionID string, terminal models.TalkSessionState, message string, endedAt time.Time) (bool, error) {
	if !terminal.IsTerminal() {
		return false, fmt.Errorf("%w: %s", ErrInvalidTerminalState, terminal)
	}
	result := r.db.WithContext(ctx).Model(&models.GbTalkSession{}).
		Where("session_id = ? AND state IN ?", sessionID, nonterminalStates).
		Updates(map[string]any{
			"state":      terminal,
			"lease_key":  nil,
			"source_key": nil,
			"recv_key":   nil,
			"ssrc_key":   nil,
			"error":      message,
			"ended_at":   endedAt,
		})
	return result.RowsAffected > 0, result.Error
}

func hashPublishToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func setActiveKeys(session *models.GbTalkSession) {
	if session.State.IsTerminal() {
		session.LeaseKey = nil
		session.SourceKey = nil
		session.RecvKey = nil
		session.SSRCKey = nil
		return
	}
	lease := session.ChannelID
	session.LeaseKey = &lease
	session.SourceKey = optionalKey(session.SourceStream)
	session.RecvKey = optionalKey(session.RecvStream)
	session.SSRCKey = optionalKey(session.SSRC)
}

func optionalKey(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func transitionUpdates(to models.TalkSessionState, patch TransitionPatch) map[string]any {
	updates := map[string]any{"state": to}
	if patch.NodeID != nil {
		updates["node_id"] = *patch.NodeID
	}
	if patch.App != nil {
		updates["app"] = *patch.App
	}
	if patch.SourceStream != nil {
		updates["source_stream"] = *patch.SourceStream
		updates["source_key"] = optionalKey(*patch.SourceStream)
	}
	if patch.RecvStream != nil {
		updates["recv_stream"] = *patch.RecvStream
		updates["recv_key"] = optionalKey(*patch.RecvStream)
	}
	if patch.SSRC != nil {
		updates["ssrc"] = *patch.SSRC
		updates["ssrc_key"] = optionalKey(*patch.SSRC)
	}
	if patch.CallID != nil {
		updates["call_id"] = *patch.CallID
	}
	if patch.PublishID != nil {
		updates["publish_id"] = *patch.PublishID
	}
	if patch.LocalPort != nil {
		updates["local_port"] = *patch.LocalPort
	}
	if patch.LocalTag != nil {
		updates["dialog_local_tag"] = *patch.LocalTag
	}
	if patch.RemoteTag != nil {
		updates["dialog_remote_tag"] = *patch.RemoteTag
	}
	if patch.RemoteURI != nil {
		updates["dialog_remote_uri"] = *patch.RemoteURI
	}
	if patch.CSeq != nil {
		updates["dialog_cseq"] = *patch.CSeq
	}
	if patch.ExpiresAt != nil {
		updates["expires_at"] = *patch.ExpiresAt
	}
	if patch.StartedAt != nil {
		updates["started_at"] = *patch.StartedAt
	}
	if patch.Error != nil {
		updates["error"] = *patch.Error
	}
	if to.IsTerminal() {
		endedAt := time.Now()
		if patch.EndedAt != nil {
			endedAt = *patch.EndedAt
		}
		updates["lease_key"] = nil
		updates["source_key"] = nil
		updates["recv_key"] = nil
		updates["ssrc_key"] = nil
		updates["ended_at"] = endedAt
	}
	return updates
}

func canTransition(from, to models.TalkSessionState) bool {
	if from.IsTerminal() || from == to {
		return false
	}
	if to == models.TalkSessionFailed || to == models.TalkSessionExpired {
		return isNonterminal(from)
	}
	switch from {
	case models.TalkSessionReserved:
		return to == models.TalkSessionPublishing || to == models.TalkSessionStopping
	case models.TalkSessionPublishing:
		return to == models.TalkSessionInviting || to == models.TalkSessionStopping
	case models.TalkSessionInviting:
		return to == models.TalkSessionActive || to == models.TalkSessionStopping
	case models.TalkSessionActive:
		return to == models.TalkSessionStopping
	case models.TalkSessionStopping:
		return to == models.TalkSessionEnded
	default:
		return false
	}
}

func isNonterminal(state models.TalkSessionState) bool {
	for _, candidate := range nonterminalStates {
		if state == candidate {
			return true
		}
	}
	return false
}

func classifyCreateError(row *models.GbTalkSession, err error) error {
	if err == nil {
		return nil
	}
	if !isUniqueConstraint(err) {
		return err
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "lease_key") || strings.Contains(message, "uk_talk_session_lease") {
		return fmt.Errorf("%w: channel %d", ErrLeaseConflict, row.ChannelID)
	}
	return fmt.Errorf("%w: %v", ErrResourceConflict, err)
}

func isUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint") || strings.Contains(message, "duplicate entry")
}
