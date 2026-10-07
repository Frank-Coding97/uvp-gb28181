package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gorm.io/gorm"

	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	catalogstore "uvplatform.com/uvp-gb28181/app/openapi/catalog/store"
)

var (
	// ErrBootstrapDraftEmpty means the editable catalog yielded nothing to
	// publish. An empty release is not a harmless no-op: it becomes the
	// authoritative scope set, so every scope the platform used to authorize
	// would stop being dispatchable. Fail instead of publishing it.
	ErrBootstrapDraftEmpty = errors.New("OpenAPI catalog draft has no dispatchable operation")
)

// SystemActorID is the audit identity used when the platform itself, rather
// than a logged-in operator, creates the initial catalog rows and the initial
// publication. 0 is the column default for every created_by in the catalog
// tables, so it already means "not a human" in this schema.
const SystemActorID int64 = 0

// Release names. The startup path only ever revises a release to close a gap
// between the code-owned surface and the published one; the explicit operator
// entry point names its own.
const (
	InitialReleaseName  = "初始发布"
	RevisionReleaseName = "核心能力补齐"
	OperatorReleaseName = "人工发布"
)

// retiredCoreScopes are contracts removed before the platform's first
// external release. They may be removed from an active snapshot automatically
// because there is no compatibility promise to preserve yet.
var retiredCoreScopes = map[string]struct{}{
	// The former pre-release playback-authorization contract was never exposed
	// to external consumers. Replace it automatically with play:live when an
	// installation still has the old active release.
	"play:live:apply": {},
}

// Publish actions reported by EnsurePublishedCatalog / PublishCurrentCatalog.
const (
	// PublishActionExisting: an active release was already published and the
	// editable catalog would not change it. Nothing was seeded, published or
	// version-bumped.
	PublishActionExisting = "already-published"
	// PublishActionPublished: no release was active and this call published the
	// first one.
	PublishActionPublished = "published"
	// PublishActionRevised: a release was already active and this call
	// published a new version on top of it.
	PublishActionRevised = "revised"
)

// PublishOutcome describes what the publication entry points left behind.
type PublishOutcome struct {
	Action string
	// Seeded counts the rows this call created. It is zero for a plain
	// PublishActionExisting because a complete publication is not re-seeded.
	Seeded BootstrapResult
	// ReleaseID, Version and ItemCount describe the release that is active
	// after the call — whether it was published now or was already there.
	ReleaseID int64
	Version   int64
	ItemCount int
	// RevisedFrom is the version this call superseded. Zero when nothing was
	// superseded.
	RevisedFrom int64
	// CoreScopesMissing lists code-owned scopes the active release still does
	// not contain after the call. Every non-empty value means an operator has
	// to act: the scopes exist in this binary but cannot be granted, because
	// client.ScopePublished reads the release.
	CoreScopesMissing []string
}

// EnsurePublishedCatalog guarantees that a migrated installation has an active
// release that covers the code-owned scope surface.
//
// It is idempotent in the strongest sense for a healthy installation: when the
// active release already contains every code-owned scope, nothing is seeded,
// published or version-bumped, so restarting a live deployment cannot roll its
// contract forward.
//
// The one automatic mutation is a strictly additive repair. A release that
// predates part of the surface — an installation that published before this
// binary learned about a scope, for example — is exactly as broken as an
// installation that never published: the extra scopes appear in the
// client-facing capability catalog but client.ScopePublished rejects them, so
// they can never be granted. Startup closes that gap only when the repair
// cannot remove anything: if the current editable catalog no longer covers a
// published scope, the release is left untouched and the gap is reported
// through CoreScopesMissing instead. Dropping a published scope stays a
// deliberate operator action, which is what -publish-catalog
// (PublishCurrentCatalog) is for. The removed pre-release playback capability
// is retired from the editable catalog by EnsureCoreCatalog; no replacement
// external playback contract is published.
//
// Publication goes through store.Publisher, which validates the draft against
// the process adapter registry before any write. A release can therefore only
// ever contain operations this binary is able to dispatch; neither the seed nor
// a revision can invent a capability that has no adapter.
func EnsurePublishedCatalog(ctx context.Context, db *gorm.DB, actorID int64, runtime *catalogruntime.CatalogRuntime) (PublishOutcome, error) {
	repository, err := publishRepository(db, actorID)
	if err != nil {
		return PublishOutcome{}, err
	}
	ctx = normalizeContext(ctx)

	active, err := repository.LoadActive(ctx)
	switch {
	case err == nil:
		return reconcilePublishedCatalog(ctx, db, actorID, runtime, repository, active)
	case errors.Is(err, catalogstore.ErrNoActiveRelease):
		// Never published: seed and publish the first release below.
	default:
		// A declared active pointer that cannot be resolved, an incomplete
		// schema or an unreadable runtime state stays fatal. Only the
		// "no release at all" state is repaired automatically.
		return PublishOutcome{}, err
	}

	seeded, draft, err := seedAndBuildDraft(ctx, db, actorID)
	if err != nil {
		return PublishOutcome{}, err
	}
	snapshot, err := publishDraft(ctx, repository, runtime, draft, InitialReleaseName, actorID)
	if err != nil {
		return PublishOutcome{}, err
	}
	return PublishOutcome{
		Action:    PublishActionPublished,
		Seeded:    seeded,
		ReleaseID: snapshot.Release.ID,
		Version:   snapshot.Release.Version,
		ItemCount: len(snapshot.Items),
	}, nil
}

