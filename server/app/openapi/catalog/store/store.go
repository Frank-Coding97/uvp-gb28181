// Package store persists the editable OpenAPI capability catalog and its
// immutable publication snapshots.
package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	openapimodels "uvplatform.com/uvp-gb28181/app/openapi/models"
)

var (
	// ErrCatalogTableMissing means the database has not been migrated with one
	// of the tables needed by this repository.
	ErrCatalogTableMissing = errors.New("OpenAPI catalog table is missing")
	// ErrCatalogDependency means a catalog row points at a missing or invalid
	// parent row, or a persisted runtime pointer is inconsistent.
	ErrCatalogDependency = errors.New("OpenAPI catalog dependency is invalid")
	// ErrCatalogRepositoryUnavailable means the repository was built without a
	// database handle.
	ErrCatalogRepositoryUnavailable = errors.New("OpenAPI catalog repository is unavailable")
	// ErrReleaseNotFound means a requested release does not exist.
	ErrReleaseNotFound = errors.New("OpenAPI catalog release not found")
	// ErrDraftOperationNotFound means a publication draft no longer matches an
	// editable operation row.
	ErrDraftOperationNotFound = errors.New("OpenAPI catalog draft operation not found")
	// ErrCatalogRuntimeUnavailable means the persisted active pointer is not
	// ready for request dispatch.
	ErrCatalogRuntimeUnavailable = errors.New("OpenAPI catalog runtime unavailable")
)

// ErrNoActiveRelease is exported as an alias so callers do not need to know
// whether the active pointer came from the in-memory runtime or persistence.
var ErrNoActiveRelease = catalogruntime.ErrNoActiveRelease

// CatalogSchemaStatus distinguishes an old installation with no OpenAPI
// catalog tables from a partially migrated or fully migrated installation.
// Only the absent state is eligible for legacy static-registry compatibility.
type CatalogSchemaStatus string

const (
	CatalogSchemaAbsent     CatalogSchemaStatus = "absent"
	CatalogSchemaIncomplete CatalogSchemaStatus = "incomplete"
	CatalogSchemaComplete   CatalogSchemaStatus = "complete"
)

// The immutable runtime projection is the minimum schema needed by readers.
// Editable source tables are intentionally not required here because a
// published release remains dispatchable without joining them.
var catalogSchemaModels = []any{
	&openapimodels.Release{},
	&openapimodels.ReleaseItem{},
	&openapimodels.RuntimeState{},
}

const runtimeStateID int64 = 1

// Repository reads editable catalog rows and persists immutable releases.
// The same database is used for source rows, release snapshots and the
// singleton runtime pointer; no separate catalog database is required.
type Repository struct {
	db  *gorm.DB
	now func() time.Time
}

// NewRepository creates a catalog repository. It does not run migrations.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db, now: func() time.Time { return time.Now().UTC() }}
}

// InspectCatalogSchema reports whether the catalog schema is absent, partial,
// or complete. GORM's Migrator does not expose a portable error from HasTable;
// a nil database is therefore the only repository-level error here.
func InspectCatalogSchema(db *gorm.DB) (CatalogSchemaStatus, error) {
	if db == nil {
		return CatalogSchemaAbsent, ErrCatalogRepositoryUnavailable
	}
	present := 0
	for _, model := range catalogSchemaModels {
		if db.Migrator().HasTable(model) {
			present++
		}
	}
	if present == 0 {
		return CatalogSchemaAbsent, nil
	}
	if present != len(catalogSchemaModels) {
		return CatalogSchemaIncomplete, nil
	}
	return CatalogSchemaComplete, nil
}

// PublishOptions controls release metadata. Version zero allocates the next
// monotonically increasing database version.
type PublishOptions struct {
	Version     int64
	Name        string
	PublishedBy *uint
	CreatedBy   uint
}

// ReleaseSnapshot is the durable form of one immutable publication. Items are
// copied on load so callers cannot mutate the returned snapshot in place.
type ReleaseSnapshot struct {
	Release         openapimodels.Release
	Items           []openapimodels.ReleaseItem
	TombstoneScopes []string
	// RuntimeIdentity is populated only by LoadActive. It records the exact
	// durable pointer that was read alongside the immutable release snapshot.
	RuntimeIdentity catalogruntime.RuntimeIdentity
}

// Draft converts an immutable release snapshot back into the runtime draft
// shape used to hydrate a process-local catalog.
func (s *ReleaseSnapshot) Draft() catalogruntime.Draft {
	if s == nil {
		return catalogruntime.Draft{}
	}
	result := catalogruntime.Draft{
		Version:         strconv.FormatInt(s.Release.Version, 10),
		Operations:      make([]catalogruntime.Operation, 0, len(s.Items)),
		TombstoneScopes: append([]string(nil), s.TombstoneScopes...),
	}
	for _, item := range s.Items {
		operationName := item.CapabilityName
		if snapshot, err := decodeSnapshot(item.SnapshotJSON); err == nil && snapshot.OperationName != "" {
			operationName = snapshot.OperationName
		}
		operation := catalogruntime.Operation{
			Scope:           item.Scope,
			Name:            operationName,
			Method:          strings.ToUpper(item.Method),
			ExternalPath:    item.ExternalPath,
			AdapterKey:      item.AdapterKey,
			ContractVersion: normalizeContractVersion(item.AdapterContractVersion),
			SysAPIPath:      item.SysAPIPath,
			SysAPIMethod:    strings.ToUpper(item.SysAPIMethod),
			Risk:            item.RiskLevel,
		}
		if item.SysAPIID != nil && *item.SysAPIID > 0 {
			operation.SysAPIID = uint(*item.SysAPIID)
		}
		result.Operations = append(result.Operations, operation)
	}
	return result
}

