package config

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

const (
	NodeRuntimeStatusUnknown = "unknown"
	NodeRuntimeStatusActive  = "active"
	NodeRuntimeProtocolV1    = int64(1)

	maxRetiredBootHistoryEntries = 1024
	maxRetiredBootHistoryBytes   = 64 * 1024
)

var (
	ErrNodeRuntimeUnavailable = errors.New("openapi node runtime unavailable")
	ErrNodeRuntimeStale       = errors.New("openapi node runtime node revision is stale")
	ErrNodeRuntimeRetired     = errors.New("openapi node runtime boot identity is retired")
	ErrNodeRuntimeInvalid     = errors.New("openapi node runtime observation is invalid")
)

// NodeRuntimeRef is the caller's already-resolved node identity. NodeRevision
// is meta_node.revision, which advances for ordinary node state mutations such
// as MarkActive/Offline as well as endpoint edits; it is not a pure config
// revision. The store rechecks all three values against the locked row.
type NodeRuntimeRef struct {
	NodeID       int64
	NodeUUID     string
	NodeRevision uint64
}

// NodeRuntimeObservation is produced only after a trusted, one-to-one media
// probe. It is intentionally not an HTTP or Hook DTO and has no arbitrary
// status/current setter.
type NodeRuntimeObservation struct {
	NodeRuntimeRef
	BootNonce       string
	ProtocolVersion int64
}

// NodeRuntimeSnapshot is the durable runtime identity projected for one node.
// CurrentBootNonce is empty only for the valid never-confirmed initial state.
type NodeRuntimeSnapshot struct {
	NodeRuntimeRef
	CurrentBootNonce         string
	RetiredBootHistory       []string
	RuntimeEpoch             int64
	RuntimeProtocolVersion   int64
	RuntimeConfirmedRevision uint64
	RuntimeConfirmedAt       *time.Time
	IdentityStatus           string
}

// NodeRuntimeStore updates only the security columns in meta_node. It never
// invokes a broad Save, creates schema, edits viewer rows, or talks to ZLM.
type NodeRuntimeStore struct {
	db  *gorm.DB
	now func() time.Time
}

func NewNodeRuntimeStore(db *gorm.DB, now func() time.Time) *NodeRuntimeStore {
	if now == nil {
		now = time.Now
	}
	return &NodeRuntimeStore{db: db, now: now}
}

// Load reads a runtime mapping only when the persisted node still matches the
// caller's exact ID, UUID, and node revision.
func (s *NodeRuntimeStore) Load(ctx context.Context, ref NodeRuntimeRef) (NodeRuntimeSnapshot, error) {
	if err := validateNodeRuntimeRef(ctx, ref); err != nil {
		return NodeRuntimeSnapshot{}, err
	}
	if s == nil || s.db == nil {
		return NodeRuntimeSnapshot{}, ErrNodeRuntimeUnavailable
	}
	return loadNodeRuntime(s.db.WithContext(ctx), ref, false, true)
}