// PublishCurrentCatalog publishes the current editable catalog as it stands.
// It is the explicit operator entry point behind `-publish-catalog`.
//
// Unlike the startup path it is allowed to shrink the surface: a scope the
// operator removed from the editable catalog disappears from the new release,
// because that is what publishing means. It is still idempotent — a catalog
// whose publishable surface equals the active release does not produce a new
// version — so running it twice, or on a healthy installation, is a no-op.
func PublishCurrentCatalog(ctx context.Context, db *gorm.DB, actorID int64, runtime *catalogruntime.CatalogRuntime) (PublishOutcome, error) {
	repository, err := publishRepository(db, actorID)
	if err != nil {
		return PublishOutcome{}, err
	}
	ctx = normalizeContext(ctx)

	var active *catalogstore.ReleaseSnapshot
	activeSnapshot, err := repository.LoadActive(ctx)
	switch {
	case err == nil:
		active = activeSnapshot
	case errors.Is(err, catalogstore.ErrNoActiveRelease):
	default:
		return PublishOutcome{}, err
	}

	seeded, draft, err := seedAndBuildDraft(ctx, db, actorID)
	if err != nil {
		return PublishOutcome{}, err
	}
	if active != nil {
		existing := PublishOutcome{
			Action:    PublishActionExisting,
			Seeded:    seeded,
			ReleaseID: active.Release.ID,
			Version:   active.Release.Version,
			ItemCount: len(active.Items),
		}
		if draftSurfaceSignature(draft) == draftSurfaceSignature(active.Draft()) {
			return existing, nil
		}
	}
	snapshot, err := publishDraft(ctx, repository, runtime, draft, OperatorReleaseName, actorID)
	if err != nil {
		return PublishOutcome{}, err
	}
	outcome := PublishOutcome{
		Action:    PublishActionPublished,
		Seeded:    seeded,
		ReleaseID: snapshot.Release.ID,
		Version:   snapshot.Release.Version,
		ItemCount: len(snapshot.Items),
	}
	if active != nil {
		outcome.Action = PublishActionRevised
		outcome.RevisedFrom = active.Release.Version
	}
	return outcome, nil
}