// BuildDraft builds a runtime draft from all non-deleted editable catalog
// rows. Disabled groups, capabilities and operations are retained in the
// database but excluded from the next publication. Client scope rows that no
// longer have a dispatchable operation are returned as orphan scopes.
func (r *Repository) BuildDraft(ctx context.Context) (catalogruntime.Draft, error) {
	if r == nil || r.db == nil {
		return catalogruntime.Draft{}, ErrCatalogRepositoryUnavailable
	}
	ctx = normalizeStoreContext(ctx)
	rows, err := r.loadRows(ctx, r.db)
	if err != nil {
		return catalogruntime.Draft{}, err
	}

	operations, knownScopes, err := rows.runtimeOperations()
	if err != nil {
		return catalogruntime.Draft{}, err
	}
	orphans, err := r.loadOrphanScopes(ctx, r.db, knownScopes)
	if err != nil {
		return catalogruntime.Draft{}, err
	}
	return catalogruntime.Draft{Operations: operations, OrphanScopes: orphans}, nil
}

// LoadActive loads the release referenced by the singleton runtime state.
func (r *Repository) LoadActive(ctx context.Context) (*ReleaseSnapshot, error) {
	if r == nil || r.db == nil {
		return nil, ErrCatalogRepositoryUnavailable
	}
	ctx = normalizeStoreContext(ctx)
	state, err := r.loadRuntimeState(ctx, r.db)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNoActiveRelease
		}
		return nil, err
	}
	if state.ActiveRelease == nil || *state.ActiveRelease == 0 {
		return nil, ErrNoActiveRelease
	}
	if state.Status != openapimodels.RuntimeCatalogReady {
		return nil, fmt.Errorf("%w: status=%s", ErrCatalogRuntimeUnavailable, state.Status)
	}
	if state.RuntimeEpoch <= 0 {
		return nil, fmt.Errorf("%w: runtime epoch is not positive", ErrCatalogRuntimeUnavailable)
	}
	snapshot, err := r.loadRelease(ctx, r.db, *state.ActiveRelease)
	if err != nil {
		if errors.Is(err, ErrReleaseNotFound) {
			return nil, fmt.Errorf("%w: active release %d", ErrCatalogDependency, *state.ActiveRelease)
		}
		return nil, err
	}
	if snapshot.Release.Status != openapimodels.ReleaseStatusPublished {
		return nil, fmt.Errorf("%w: active release %d has status %s", ErrCatalogDependency, snapshot.Release.ID, snapshot.Release.Status)
	}
	if state.ActiveVersion <= 0 || state.ActiveVersion != snapshot.Release.Version {
		return nil, fmt.Errorf("%w: active version %d points to release version %d", ErrCatalogDependency, state.ActiveVersion, snapshot.Release.Version)
	}
	if strings.TrimSpace(state.SnapshotHash) == "" || !strings.EqualFold(strings.TrimSpace(state.SnapshotHash), snapshot.Release.SnapshotHash) {
		return nil, fmt.Errorf("%w: active state snapshot hash mismatch", ErrCatalogDependency)
	}
	// Re-read the singleton pointer after loading the release and all items.
	// Without this second read, a concurrent publication could combine the old
	// state row with a newer release snapshot and hydrate a contract that was
	// never atomically active.
	confirmed, confirmErr := r.loadRuntimeState(ctx, r.db)
	if errors.Is(confirmErr, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: active runtime state disappeared", ErrCatalogRuntimeUnavailable)
	}
	if confirmErr != nil {
		return nil, confirmErr
	}
	if !sameRuntimeStateIdentity(state, confirmed) {
		return nil, fmt.Errorf("%w: runtime state changed while loading active release", ErrCatalogRuntimeUnavailable)
	}
	if validateErr := validateRuntimeStateSnapshot(confirmed, snapshot); validateErr != nil {
		return nil, validateErr
	}
	snapshot.RuntimeIdentity = runtimeIdentityFromState(confirmed)
	return snapshot, nil
}

// LoadRelease loads one immutable release and all of its operation snapshots.
func (r *Repository) LoadRelease(ctx context.Context, releaseID int64) (*ReleaseSnapshot, error) {
	if r == nil || r.db == nil {
		return nil, ErrCatalogRepositoryUnavailable
	}
	ctx = normalizeStoreContext(ctx)
	return r.loadRelease(ctx, r.db, releaseID)
}

// Hydrate loads the persisted active release into a process-local runtime.
// Only immutable release items are used; editable draft rows and sys_api are
// never joined during request dispatch. The current client-scope rows are
// inspected solely to retain historical orphan scopes as non-dispatchable
// compatibility metadata.
func (r *Repository) Hydrate(ctx context.Context, runtime *catalogruntime.CatalogRuntime) error {
	if r == nil || r.db == nil || runtime == nil {
		return ErrCatalogRepositoryUnavailable
	}
	ctx = normalizeStoreContext(ctx)
	snapshot, err := r.LoadActive(ctx)
	if err != nil {
		return err
	}
	draft := snapshot.Draft()
	known := make(map[string]struct{}, len(draft.Operations))
	for _, operation := range draft.Operations {
		known[operation.Scope] = struct{}{}
	}
	orphans, err := r.loadOrphanScopes(ctx, r.db, known)
	if err != nil {
		return err
	}
	tombstones := make(map[string]struct{}, len(draft.TombstoneScopes))
	for _, scope := range draft.TombstoneScopes {
		tombstones[strings.TrimSpace(scope)] = struct{}{}
	}
	filteredOrphans := orphans[:0]
	for _, scope := range orphans {
		if _, tombstoned := tombstones[scope]; tombstoned {
			continue
		}
		filteredOrphans = append(filteredOrphans, scope)
	}
	draft.OrphanScopes = filteredOrphans
	if _, report, publishErr := runtime.Publish(ctx, draft); publishErr != nil {
		if !report.Valid() {
			return fmt.Errorf("%w: %v", ErrCatalogDependency, publishErr)
		}
		return publishErr
	}
	if bindErr := runtime.BindIdentity(snapshot.RuntimeIdentity); bindErr != nil {
		return fmt.Errorf("%w: bind durable runtime identity: %v", ErrCatalogDependency, bindErr)
	}
	return nil
}