// ConfirmProbe commits a trusted media-process identity. A repeated boot is
// idempotent; a new boot retires the old nonce and advances the observation
// epoch in the same short transaction. A nonce present in retired history can
// never become current again. The caller must serialize probes per node: the
// store cannot identify an out-of-order first response before any boot is
// durably current.
func (s *NodeRuntimeStore) ConfirmProbe(ctx context.Context, observation NodeRuntimeObservation) (NodeRuntimeSnapshot, error) {
	if err := validateNodeRuntimeObservation(ctx, observation); err != nil {
		return NodeRuntimeSnapshot{}, err
	}
	if s == nil || s.db == nil || s.now == nil {
		return NodeRuntimeSnapshot{}, ErrNodeRuntimeUnavailable
	}
	confirmedAt := s.now().UTC().Truncate(time.Microsecond)
	if confirmedAt.IsZero() {
		return NodeRuntimeSnapshot{}, ErrNodeRuntimeUnavailable
	}

	var result NodeRuntimeSnapshot
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current, err := loadNodeRuntime(tx.WithContext(ctx), observation.NodeRuntimeRef, true, false)
		if err != nil {
			return err
		}
		if current.CurrentBootNonce != "" && current.CurrentBootNonce != observation.BootNonce && containsBootNonce(current.RetiredBootHistory, observation.BootNonce) {
			return ErrNodeRuntimeRetired
		}
		if current.CurrentBootNonce == observation.BootNonce && current.RuntimeProtocolVersion > 0 && current.RuntimeProtocolVersion != observation.ProtocolVersion {
			return ErrNodeRuntimeInvalid
		}

		history := append([]string{}, current.RetiredBootHistory...)
		epoch := current.RuntimeEpoch
		if current.CurrentBootNonce != observation.BootNonce {
			if epoch == math.MaxInt64 {
				return ErrNodeRuntimeUnavailable
			}
			if current.CurrentBootNonce != "" {
				history = append(history, current.CurrentBootNonce)
			}
			epoch++
		}
		historyJSON, err := marshalRetiredBootHistory(history)
		if err != nil {
			return ErrNodeRuntimeUnavailable
		}
		updates := map[string]any{
			"current_boot_nonce":         observation.BootNonce,
			"retired_boot_history":       historyJSON,
			"runtime_epoch":              epoch,
			"runtime_protocol_version":   observation.ProtocolVersion,
			"runtime_confirmed_revision": int64(observation.NodeRevision),
			"runtime_confirmed_at":       confirmedAt,
			"runtime_identity_status":    NodeRuntimeStatusActive,
		}
		if nodeRuntimeUpdateMatches(current, observation, epoch, history, confirmedAt) {
			result = NodeRuntimeSnapshot{
				NodeRuntimeRef:           observation.NodeRuntimeRef,
				CurrentBootNonce:         observation.BootNonce,
				RetiredBootHistory:       history,
				RuntimeEpoch:             epoch,
				RuntimeProtocolVersion:   observation.ProtocolVersion,
				RuntimeConfirmedRevision: observation.NodeRevision,
				RuntimeConfirmedAt:       timePtrUTC(confirmedAt),
				IdentityStatus:           NodeRuntimeStatusActive,
			}
			return nil
		}
		updated := tx.Model(&models.MediaNodeSecurity{}).
			Where("id = ? AND media_server_uuid = ? AND revision = ?", observation.NodeID, observation.NodeUUID, observation.NodeRevision).
			Updates(updates)
		if updated.Error != nil || updated.RowsAffected != 1 {
			return ErrNodeRuntimeUnavailable
		}
		result = NodeRuntimeSnapshot{
			NodeRuntimeRef:           observation.NodeRuntimeRef,
			CurrentBootNonce:         observation.BootNonce,
			RetiredBootHistory:       history,
			RuntimeEpoch:             epoch,
			RuntimeProtocolVersion:   observation.ProtocolVersion,
			RuntimeConfirmedRevision: observation.NodeRevision,
			RuntimeConfirmedAt:       timePtrUTC(confirmedAt),
			IdentityStatus:           NodeRuntimeStatusActive,
		}
		return nil
	})
	if err != nil {
		return NodeRuntimeSnapshot{}, normalizeNodeRuntimeError(err)
	}
	return result, nil
}

func nodeRuntimeUpdateMatches(current NodeRuntimeSnapshot, observation NodeRuntimeObservation, epoch int64, history []string, confirmedAt time.Time) bool {
	if current.CurrentBootNonce != observation.BootNonce || current.RuntimeEpoch != epoch || current.RuntimeProtocolVersion != observation.ProtocolVersion || current.RuntimeConfirmedRevision != observation.NodeRevision || current.IdentityStatus != NodeRuntimeStatusActive || !sameBootHistory(current.RetiredBootHistory, history) {
		return false
	}
	return current.RuntimeConfirmedAt != nil && current.RuntimeConfirmedAt.Equal(confirmedAt)
}