// reconcilePublishedCatalog closes a code-owned gap without ever removing a
// published scope.
func reconcilePublishedCatalog(ctx context.Context, db *gorm.DB, actorID int64, runtime *catalogruntime.CatalogRuntime, repository *catalogstore.Repository, active *catalogstore.ReleaseSnapshot) (PublishOutcome, error) {
	ctx = normalizeContext(ctx)
	existing := PublishOutcome{
		Action:    PublishActionExisting,
		ReleaseID: active.Release.ID,
		Version:   active.Release.Version,
		ItemCount: len(active.Items),
	}
	missing := missingCoreScopes(active)
	retired := activeRetiredCoreScopes(active)
	groupDrift, err := coreCatalogGroupDrift(ctx, db, active)
	if err != nil {
		return PublishOutcome{}, err
	}
	if len(missing) == 0 && len(retired) == 0 && !groupDrift {
		return existing, nil
	}
	existing.CoreScopesMissing = missing

	seeded, draft, err := seedAndBuildDraft(ctx, db, actorID)
	if err != nil {
		return PublishOutcome{}, err
	}
	existing.Seeded = seeded
	// A repair that would drop any non-retired published scope is not a repair.
	// Leave the release exactly as it is and keep reporting the gap.
	if !draftCoversScopes(draft, publishedScopesExcludingRetired(active)) {
		return existing, nil
	}
	if len(retired) == 0 && draftSurfaceSignature(draft) == draftSurfaceSignature(active.Draft()) && !groupDrift {
		// The rows exist but produce the same surface — a disabled or
		// soft-deleted core row is kept out of the draft by catalogRows.
		// Report rather than publish a release that would not close the gap.
		return existing, nil
	}
	snapshot, err := publishDraft(ctx, repository, runtime, draft, RevisionReleaseName, actorID)
	if err != nil {
		return PublishOutcome{}, err
	}
	return PublishOutcome{
		Action:      PublishActionRevised,
		Seeded:      seeded,
		ReleaseID:   snapshot.Release.ID,
		Version:     snapshot.Release.Version,
		ItemCount:   len(snapshot.Items),
		RevisedFrom: active.Release.Version,
	}, nil
}

// coreCatalogGroupDrift detects a metadata-only publication change that the
// runtime draft intentionally does not carry: a code-owned capability moved
// between categories. The scope set is unchanged, but the admin capability
// catalog must still publish the new grouping for readers of the immutable
// release snapshot.
func coreCatalogGroupDrift(ctx context.Context, db *gorm.DB, active *catalogstore.ReleaseSnapshot) (bool, error) {
	if db == nil || active == nil {
		return false, nil
	}
	type capabilityGroupRow struct {
		Scope     string `gorm:"column:scope"`
		GroupCode string `gorm:"column:group_code"`
	}
	var rows []capabilityGroupRow
	query := db.WithContext(normalizeContext(ctx)).Table("sys_openapi_capability AS c").
		Select("c.scope, g.code AS group_code").
		Joins("JOIN sys_openapi_capability_group AS g ON g.id = c.group_id AND g.deleted_at IS NULL").
		Where("c.deleted_at IS NULL AND c.scope IN ?", CoreCatalogScopes()).Find(&rows)
	if query.Error != nil {
		return false, query.Error
	}
	current := make(map[string]string, len(rows))
	for _, row := range rows {
		current[strings.TrimSpace(row.Scope)] = strings.TrimSpace(row.GroupCode)
	}
	for _, item := range active.Items {
		if expected, ok := current[strings.TrimSpace(item.Scope)]; ok && expected != strings.TrimSpace(item.GroupCode) {
			return true, nil
		}
	}
	return false, nil
}

func publishRepository(db *gorm.DB, actorID int64) (*catalogstore.Repository, error) {
	if db == nil {
		return nil, ErrBootstrapUnavailable
	}
	if actorID < SystemActorID {
		return nil, fmt.Errorf("%w: actor id must not be negative", ErrBootstrapConflict)
	}
	return catalogstore.NewRepository(db), nil
}

func seedAndBuildDraft(ctx context.Context, db *gorm.DB, actorID int64) (BootstrapResult, catalogruntime.Draft, error) {
	seeded, err := EnsureCoreCatalog(ctx, db, actorID)
	if err != nil {
		return BootstrapResult{}, catalogruntime.Draft{}, err
	}
	draft, err := catalogstore.NewRepository(db).BuildDraft(ctx)
	if err != nil {
		return BootstrapResult{}, catalogruntime.Draft{}, err
	}
	if len(draft.Operations) == 0 {
		return BootstrapResult{}, catalogruntime.Draft{}, ErrBootstrapDraftEmpty
	}
	return seeded, draft, nil
}