// Publish atomically creates a release and complete release-item snapshots,
// marks included capabilities active, and moves the singleton active pointer.
// Any failure rolls back the release row, items, source status updates and
// active pointer together.
func (r *Repository) Publish(ctx context.Context, draft catalogruntime.Draft, options PublishOptions) (*ReleaseSnapshot, error) {
	if r == nil || r.db == nil {
		return nil, ErrCatalogRepositoryUnavailable
	}
	ctx = normalizeStoreContext(ctx)
	var published *ReleaseSnapshot
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		state, stateErr := r.loadRuntimeStateForPublish(ctx, tx)
		if stateErr != nil && !errors.Is(stateErr, gorm.ErrRecordNotFound) {
			return stateErr
		}
		if errors.Is(stateErr, gorm.ErrRecordNotFound) {
			state = &openapimodels.RuntimeState{ID: runtimeStateID, Status: openapimodels.RuntimeCatalogReady}
		}
		var active *ReleaseSnapshot
		if state.ActiveRelease != nil && *state.ActiveRelease > 0 {
			active, stateErr = r.loadRelease(ctx, tx, *state.ActiveRelease)
			if stateErr != nil {
				if errors.Is(stateErr, ErrReleaseNotFound) {
					return fmt.Errorf("%w: active release %d", ErrCatalogDependency, *state.ActiveRelease)
				}
				return stateErr
			}
			if stateErr = validateRuntimeStateSnapshot(state, active); stateErr != nil {
				return stateErr
			}
		}

		rows, rowsErr := r.loadRows(ctx, tx)
		if rowsErr != nil {
			return rowsErr
		}
		var tombstones []string
		if active != nil {
			tombstones = active.TombstoneScopes
		}
		if report := catalogruntime.ValidateDraftStructure(draft, tombstones, nil); !report.Valid() {
			return fmt.Errorf("%w: %s", catalogruntime.ErrDraftInvalid, report.Errors[0].Message)
		}
		items, capabilityIDs, itemErr := rows.releaseItems(draft)
		if itemErr != nil {
			return itemErr
		}

		version := options.Version
		if version <= 0 {
			version, itemErr = nextReleaseVersion(ctx, tx)
			if itemErr != nil {
				return classifyQueryError((openapimodels.Release{}).TableName(), itemErr)
			}
		}
		if version <= 0 {
			return fmt.Errorf("%w: release version must be positive", ErrCatalogDependency)
		}

		now := r.now().UTC()
		parentRelease := state.ActiveRelease
		snapshotHash, hashErr := hashReleaseItems(items)
		if hashErr != nil {
			return hashErr
		}
		release := &openapimodels.Release{
			Version:       version,
			Name:          strings.TrimSpace(options.Name),
			Status:        openapimodels.ReleaseStatusPublished,
			SnapshotHash:  snapshotHash,
			ItemCount:     len(items),
			ParentRelease: parentRelease,
			PublishedBy:   options.PublishedBy,
			PublishedAt:   &now,
			CreatedBy:     options.CreatedBy,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if createErr := tx.WithContext(ctx).Create(release).Error; createErr != nil {
			return classifyWriteError((openapimodels.Release{}).TableName(), createErr)
		}
		for index := range items {
			items[index].ReleaseID = release.ID
			items[index].CreatedAt = now
			if createErr := tx.WithContext(ctx).Create(&items[index]).Error; createErr != nil {
				return classifyWriteError((openapimodels.ReleaseItem{}).TableName(), createErr)
			}
		}

		if len(capabilityIDs) > 0 {
			updates := map[string]any{
				"status":       openapimodels.CatalogStatusActive,
				"published_at": now,
				"updated_at":   now,
				"updated_by":   options.CreatedBy,
			}
			if updateErr := tx.WithContext(ctx).Model(&openapimodels.Capability{}).Where("id IN ?", capabilityIDs).Updates(updates).Error; updateErr != nil {
				return classifyWriteError((openapimodels.Capability{}).TableName(), updateErr)
			}
		}
		if state.ActiveRelease != nil && *state.ActiveRelease != 0 {
			if updateErr := tx.WithContext(ctx).Model(&openapimodels.Release{}).Where("id = ?", *state.ActiveRelease).Update("status", openapimodels.ReleaseStatusSuperseded).Error; updateErr != nil {
				return classifyWriteError((openapimodels.Release{}).TableName(), updateErr)
			}
		}

		previousEpoch := state.RuntimeEpoch
		state.ActiveRelease = int64Pointer(release.ID)
		state.ActiveVersion = release.Version
		state.SnapshotHash = release.SnapshotHash
		state.Status = openapimodels.RuntimeCatalogReady
		state.RuntimeEpoch = previousEpoch + 1
		state.LastError = ""
		state.LoadedAt = &now
		state.UpdatedAt = now
		if state.ID == 0 {
			state.ID = runtimeStateID
		}
		if saveErr := tx.WithContext(ctx).Save(state).Error; saveErr != nil {
			return classifyWriteError((openapimodels.RuntimeState{}).TableName(), saveErr)
		}

		loaded, loadErr := r.loadRelease(ctx, tx, release.ID)
		if loadErr != nil {
			return loadErr
		}
		if stateErr := validateRuntimeStateSnapshot(state, loaded); stateErr != nil {
			return stateErr
		}
		published = loaded
		return nil
	})
	if err != nil {
		return nil, err
	}
	return published, nil
}

// Publisher is a named façade for callers that separate read repositories
// from write publication orchestration. It shares the repository transaction.
// When a runtime is supplied, its controlled adapter registry and route
// validators are run before any database write.
type Publisher struct {
	repository *Repository
	runtime    *catalogruntime.CatalogRuntime
}