func sameBootHistory(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

// MarkUnknown records loss of trustworthy runtime continuity without changing
// the last current nonce, retired history, epoch, or confirmed revision.
func (s *NodeRuntimeStore) MarkUnknown(ctx context.Context, ref NodeRuntimeRef) (NodeRuntimeSnapshot, error) {
	if err := validateNodeRuntimeRef(ctx, ref); err != nil {
		return NodeRuntimeSnapshot{}, err
	}
	if s == nil || s.db == nil {
		return NodeRuntimeSnapshot{}, ErrNodeRuntimeUnavailable
	}
	var result NodeRuntimeSnapshot
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current, err := loadNodeRuntime(tx.WithContext(ctx), ref, true, false)
		if err != nil {
			return err
		}
		if current.IdentityStatus == NodeRuntimeStatusUnknown {
			result = current
			return nil
		}
		updated := tx.Model(&models.MediaNodeSecurity{}).
			Where("id = ? AND media_server_uuid = ? AND revision = ?", ref.NodeID, ref.NodeUUID, ref.NodeRevision).
			Update("runtime_identity_status", NodeRuntimeStatusUnknown)
		if updated.Error != nil || updated.RowsAffected != 1 {
			return ErrNodeRuntimeUnavailable
		}
		current.IdentityStatus = NodeRuntimeStatusUnknown
		result = current
		return nil
	})
	if err != nil {
		return NodeRuntimeSnapshot{}, normalizeNodeRuntimeError(err)
	}
	return result, nil
}

type nodeRuntimeProjection struct {
	ID                       *int64     `gorm:"column:id"`
	Revision                 *uint64    `gorm:"column:revision"`
	MediaServerUUID          *string    `gorm:"column:media_server_uuid"`
	CurrentBootNonce         *string    `gorm:"column:current_boot_nonce"`
	RetiredBootHistory       *string    `gorm:"column:retired_boot_history"`
	RuntimeEpoch             *int64     `gorm:"column:runtime_epoch"`
	RuntimeProtocolVersion   *int64     `gorm:"column:runtime_protocol_version"`
	RuntimeConfirmedRevision *int64     `gorm:"column:runtime_confirmed_revision"`
	RuntimeConfirmedAt       *time.Time `gorm:"column:runtime_confirmed_at"`
	RuntimeIdentityStatus    *string    `gorm:"column:runtime_identity_status"`
}

func (nodeRuntimeProjection) TableName() string { return "meta_node" }

func loadNodeRuntime(db *gorm.DB, ref NodeRuntimeRef, lock bool, requireFreshConfirmation bool) (NodeRuntimeSnapshot, error) {
	query := db.Model(&models.MediaNodeSecurity{})
	if lock {
		query = lockNodeRuntimeRow(db)
	}
	var rows []nodeRuntimeProjection
	result := query.Select("id, revision, media_server_uuid, current_boot_nonce, retired_boot_history, runtime_epoch, runtime_protocol_version, runtime_confirmed_revision, runtime_confirmed_at, runtime_identity_status").
		Where("id = ?", ref.NodeID).
		Order("id ASC").
		Limit(2).
		Find(&rows)
	if result.Error != nil || result.RowsAffected != 1 || len(rows) != 1 {
		if result.Error != nil && (errors.Is(result.Error, context.Canceled) || errors.Is(result.Error, context.DeadlineExceeded)) {
			return NodeRuntimeSnapshot{}, result.Error
		}
		return NodeRuntimeSnapshot{}, ErrNodeRuntimeUnavailable
	}
	row := rows[0]
	if row.ID == nil || *row.ID != ref.NodeID || row.MediaServerUUID == nil || *row.MediaServerUUID == "" || row.Revision == nil || *row.Revision == 0 {
		return NodeRuntimeSnapshot{}, ErrNodeRuntimeUnavailable
	}
	if *row.MediaServerUUID != ref.NodeUUID || *row.Revision != ref.NodeRevision {
		return NodeRuntimeSnapshot{}, ErrNodeRuntimeStale
	}
	snapshot, err := snapshotFromProjection(row, ref)
	if err != nil {
		return NodeRuntimeSnapshot{}, err
	}
	if requireFreshConfirmation && snapshot.CurrentBootNonce != "" && snapshot.RuntimeConfirmedRevision != ref.NodeRevision {
		return NodeRuntimeSnapshot{}, ErrNodeRuntimeStale
	}
	return snapshot, nil
}