func publishDraft(ctx context.Context, repository *catalogstore.Repository, runtime *catalogruntime.CatalogRuntime, draft catalogruntime.Draft, name string, actorID int64) (*catalogstore.ReleaseSnapshot, error) {
	publishedBy := uint(actorID)
	return catalogstore.NewPublisher(repository, runtime).Publish(ctx, draft, catalogstore.PublishOptions{
		Name:        name,
		PublishedBy: &publishedBy,
		CreatedBy:   uint(actorID),
	})
}

// missingCoreScopes returns the code-owned scopes the active release does not
// publish, sorted. It is the only trigger for an automatic revision.
func missingCoreScopes(active *catalogstore.ReleaseSnapshot) []string {
	published := publishedScopeSet(active)
	missing := make([]string, 0)
	for _, scope := range CoreCatalogScopes() {
		if _, ok := published[scope]; !ok {
			missing = append(missing, scope)
		}
	}
	sort.Strings(missing)
	return missing
}

func publishedScopes(active *catalogstore.ReleaseSnapshot) []string {
	published := publishedScopeSet(active)
	scopes := make([]string, 0, len(published))
	for scope := range published {
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)
	return scopes
}

func publishedScopesExcludingRetired(active *catalogstore.ReleaseSnapshot) []string {
	scopes := publishedScopes(active)
	filtered := scopes[:0]
	for _, scope := range scopes {
		if _, ok := retiredCoreScopes[scope]; !ok {
			filtered = append(filtered, scope)
		}
	}
	return filtered
}

func activeRetiredCoreScopes(active *catalogstore.ReleaseSnapshot) []string {
	if active == nil {
		return nil
	}
	seen := make(map[string]struct{})
	for _, item := range active.Items {
		if _, ok := retiredCoreScopes[strings.TrimSpace(item.Scope)]; ok {
			seen[strings.TrimSpace(item.Scope)] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for scope := range seen {
		result = append(result, scope)
	}
	sort.Strings(result)
	return result
}

func publishedScopeSet(active *catalogstore.ReleaseSnapshot) map[string]struct{} {
	if active == nil {
		return make(map[string]struct{})
	}
	published := make(map[string]struct{}, len(active.Items))
	for _, item := range active.Items {
		if scope := strings.TrimSpace(item.Scope); scope != "" {
			published[scope] = struct{}{}
		}
	}
	return published
}

// draftCoversScopes reports whether the draft still publishes every scope in
// the given set. The startup repair uses it to prove it cannot shrink the
// contract.
func draftCoversScopes(draft catalogruntime.Draft, scopes []string) bool {
	present := make(map[string]struct{}, len(draft.Operations))
	for _, operation := range draft.Operations {
		present[strings.TrimSpace(operation.Scope)] = struct{}{}
	}
	for _, scope := range scopes {
		if _, ok := present[strings.TrimSpace(scope)]; !ok {
			return false
		}
	}
	return true
}

// draftSurfaceSignature canonicalizes the publishable surface of a draft so a
// candidate publication can be compared with the active release without
// writing anything. Both sides are built as catalogruntime.Draft, so the
// comparison is symmetric: the active release is compared through
// ReleaseSnapshot.Draft().
//
// Fields the runtime draft does not carry (resource type, request/response
// schemas) are intentionally out of scope: they are seed-owned with no editing
// path, and catalog.store.releaseItems already refuses a draft that disagrees
// with its source row on any field it does carry.
func draftSurfaceSignature(draft catalogruntime.Draft) string {
	entries := make([]string, 0, len(draft.Operations))
	for _, operation := range draft.Operations {
		entries = append(entries, strings.Join([]string{
			strings.TrimSpace(operation.Scope),
			strings.ToUpper(strings.TrimSpace(operation.Method)),
			strings.TrimSpace(operation.ExternalPath),
			strings.TrimSpace(operation.AdapterKey),
			strings.TrimSpace(operation.ContractVersion),
			strings.TrimSpace(operation.Name),
			strings.TrimSpace(operation.Risk),
			strings.TrimSpace(operation.SysAPIPath),
			strings.ToUpper(strings.TrimSpace(operation.SysAPIMethod)),
			strconv.FormatUint(uint64(operation.SysAPIID), 10),
		}, "\x1f"))
	}
	sort.Strings(entries)
	return strings.Join(entries, "\x1e")
}