// NewPublisher creates a publication façade over a repository.
func NewPublisher(repository *Repository, runtimes ...*catalogruntime.CatalogRuntime) *Publisher {
	var runtime *catalogruntime.CatalogRuntime
	if len(runtimes) > 0 {
		runtime = runtimes[0]
	}
	return &Publisher{repository: repository, runtime: runtime}
}

// Publish delegates to the repository's atomic publication operation.
func (p *Publisher) Publish(ctx context.Context, draft catalogruntime.Draft, options PublishOptions) (*ReleaseSnapshot, error) {
	if p == nil || p.repository == nil {
		return nil, ErrCatalogRepositoryUnavailable
	}
	ctx = normalizeStoreContext(ctx)
	if p.runtime != nil {
		report := p.runtime.Validate(ctx, draft)
		if !report.Valid() {
			return nil, fmt.Errorf("%w: %s", catalogruntime.ErrDraftInvalid, report.Errors[0].Message)
		}
	}
	return p.repository.Publish(ctx, draft, options)
}

func normalizeStoreContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

type catalogRows struct {
	groups       []openapimodels.CapabilityGroup
	capabilities []openapimodels.Capability
	operations   []openapimodels.Operation
}

func (r *Repository) loadRows(ctx context.Context, db *gorm.DB) (catalogRows, error) {
	var rows catalogRows
	queries := []struct {
		table string
		dest  any
		query func(*gorm.DB) *gorm.DB
	}{
		{table: (openapimodels.CapabilityGroup{}).TableName(), dest: &rows.groups, query: func(q *gorm.DB) *gorm.DB {
			return q.Model(&openapimodels.CapabilityGroup{}).Where("deleted_at IS NULL")
		}},
		{table: (openapimodels.Capability{}).TableName(), dest: &rows.capabilities, query: func(q *gorm.DB) *gorm.DB {
			return q.Model(&openapimodels.Capability{}).Where("deleted_at IS NULL")
		}},
		{table: (openapimodels.Operation{}).TableName(), dest: &rows.operations, query: func(q *gorm.DB) *gorm.DB {
			return q.Model(&openapimodels.Operation{}).Where("deleted_at IS NULL")
		}},
	}
	for _, query := range queries {
		result := query.query(db.WithContext(ctx)).Find(query.dest)
		if result.Error != nil {
			return catalogRows{}, classifyQueryError(query.table, result.Error)
		}
	}
	return rows, nil
}

type sourceOperation struct {
	group       openapimodels.CapabilityGroup
	capability  openapimodels.Capability
	operation   openapimodels.Operation
	dispatchKey string
}

func (rows catalogRows) sourceOperations() ([]sourceOperation, error) {
	groups := make(map[int64]openapimodels.CapabilityGroup, len(rows.groups))
	for _, group := range rows.groups {
		groups[group.ID] = group
	}
	capabilities := make(map[int64]openapimodels.Capability, len(rows.capabilities))
	for _, capability := range rows.capabilities {
		if isDisabled(capability.Status) {
			continue
		}
		group, ok := groups[capability.GroupID]
		if !ok {
			return nil, fmt.Errorf("%w: capability %d references group %d", ErrCatalogDependency, capability.ID, capability.GroupID)
		}
		if isDisabled(group.Status) {
			continue
		}
		capabilities[capability.ID] = capability
	}

	result := make([]sourceOperation, 0, len(rows.operations))
	for _, operation := range rows.operations {
		if isDisabled(operation.Status) {
			continue
		}
		capability, ok := capabilities[operation.CapabilityID]
		if !ok {
			return nil, fmt.Errorf("%w: operation %d references capability %d", ErrCatalogDependency, operation.ID, operation.CapabilityID)
		}
		group := groups[capability.GroupID]
		method := strings.ToUpper(strings.TrimSpace(operation.Method))
		path := strings.TrimSpace(operation.ExternalPath)
		result = append(result, sourceOperation{
			group: group, capability: capability, operation: operation,
			dispatchKey: dispatchKey(capability.Scope, method, path),
		})
	}
	sort.SliceStable(result, func(i, j int) bool {
		left, right := result[i], result[j]
		if left.group.Sort != right.group.Sort {
			return left.group.Sort < right.group.Sort
		}
		if left.group.ID != right.group.ID {
			return left.group.ID < right.group.ID
		}
		if left.capability.ID != right.capability.ID {
			return left.capability.ID < right.capability.ID
		}
		if left.operation.Sort != right.operation.Sort {
			return left.operation.Sort < right.operation.Sort
		}
		return left.operation.ID < right.operation.ID
	})
	return result, nil
}

func (rows catalogRows) runtimeOperations() ([]catalogruntime.Operation, map[string]struct{}, error) {
	sources, err := rows.sourceOperations()
	if err != nil {
		return nil, nil, err
	}
	operations := make([]catalogruntime.Operation, 0, len(sources))
	knownScopes := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		operation := runtimeOperation(source)
		operations = append(operations, operation)
		knownScopes[operation.Scope] = struct{}{}
	}
	return operations, knownScopes, nil
}