func snapshotFromProjection(row nodeRuntimeProjection, ref NodeRuntimeRef) (NodeRuntimeSnapshot, error) {
	if row.RuntimeEpoch == nil || row.RuntimeProtocolVersion == nil || row.RuntimeConfirmedRevision == nil || row.RuntimeIdentityStatus == nil {
		return NodeRuntimeSnapshot{}, ErrNodeRuntimeUnavailable
	}
	if *row.RuntimeEpoch < 0 || *row.RuntimeProtocolVersion < 0 || *row.RuntimeConfirmedRevision < 0 {
		return NodeRuntimeSnapshot{}, ErrNodeRuntimeUnavailable
	}
	status := *row.RuntimeIdentityStatus
	if status != NodeRuntimeStatusUnknown && status != NodeRuntimeStatusActive {
		return NodeRuntimeSnapshot{}, ErrNodeRuntimeUnavailable
	}
	history, historyWasNull, err := parseRetiredBootHistory(row.RetiredBootHistory)
	if err != nil {
		return NodeRuntimeSnapshot{}, ErrNodeRuntimeUnavailable
	}
	current := ""
	if row.CurrentBootNonce != nil {
		if !validBootNonce(*row.CurrentBootNonce) {
			return NodeRuntimeSnapshot{}, ErrNodeRuntimeUnavailable
		}
		current = *row.CurrentBootNonce
	}
	if current == "" {
		if *row.RuntimeEpoch != 0 || *row.RuntimeProtocolVersion != 0 || *row.RuntimeConfirmedRevision != 0 || row.RuntimeConfirmedAt != nil || status != NodeRuntimeStatusUnknown {
			return NodeRuntimeSnapshot{}, ErrNodeRuntimeUnavailable
		}
		if !historyWasNull && len(history) != 0 {
			return NodeRuntimeSnapshot{}, ErrNodeRuntimeUnavailable
		}
	} else {
		if *row.RuntimeEpoch <= 0 || *row.RuntimeProtocolVersion != NodeRuntimeProtocolV1 || *row.RuntimeConfirmedRevision <= 0 || row.RuntimeConfirmedAt == nil || row.RuntimeConfirmedAt.IsZero() {
			return NodeRuntimeSnapshot{}, ErrNodeRuntimeUnavailable
		}
		if historyWasNull {
			return NodeRuntimeSnapshot{}, ErrNodeRuntimeUnavailable
		}
		if containsBootNonce(history, current) {
			return NodeRuntimeSnapshot{}, ErrNodeRuntimeUnavailable
		}
	}
	if uint64(*row.RuntimeConfirmedRevision) > ref.NodeRevision {
		return NodeRuntimeSnapshot{}, ErrNodeRuntimeUnavailable
	}
	if row.RuntimeConfirmedAt != nil {
		confirmedAt := row.RuntimeConfirmedAt.UTC().Truncate(time.Microsecond)
		row.RuntimeConfirmedAt = &confirmedAt
	}
	return NodeRuntimeSnapshot{
		NodeRuntimeRef:           ref,
		CurrentBootNonce:         current,
		RetiredBootHistory:       append([]string{}, history...),
		RuntimeEpoch:             *row.RuntimeEpoch,
		RuntimeProtocolVersion:   *row.RuntimeProtocolVersion,
		RuntimeConfirmedRevision: uint64(*row.RuntimeConfirmedRevision),
		RuntimeConfirmedAt:       timePtrUTCValue(row.RuntimeConfirmedAt),
		IdentityStatus:           status,
	}, nil
}

