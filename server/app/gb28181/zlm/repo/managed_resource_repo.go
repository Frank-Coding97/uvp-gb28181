package repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

var (
	ErrManagedResourceNotFound      = errors.New("managed ZLM resource not found")
	ErrManagedResourceInvalidInput  = errors.New("invalid managed ZLM resource input")
	ErrManagedResourceSensitiveData = errors.New("sensitive data is not allowed in managed ZLM resource identity")
)

// ManagedResourceIdentity is the stable, non-secret identity used by the ledger.
// ResourceKey must be a canonical key, not a source/target URL or credential.
type ManagedResourceIdentity struct {
	NodeID       int64
	ResourceType string
	ResourceKey  string
	App          string
	Stream       string
}

// ManagedResourceRegistration contains only data safe for the provenance ledger.
// Callers that have a source/target URL must hash it with
// FingerprintManagedResourceParts and pass the digest, never the URL itself.
type ManagedResourceRegistration struct {
	Identity    ManagedResourceIdentity
	CreatedBy   uint64
	Fingerprint string
	Summary     string
	ObservedAt  time.Time
}

type ManagedResourceFilter struct {
	NodeID            int64
	ResourceType      string
	IncludeTombstoned bool
}

type ManagedResourceRepo struct {
	db  *gorm.DB
	now func() time.Time
}

func NewManagedResourceRepo(db *gorm.DB) *ManagedResourceRepo {
	return &ManagedResourceRepo{db: db, now: time.Now}
}

// Register records a resource after ZLM confirmed its creation. Re-registering
// the same identity updates observation metadata while preserving creation source.
func (r *ManagedResourceRepo) Register(ctx context.Context, input ManagedResourceRegistration) (*gbmodels.GbZLMManagedResource, error) {
	if err := validateManagedResourceIdentity(input.Identity); err != nil {
		return nil, err
	}
	fingerprint, err := normalizeManagedResourceFingerprint(input.Fingerprint, input.Identity)
	if err != nil {
		return nil, err
	}
	now := r.currentTime()
	observedAt := input.ObservedAt
	if observedAt.IsZero() {
		observedAt = now
	}
	observedAt = observedAt.UTC()
	row := gbmodels.GbZLMManagedResource{
		NodeID:              input.Identity.NodeID,
		ResourceType:        input.Identity.ResourceType,
		ResourceKey:         input.Identity.ResourceKey,
		App:                 input.Identity.App,
		Stream:              input.Identity.Stream,
		IdentityFingerprint: fingerprint,
		Summary:             RedactManagedResourceSummary(input.Summary),
		CreatedBy:           input.CreatedBy,
		CreatedAt:           now,
		LastObservedAt:      &observedAt,
		UpdatedAt:           now,
	}

	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing gbmodels.GbZLMManagedResource
		result := tx.Where("node_id = ? AND resource_type = ? AND resource_key = ?", input.Identity.NodeID, input.Identity.ResourceType, input.Identity.ResourceKey).
			Limit(1).Find(&existing)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return tx.Create(&row).Error
		}
		return updateManagedResource(tx, input.Identity, row)
	})
	if err != nil {
		// A concurrent registration may win the insert race. The unique identity
		// key makes the loser safe to retry as the update branch.
		if !isManagedResourceUniqueConflict(err) {
			return nil, err
		}
		if err := updateManagedResource(r.db.WithContext(ctx), input.Identity, row); err != nil {
			return nil, err
		}
	}
	return r.Find(ctx, input.Identity)
}

func updateManagedResource(db *gorm.DB, identity ManagedResourceIdentity, row gbmodels.GbZLMManagedResource) error {
	result := db.Model(&gbmodels.GbZLMManagedResource{}).
		Where("node_id = ? AND resource_type = ? AND resource_key = ?", identity.NodeID, identity.ResourceType, identity.ResourceKey).
		Updates(map[string]any{
			"app":                  row.App,
			"stream":               row.Stream,
			"identity_fingerprint": row.IdentityFingerprint,
			"summary":              row.Summary,
			"last_observed_at":     row.LastObservedAt,
			"tombstoned_at":        nil,
			"updated_at":           row.UpdatedAt,
		})
	return result.Error
}