func (rows catalogRows) releaseItems(draft catalogruntime.Draft) ([]openapimodels.ReleaseItem, []int64, error) {
	sources, err := rows.sourceOperations()
	if err != nil {
		return nil, nil, err
	}
	byKey := make(map[string]sourceOperation, len(sources))
	for _, source := range sources {
		byKey[source.dispatchKey] = source
	}
	items := make([]openapimodels.ReleaseItem, 0, len(draft.Operations))
	capabilitySet := make(map[int64]struct{}, len(draft.Operations))
	for _, operation := range draft.Operations {
		method := strings.ToUpper(strings.TrimSpace(operation.Method))
		path := strings.TrimSpace(operation.ExternalPath)
		source, ok := byKey[dispatchKey(strings.TrimSpace(operation.Scope), method, path)]
		if !ok {
			return nil, nil, fmt.Errorf("%w: %s %s (%s)", ErrDraftOperationNotFound, method, path, operation.Scope)
		}
		if !draftMatchesSource(source, operation) {
			return nil, nil, fmt.Errorf("%w: editable contract drift for %s %s (%s)", ErrDraftOperationNotFound, method, path, operation.Scope)
		}
		item, itemErr := makeReleaseItem(source, operation)
		if itemErr != nil {
			return nil, nil, itemErr
		}
		items = append(items, item)
		capabilitySet[source.capability.ID] = struct{}{}
	}
	capabilityIDs := make([]int64, 0, len(capabilitySet))
	for id := range capabilitySet {
		capabilityIDs = append(capabilityIDs, id)
	}
	sort.Slice(capabilityIDs, func(i, j int) bool { return capabilityIDs[i] < capabilityIDs[j] })
	return items, capabilityIDs, nil
}

func draftMatchesSource(source sourceOperation, draft catalogruntime.Operation) bool {
	expected := runtimeOperation(source)
	if strings.TrimSpace(draft.Name) != "" && strings.TrimSpace(draft.Name) != expected.Name {
		return false
	}
	if strings.TrimSpace(draft.AdapterKey) != "" && strings.TrimSpace(draft.AdapterKey) != expected.AdapterKey {
		return false
	}
	if strings.TrimSpace(draft.ContractVersion) != "" && normalizeContractVersion(draft.ContractVersion) != expected.ContractVersion {
		return false
	}
	if strings.TrimSpace(draft.SysAPIPath) != "" && strings.TrimSpace(draft.SysAPIPath) != expected.SysAPIPath {
		return false
	}
	if strings.TrimSpace(draft.SysAPIMethod) != "" && strings.ToUpper(strings.TrimSpace(draft.SysAPIMethod)) != expected.SysAPIMethod {
		return false
	}
	if strings.TrimSpace(draft.Risk) != "" && strings.TrimSpace(draft.Risk) != expected.Risk {
		return false
	}
	if draft.SysAPIID != 0 && draft.SysAPIID != expected.SysAPIID {
		return false
	}
	return true
}

func runtimeOperation(source sourceOperation) catalogruntime.Operation {
	operation := source.operation
	capability := source.capability
	name := strings.TrimSpace(operation.Name)
	if name == "" {
		name = strings.TrimSpace(capability.Name)
	}
	sysAPIID := operation.SysAPIID
	if sysAPIID == nil {
		sysAPIID = capability.SysAPIID
	}
	result := catalogruntime.Operation{
		Scope:           strings.TrimSpace(capability.Scope),
		Name:            name,
		Method:          strings.ToUpper(strings.TrimSpace(operation.Method)),
		ExternalPath:    strings.TrimSpace(operation.ExternalPath),
		AdapterKey:      strings.TrimSpace(operation.AdapterKey),
		ContractVersion: normalizeContractVersion(operation.AdapterContractVersion),
		SysAPIPath:      strings.TrimSpace(operation.SysAPIPath),
		SysAPIMethod:    strings.ToUpper(strings.TrimSpace(operation.SysAPIMethod)),
		Risk:            strings.TrimSpace(operation.RiskLevel),
	}
	if result.SysAPIPath == "" {
		result.SysAPIPath = strings.TrimSpace(capability.SysAPIPath)
	}
	if result.SysAPIMethod == "" {
		result.SysAPIMethod = strings.ToUpper(strings.TrimSpace(capability.SysAPIMethod))
	}
	if result.Risk == "" {
		result.Risk = strings.TrimSpace(capability.RiskLevel)
	}
	if sysAPIID != nil && *sysAPIID > 0 {
		result.SysAPIID = uint(*sysAPIID)
	}
	return result
}

func makeReleaseItem(source sourceOperation, draft catalogruntime.Operation) (openapimodels.ReleaseItem, error) {
	operation := source.operation
	capability := source.capability
	group := source.group
	name := strings.TrimSpace(draft.Name)
	if name == "" {
		name = strings.TrimSpace(operation.Name)
	}
	if name == "" {
		name = strings.TrimSpace(capability.Name)
	}
	method := strings.ToUpper(strings.TrimSpace(draft.Method))
	path := strings.TrimSpace(draft.ExternalPath)
	adapterKey := strings.TrimSpace(draft.AdapterKey)
	if adapterKey == "" {
		adapterKey = strings.TrimSpace(operation.AdapterKey)
	}
	contractVersion := ""
	if strings.TrimSpace(draft.ContractVersion) != "" {
		contractVersion = normalizeContractVersion(draft.ContractVersion)
	}
	if contractVersion == "" {
		contractVersion = normalizeContractVersion(operation.AdapterContractVersion)
	}
	sysAPIID := operation.SysAPIID
	if sysAPIID == nil {
		sysAPIID = capability.SysAPIID
	}
	sysAPIPath := strings.TrimSpace(draft.SysAPIPath)
	if sysAPIPath == "" {
		sysAPIPath = strings.TrimSpace(operation.SysAPIPath)
	}
	if sysAPIPath == "" {
		sysAPIPath = strings.TrimSpace(capability.SysAPIPath)
	}
	sysAPIMethod := strings.ToUpper(strings.TrimSpace(draft.SysAPIMethod))
	if sysAPIMethod == "" {
		sysAPIMethod = strings.ToUpper(strings.TrimSpace(operation.SysAPIMethod))
	}
	if sysAPIMethod == "" {
		sysAPIMethod = strings.ToUpper(strings.TrimSpace(capability.SysAPIMethod))
	}
	risk := strings.TrimSpace(draft.Risk)
	if risk == "" {
		risk = strings.TrimSpace(operation.RiskLevel)
	}
	if risk == "" {
		risk = strings.TrimSpace(capability.RiskLevel)
	}
	item := openapimodels.ReleaseItem{
		CapabilityID:           int64Pointer(capability.ID),
		OperationID:            int64Pointer(operation.ID),
		GroupCode:              group.Code,
		GroupName:              group.Name,
		CapabilityCode:         capability.Code,
		CapabilityName:         capability.Name,
		Scope:                  strings.TrimSpace(draft.Scope),
		Method:                 method,
		ExternalPath:           path,
		AdapterKey:             adapterKey,
		AdapterContractVersion: contractVersion,
		ResourceType:           firstNonEmpty(operation.ResourceType, capability.ResourceType),
		RiskLevel:              risk,
		IdempotencyMode:        operation.IdempotencyMode,
		RequestSchema:          operation.RequestSchema,
		ResponseSchema:         operation.ResponseSchema,
		SysAPIID:               sysAPIID,
		SysAPIPath:             sysAPIPath,
		SysAPIMethod:           sysAPIMethod,
		Sort:                   operation.Sort,
	}
	snapshot := releaseItemSnapshot{
		CapabilityID:           capability.ID,
		OperationID:            operation.ID,
		GroupCode:              item.GroupCode,
		GroupName:              item.GroupName,
		CapabilityCode:         item.CapabilityCode,
		CapabilityName:         item.CapabilityName,
		OperationName:          name,
		Scope:                  item.Scope,
		Method:                 item.Method,
		ExternalPath:           item.ExternalPath,
		AdapterKey:             item.AdapterKey,
		AdapterContractVersion: item.AdapterContractVersion,
		ResourceType:           item.ResourceType,
		RiskLevel:              item.RiskLevel,
		IdempotencyMode:        item.IdempotencyMode,
		RequestSchema:          item.RequestSchema,
		ResponseSchema:         item.ResponseSchema,
		SysAPIID:               pointerValue(item.SysAPIID),
		SysAPIPath:             item.SysAPIPath,
		SysAPIMethod:           item.SysAPIMethod,
		Sort:                   item.Sort,
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return openapimodels.ReleaseItem{}, fmt.Errorf("%w: snapshot encoding: %v", ErrCatalogDependency, err)
	}
	item.SnapshotJSON = string(raw)
	return item, nil
}