func parseRetiredBootHistory(raw *string) ([]string, bool, error) {
	if raw == nil {
		return nil, true, nil
	}
	if len(*raw) == 0 || len(*raw) > maxRetiredBootHistoryBytes {
		return nil, false, ErrNodeRuntimeUnavailable
	}
	var history []string
	if err := json.Unmarshal([]byte(*raw), &history); err != nil || history == nil || len(history) > maxRetiredBootHistoryEntries {
		return nil, false, ErrNodeRuntimeUnavailable
	}
	seen := make(map[string]struct{}, len(history))
	for _, nonce := range history {
		if !validBootNonce(nonce) {
			return nil, false, ErrNodeRuntimeUnavailable
		}
		if _, ok := seen[nonce]; ok {
			return nil, false, ErrNodeRuntimeUnavailable
		}
		seen[nonce] = struct{}{}
	}
	return history, false, nil
}

func marshalRetiredBootHistory(history []string) (string, error) {
	if len(history) > maxRetiredBootHistoryEntries {
		return "", ErrNodeRuntimeUnavailable
	}
	if history == nil {
		history = []string{}
	}
	seen := make(map[string]struct{}, len(history))
	for _, nonce := range history {
		if !validBootNonce(nonce) {
			return "", ErrNodeRuntimeUnavailable
		}
		if _, ok := seen[nonce]; ok {
			return "", ErrNodeRuntimeUnavailable
		}
		seen[nonce] = struct{}{}
	}
	raw, err := json.Marshal(history)
	if err != nil || len(raw) > maxRetiredBootHistoryBytes {
		return "", ErrNodeRuntimeUnavailable
	}
	return string(raw), nil
}

func lockNodeRuntimeRow(db *gorm.DB) *gorm.DB {
	if db == nil {
		return nil
	}
	if db.Dialector.Name() == "sqlserver" {
		return db.Table("meta_node WITH (UPDLOCK, HOLDLOCK)")
	}
	return db.Model(&models.MediaNodeSecurity{}).Clauses(clause.Locking{Strength: "UPDATE"})
}

func validateNodeRuntimeRef(ctx context.Context, ref NodeRuntimeRef) error {
	if ctx == nil {
		return ErrNodeRuntimeInvalid
	}
	if ref.NodeID <= 0 || ref.NodeRevision == 0 || ref.NodeRevision > math.MaxInt64 || !validNodeUUID(ref.NodeUUID) {
		return ErrNodeRuntimeInvalid
	}
	return nil
}

func validateNodeRuntimeObservation(ctx context.Context, observation NodeRuntimeObservation) error {
	if err := validateNodeRuntimeRef(ctx, observation.NodeRuntimeRef); err != nil {
		return err
	}
	if !validBootNonce(observation.BootNonce) || observation.ProtocolVersion != NodeRuntimeProtocolV1 {
		return ErrNodeRuntimeInvalid
	}
	return nil
}

func validBootNonce(value string) bool {
	if len(value) != 32 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validNodeUUID(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || len(value) > 64 || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func containsBootNonce(history []string, nonce string) bool {
	for _, candidate := range history {
		if candidate == nonce {
			return true
		}
	}
	return false
}

func normalizeNodeRuntimeError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, ErrNodeRuntimeUnavailable), errors.Is(err, ErrNodeRuntimeStale), errors.Is(err, ErrNodeRuntimeRetired), errors.Is(err, ErrNodeRuntimeInvalid):
		return err
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		return ErrNodeRuntimeUnavailable
	}
}

func timePtrUTC(value time.Time) *time.Time {
	value = value.UTC().Truncate(time.Microsecond)
	return &value
}

func timePtrUTCValue(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	return timePtrUTC(*value)
}