func isManagedResourceUniqueConflict(err error) bool {
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"duplicate", "unique constraint", "unique key", "violates unique", "2627", "2601", "1062", "23505",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

// Find returns provenance metadata. It does not assert that the resource exists
// in ZLM; callers must reconcile this row with the live ZLM list.
func (r *ManagedResourceRepo) Find(ctx context.Context, identity ManagedResourceIdentity) (*gbmodels.GbZLMManagedResource, error) {
	if err := validateManagedResourceIdentity(identity); err != nil {
		return nil, err
	}
	var row gbmodels.GbZLMManagedResource
	result := r.db.WithContext(ctx).
		Where("node_id = ? AND resource_type = ? AND resource_key = ?", identity.NodeID, identity.ResourceType, identity.ResourceKey).
		Limit(1).Find(&row)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrManagedResourceNotFound
	}
	return &row, nil
}

// Observe confirms that a previously managed resource is present again. An
// unknown resource is never inserted by observation, preserving unknown/fail-closed semantics.
func (r *ManagedResourceRepo) Observe(ctx context.Context, identity ManagedResourceIdentity, observedAt time.Time) (*gbmodels.GbZLMManagedResource, error) {
	if _, err := r.Find(ctx, identity); err != nil {
		return nil, err
	}
	if observedAt.IsZero() {
		observedAt = r.currentTime()
	}
	observedAt = observedAt.UTC()
	result := r.db.WithContext(ctx).Model(&gbmodels.GbZLMManagedResource{}).
		Where("node_id = ? AND resource_type = ? AND resource_key = ?", identity.NodeID, identity.ResourceType, identity.ResourceKey).
		Updates(map[string]any{
			"last_observed_at": observedAt,
			"tombstoned_at":    nil,
			"updated_at":       r.currentTime(),
		})
	if result.Error != nil {
		return nil, result.Error
	}
	return r.Find(ctx, identity)
}

// Tombstone marks a managed resource absent while retaining the ledger row.
// Repeated tombstones are idempotent and retain the first confirmed timestamp.
func (r *ManagedResourceRepo) Tombstone(ctx context.Context, identity ManagedResourceIdentity, tombstonedAt time.Time) (*gbmodels.GbZLMManagedResource, error) {
	if _, err := r.Find(ctx, identity); err != nil {
		return nil, err
	}
	if tombstonedAt.IsZero() {
		tombstonedAt = r.currentTime()
	}
	tombstonedAt = tombstonedAt.UTC()
	result := r.db.WithContext(ctx).Model(&gbmodels.GbZLMManagedResource{}).
		Where("node_id = ? AND resource_type = ? AND resource_key = ?", identity.NodeID, identity.ResourceType, identity.ResourceKey).
		Updates(map[string]any{
			"tombstoned_at": gorm.Expr("COALESCE(tombstoned_at, ?)", tombstonedAt),
			"updated_at":    r.currentTime(),
		})
	if result.Error != nil {
		return nil, result.Error
	}
	return r.Find(ctx, identity)
}

// MarkTombstone is the explicit alias used by callers that model state changes
// as a ledger event.
func (r *ManagedResourceRepo) MarkTombstone(ctx context.Context, identity ManagedResourceIdentity, tombstonedAt time.Time) (*gbmodels.GbZLMManagedResource, error) {
	return r.Tombstone(ctx, identity, tombstonedAt)
}