type releaseItemSnapshot struct {
	CapabilityID           int64  `json:"capabilityId"`
	OperationID            int64  `json:"operationId"`
	GroupCode              string `json:"groupCode"`
	GroupName              string `json:"groupName"`
	CapabilityCode         string `json:"capabilityCode"`
	CapabilityName         string `json:"capabilityName"`
	OperationName          string `json:"operationName"`
	Scope                  string `json:"scope"`
	Method                 string `json:"method"`
	ExternalPath           string `json:"externalPath"`
	AdapterKey             string `json:"adapterKey"`
	AdapterContractVersion string `json:"adapterContractVersion"`
	ResourceType           string `json:"resourceType"`
	RiskLevel              string `json:"riskLevel"`
	IdempotencyMode        string `json:"idempotencyMode"`
	RequestSchema          string `json:"requestSchema"`
	ResponseSchema         string `json:"responseSchema"`
	SysAPIID               int64  `json:"sysApiId,omitempty"`
	SysAPIPath             string `json:"sysApiPath"`
	SysAPIMethod           string `json:"sysApiMethod"`
	Sort                   int    `json:"sort"`
}

func decodeSnapshot(raw string) (releaseItemSnapshot, error) {
	var result releaseItemSnapshot
	if strings.TrimSpace(raw) == "" {
		return result, errors.New("empty release snapshot")
	}
	return result, json.Unmarshal([]byte(raw), &result)
}

func hashReleaseItems(items []openapimodels.ReleaseItem) (string, error) {
	canonical := make([]string, 0, len(items))
	for _, item := range items {
		canonical = append(canonical, item.SnapshotJSON)
	}
	raw, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("%w: release hash: %v", ErrCatalogDependency, err)
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func (r *Repository) loadOrphanScopes(ctx context.Context, db *gorm.DB, known map[string]struct{}) ([]string, error) {
	type clientScopeRow struct {
		Scope string `gorm:"column:scope"`
	}
	var rows []clientScopeRow
	result := db.WithContext(ctx).Table((openapimodels.ClientScope{}).TableName()).Select("scope").Distinct().Order("scope").Scan(&rows)
	if result.Error != nil {
		return nil, classifyQueryError((openapimodels.ClientScope{}).TableName(), result.Error)
	}
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		scope := strings.TrimSpace(row.Scope)
		if scope == "" {
			continue
		}
		if _, exists := known[scope]; exists {
			continue
		}
		seen[scope] = struct{}{}
	}
	orphans := make([]string, 0, len(seen))
	for scope := range seen {
		orphans = append(orphans, scope)
	}
	sort.Strings(orphans)
	return orphans, nil
}

func (r *Repository) loadRuntimeState(ctx context.Context, db *gorm.DB) (*openapimodels.RuntimeState, error) {
	return r.loadRuntimeStateWithQuery(ctx, db.WithContext(ctx))
}

func (r *Repository) loadRuntimeStateForPublish(ctx context.Context, db *gorm.DB) (*openapimodels.RuntimeState, error) {
	query := db.WithContext(ctx)
	if db.Name() == "sqlserver" {
		query = query.Table((openapimodels.RuntimeState{}).TableName() + " WITH (UPDLOCK, HOLDLOCK)")
	} else {
		query = query.Model(&openapimodels.RuntimeState{}).Clauses(clause.Locking{Strength: "UPDATE"})
	}
	return r.loadRuntimeStateWithQuery(ctx, query)
}

func (r *Repository) loadRuntimeStateWithQuery(ctx context.Context, query *gorm.DB) (*openapimodels.RuntimeState, error) {
	var state openapimodels.RuntimeState
	result := query.WithContext(ctx).Where("id = ?", runtimeStateID).First(&state)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, gorm.ErrRecordNotFound
		}
		return nil, classifyQueryError((openapimodels.RuntimeState{}).TableName(), result.Error)
	}
	return &state, nil
}

func validateRuntimeStateSnapshot(state *openapimodels.RuntimeState, snapshot *ReleaseSnapshot) error {
	if state == nil || snapshot == nil {
		return fmt.Errorf("%w: active runtime snapshot is nil", ErrCatalogDependency)
	}
	if state.Status != openapimodels.RuntimeCatalogReady {
		return fmt.Errorf("%w: runtime status is %s", ErrCatalogRuntimeUnavailable, state.Status)
	}
	if state.RuntimeEpoch <= 0 {
		return fmt.Errorf("%w: runtime epoch is not positive", ErrCatalogRuntimeUnavailable)
	}
	if snapshot.Release.Status != openapimodels.ReleaseStatusPublished {
		return fmt.Errorf("%w: active release %d has status %s", ErrCatalogDependency, snapshot.Release.ID, snapshot.Release.Status)
	}
	if state.ActiveRelease == nil || *state.ActiveRelease != snapshot.Release.ID {
		return fmt.Errorf("%w: active release pointer mismatch", ErrCatalogDependency)
	}
	if state.ActiveVersion <= 0 || state.ActiveVersion != snapshot.Release.Version {
		return fmt.Errorf("%w: active version %d points to release version %d", ErrCatalogDependency, state.ActiveVersion, snapshot.Release.Version)
	}
	if strings.TrimSpace(state.SnapshotHash) == "" || !strings.EqualFold(strings.TrimSpace(state.SnapshotHash), snapshot.Release.SnapshotHash) {
		return fmt.Errorf("%w: active state snapshot hash mismatch", ErrCatalogDependency)
	}
	return nil
}

func runtimeIdentityFromState(state *openapimodels.RuntimeState) catalogruntime.RuntimeIdentity {
	if state == nil || state.ActiveRelease == nil {
		return catalogruntime.RuntimeIdentity{}
	}
	return catalogruntime.RuntimeIdentity{
		ReleaseID:    *state.ActiveRelease,
		Version:      state.ActiveVersion,
		SnapshotHash: state.SnapshotHash,
		RuntimeEpoch: state.RuntimeEpoch,
	}
}

func sameRuntimeStateIdentity(left, right *openapimodels.RuntimeState) bool {
	if left == nil || right == nil {
		return false
	}
	if left.ID != right.ID || left.Status != right.Status || left.ActiveVersion != right.ActiveVersion || left.RuntimeEpoch != right.RuntimeEpoch || !strings.EqualFold(strings.TrimSpace(left.SnapshotHash), strings.TrimSpace(right.SnapshotHash)) {
		return false
	}
	if left.ActiveRelease == nil || right.ActiveRelease == nil {
		return left.ActiveRelease == nil && right.ActiveRelease == nil
	}
	return *left.ActiveRelease == *right.ActiveRelease
}

func (r *Repository) loadRelease(ctx context.Context, db *gorm.DB, releaseID int64) (*ReleaseSnapshot, error) {
	if releaseID <= 0 {
		return nil, ErrReleaseNotFound
	}
	var release openapimodels.Release
	result := db.WithContext(ctx).Where("id = ?", releaseID).First(&release)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrReleaseNotFound
		}
		return nil, classifyQueryError((openapimodels.Release{}).TableName(), result.Error)
	}
	var items []openapimodels.ReleaseItem
	result = db.WithContext(ctx).Where("release_id = ?", release.ID).Order("id").Find(&items)
	if result.Error != nil {
		return nil, classifyQueryError((openapimodels.ReleaseItem{}).TableName(), result.Error)
	}
	if validateErr := validateReleaseSnapshot(release, items); validateErr != nil {
		return nil, validateErr
	}
	tombstones, tombstoneErr := r.loadTombstones(ctx, db, release)
	if tombstoneErr != nil {
		return nil, tombstoneErr
	}
	return &ReleaseSnapshot{
		Release:         release,
		Items:           append([]openapimodels.ReleaseItem(nil), items...),
		TombstoneScopes: tombstones,
	}, nil
}

func validateReleaseSnapshot(release openapimodels.Release, items []openapimodels.ReleaseItem) error {
	if release.ItemCount != len(items) {
		return fmt.Errorf("%w: release %d item count is %d, loaded %d", ErrCatalogDependency, release.ID, release.ItemCount, len(items))
	}
	for _, item := range items {
		if strings.TrimSpace(item.SnapshotJSON) == "" {
			return fmt.Errorf("%w: release item %d has an empty snapshot", ErrCatalogDependency, item.ID)
		}
		snapshot, err := decodeSnapshot(item.SnapshotJSON)
		if err != nil {
			return fmt.Errorf("%w: release item %d snapshot is invalid: %v", ErrCatalogDependency, item.ID, err)
		}
		if !snapshotMatchesItem(snapshot, item) {
			return fmt.Errorf("%w: release item %d snapshot does not match item columns", ErrCatalogDependency, item.ID)
		}
	}
	if strings.TrimSpace(release.SnapshotHash) == "" {
		return fmt.Errorf("%w: release %d has an empty snapshot hash", ErrCatalogDependency, release.ID)
	}
	digest, err := hashReleaseItems(items)
	if err != nil {
		return err
	}
	if !strings.EqualFold(digest, strings.TrimSpace(release.SnapshotHash)) {
		return fmt.Errorf("%w: release %d snapshot hash mismatch", ErrCatalogDependency, release.ID)
	}
	return nil
}