// List returns provenance rows, never live ZLM state. By default tombstoned
// rows are hidden; IncludeTombstoned is intended for audit/reconciliation views.
func (r *ManagedResourceRepo) List(ctx context.Context, filter ManagedResourceFilter) ([]gbmodels.GbZLMManagedResource, error) {
	query := r.db.WithContext(ctx).Model(&gbmodels.GbZLMManagedResource{})
	if filter.NodeID != 0 {
		query = query.Where("node_id = ?", filter.NodeID)
	}
	if strings.TrimSpace(filter.ResourceType) != "" {
		if err := validateResourceType(filter.ResourceType); err != nil {
			return nil, err
		}
		query = query.Where("resource_type = ?", strings.TrimSpace(filter.ResourceType))
	}
	if !filter.IncludeTombstoned {
		query = query.Where("tombstoned_at IS NULL")
	}
	var rows []gbmodels.GbZLMManagedResource
	if err := query.Order("node_id").Order("resource_type").Order("resource_key").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// FingerprintManagedResource hashes the safe identity tuple. It is suitable for
// identities that do not contain secrets; sensitive source/target values should
// be supplied to FingerprintManagedResourceParts instead.
func FingerprintManagedResource(identity ManagedResourceIdentity) string {
	return FingerprintManagedResourceParts(
		strconv.FormatInt(identity.NodeID, 10), identity.ResourceType,
		identity.ResourceKey, identity.App, identity.Stream,
	)
}

// FingerprintManagedResourceParts returns a SHA-256 digest of length-delimited
// parts. It never persists or logs the supplied values.
func FingerprintManagedResourceParts(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = fmt.Fprintf(h, "%d:", len(part))
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{'|'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

var sensitiveSummaryURL = regexp.MustCompile(`(?i)\b(?:rtsp|rtmp|http|https|ws|wss|srt)://[^\s"'<>]+`)
var sensitiveSummaryKV = regexp.MustCompile(`(?i)\b(password|passwd|secret|token|access[_-]?token|api[_-]?key|authorization|credential)=([^\s,;]+)`)
var sensitiveSummaryJSON = regexp.MustCompile(`(?i)["']?(?:password|passwd|secret|token|access[_-]?token|api[_-]?key|authorization|credential)["']?\s*[:=]\s*["']?[^"',}\s]+["']?`)
var sensitiveSummaryUserInfo = regexp.MustCompile(`(?i)\b[^\s/@:]+:[^\s/@]+@`)
var sensitiveSummaryAuthHeader = regexp.MustCompile(`(?i)\b(?:authorization\s*[:=]\s*|bearer\s+|basic\s+)[^\s,;]+`)

// RedactManagedResourceSummary keeps a short operational hint while removing
// credentials, query tokens, and URL paths. It is intentionally conservative.
func RedactManagedResourceSummary(summary string) string {
	redacted := strings.TrimSpace(summary)
	redacted = sensitiveSummaryURL.ReplaceAllStringFunc(redacted, func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme == "" || u.Hostname() == "" {
			return "<redacted-url>"
		}
		return u.Scheme + "://" + u.Hostname()
	})
	redacted = sensitiveSummaryKV.ReplaceAllString(redacted, "<redacted>")
	redacted = sensitiveSummaryJSON.ReplaceAllString(redacted, "<redacted>")
	redacted = sensitiveSummaryAuthHeader.ReplaceAllString(redacted, "<redacted>")
	redacted = sensitiveSummaryUserInfo.ReplaceAllString(redacted, "<redacted>@")
	redacted = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, redacted)
	if len(redacted) > 512 {
		limit := 0
		for _, r := range redacted {
			size := utf8.RuneLen(r)
			if limit+size > 512 {
				break
			}
			limit += size
		}
		redacted = redacted[:limit]
	}
	return redacted
}

func normalizeManagedResourceFingerprint(input string, identity ManagedResourceIdentity) (string, error) {
	if strings.TrimSpace(input) == "" {
		return FingerprintManagedResource(identity), nil
	}
	fingerprint := strings.TrimSpace(input)
	if len(fingerprint) != sha256.Size*2 {
		return "", fmt.Errorf("%w: fingerprint must be a SHA-256 hex digest", ErrManagedResourceInvalidInput)
	}
	if _, err := hex.DecodeString(fingerprint); err != nil {
		return "", fmt.Errorf("%w: fingerprint must be a SHA-256 hex digest", ErrManagedResourceInvalidInput)
	}
	return strings.ToLower(fingerprint), nil
}

func validateManagedResourceIdentity(identity ManagedResourceIdentity) error {
	if identity.NodeID <= 0 {
		return fmt.Errorf("%w: node id must be positive", ErrManagedResourceInvalidInput)
	}
	if err := validateResourceType(identity.ResourceType); err != nil {
		return err
	}
	if err := validateSafeIdentityPart("resource key", identity.ResourceKey, 255, true); err != nil {
		return err
	}
	if err := validateSafeIdentityPart("app", identity.App, 64, false); err != nil {
		return err
	}
	return validateSafeIdentityPart("stream", identity.Stream, 255, false)
}

func validateResourceType(value string) error {
	return validateSafeIdentityPart("resource type", value, 32, false)
}

func validateSafeIdentityPart(name, value string, maxBytes int, rejectURL bool) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%w: %s must not be empty", ErrManagedResourceInvalidInput, name)
	}
	if len(value) > maxBytes {
		return fmt.Errorf("%w: %s is too long", ErrManagedResourceInvalidInput, name)
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("%w: %s contains control characters", ErrManagedResourceInvalidInput, name)
		}
	}
	if rejectURL && looksSensitiveResourceValue(value) {
		return fmt.Errorf("%w: %s cannot contain a URL or credential", ErrManagedResourceSensitiveData, name)
	}
	return nil
}

func looksSensitiveResourceValue(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{
		"://", "password=", "passwd=", "secret=", "token=", "access_token=", "api_key=", "apikey=", "authorization=", "credential=",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func (r *ManagedResourceRepo) currentTime() time.Time {
	if r.now == nil {
		return time.Now()
	}
	return r.now()
}