func snapshotMatchesItem(snapshot releaseItemSnapshot, item openapimodels.ReleaseItem) bool {
	return snapshot.CapabilityID == pointerValue(item.CapabilityID) &&
		snapshot.OperationID == pointerValue(item.OperationID) &&
		snapshot.GroupCode == item.GroupCode &&
		snapshot.GroupName == item.GroupName &&
		snapshot.CapabilityCode == item.CapabilityCode &&
		snapshot.CapabilityName == item.CapabilityName &&
		strings.TrimSpace(snapshot.OperationName) != "" &&
		snapshot.Scope == item.Scope &&
		snapshot.Method == item.Method &&
		snapshot.ExternalPath == item.ExternalPath &&
		snapshot.AdapterKey == item.AdapterKey &&
		snapshot.AdapterContractVersion == item.AdapterContractVersion &&
		snapshot.ResourceType == item.ResourceType &&
		snapshot.RiskLevel == item.RiskLevel &&
		snapshot.IdempotencyMode == item.IdempotencyMode &&
		snapshot.RequestSchema == item.RequestSchema &&
		snapshot.ResponseSchema == item.ResponseSchema &&
		snapshot.SysAPIID == pointerValue(item.SysAPIID) &&
		snapshot.SysAPIPath == item.SysAPIPath &&
		snapshot.SysAPIMethod == item.SysAPIMethod &&
		snapshot.Sort == item.Sort
}

func (r *Repository) loadTombstones(ctx context.Context, db *gorm.DB, release openapimodels.Release) ([]string, error) {
	currentScopes := make(map[string]struct{})
	var currentItems []openapimodels.ReleaseItem
	result := db.WithContext(ctx).Where("release_id = ?", release.ID).Find(&currentItems)
	if result.Error != nil {
		return nil, classifyQueryError((openapimodels.ReleaseItem{}).TableName(), result.Error)
	}
	for _, item := range currentItems {
		if scope := strings.TrimSpace(item.Scope); scope != "" {
			currentScopes[scope] = struct{}{}
		}
	}
	tombstoneSet := make(map[string]struct{})
	seenReleases := map[int64]struct{}{release.ID: {}}
	parentID := release.ParentRelease
	for parentID != nil && *parentID > 0 {
		if _, seen := seenReleases[*parentID]; seen {
			return nil, fmt.Errorf("%w: release parent cycle at %d", ErrCatalogDependency, *parentID)
		}
		seenReleases[*parentID] = struct{}{}
		var parent openapimodels.Release
		result = db.WithContext(ctx).Where("id = ?", *parentID).First(&parent)
		if result.Error != nil {
			if errors.Is(result.Error, gorm.ErrRecordNotFound) {
				return nil, fmt.Errorf("%w: parent release %d", ErrCatalogDependency, *parentID)
			}
			return nil, classifyQueryError((openapimodels.Release{}).TableName(), result.Error)
		}
		var parentItems []openapimodels.ReleaseItem
		// Order is part of correctness here, not cosmetics. These rows are
		// handed to validateReleaseSnapshot, which recomputes a hash over the
		// items in slice order; without an explicit ORDER BY SQLite may return a
		// table or index scan in any order, and the hash of an intact parent
		// release then fails to match. It reproduces as soon as a second release
		// is published after any row update has changed the query plan: disabling
		// one operation and republishing was enough to report a healthy parent
		// release as corrupted.
		result = db.WithContext(ctx).Where("release_id = ?", parent.ID).Order("id").Find(&parentItems)
		if result.Error != nil {
			return nil, classifyQueryError((openapimodels.ReleaseItem{}).TableName(), result.Error)
		}
		if validateErr := validateReleaseSnapshot(parent, parentItems); validateErr != nil {
			return nil, validateErr
		}
		for _, item := range parentItems {
			scope := strings.TrimSpace(item.Scope)
			if scope == "" {
				continue
			}
			if _, active := currentScopes[scope]; !active {
				tombstoneSet[scope] = struct{}{}
			}
		}
		parentID = parent.ParentRelease
	}
	tombstones := make([]string, 0, len(tombstoneSet))
	for scope := range tombstoneSet {
		tombstones = append(tombstones, scope)
	}
	sort.Strings(tombstones)
	return tombstones, nil
}

func nextReleaseVersion(ctx context.Context, db *gorm.DB) (int64, error) {
	var row struct{ Version *int64 }
	result := db.WithContext(ctx).Model(&openapimodels.Release{}).Select("MAX(version) AS version").Scan(&row)
	if result.Error != nil {
		return 0, result.Error
	}
	if row.Version == nil || *row.Version < 1 {
		return 1, nil
	}
	return *row.Version + 1, nil
}

func dispatchKey(scope, method, path string) string {
	return strings.TrimSpace(scope) + "\x00" + strings.ToUpper(strings.TrimSpace(method)) + "\x00" + strings.TrimSpace(path)
}

func normalizeContractVersion(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "v1"
	}
	if strings.HasPrefix(strings.ToLower(value), "v") {
		if _, err := strconv.Atoi(value[1:]); err == nil {
			return "v" + value[1:]
		}
	}
	if _, err := strconv.Atoi(value); err == nil {
		return "v" + value
	}
	return value
}

func isDisabled(status string) bool {
	return strings.EqualFold(strings.TrimSpace(status), openapimodels.CatalogStatusDisabled)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func int64Pointer(value int64) *int64 { return &value }

func pointerValue(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func classifyQueryError(table string, err error) error {
	if err == nil {
		return nil
	}
	if isMissingTableError(err) {
		return fmt.Errorf("%w: %s: %v", ErrCatalogTableMissing, table, err)
	}
	return fmt.Errorf("%w: %s: %v", ErrCatalogDependency, table, err)
}

func classifyWriteError(table string, err error) error {
	if err == nil {
		return nil
	}
	if isMissingTableError(err) {
		return fmt.Errorf("%w: %s: %v", ErrCatalogTableMissing, table, err)
	}
	return err
}

func isMissingTableError(err error) bool {
	message := strings.ToLower(err.Error())
	patterns := []string{
		"no such table",
		"doesn't exist",
		"does not exist",
		"invalid object name",
		"undefined table",
		"relation \"",
	}
	for _, pattern := range patterns {
		if strings.Contains(message, pattern) {
			return true
		}
	}
	return false
}
