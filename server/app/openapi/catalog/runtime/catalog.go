package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrDraftInvalid           = errors.New("OpenAPI catalog draft is invalid")
	ErrNoActiveRelease        = errors.New("OpenAPI catalog has no active release")
	ErrRollbackUnavailable    = errors.New("OpenAPI catalog rollback unavailable")
	ErrRuntimeIdentityInvalid = errors.New("OpenAPI catalog runtime identity is invalid")
	ErrScopeNotDispatchable   = errors.New("OpenAPI scope is not dispatchable")
	ErrRouteNotFound          = errors.New("OpenAPI catalog route not found")
)

const (
	IssueCodeInvalidScope       = "INVALID_SCOPE"
	IssueCodeInvalidRoute       = "INVALID_ROUTE"
	IssueCodeDuplicateScope     = "DUPLICATE_SCOPE"
	IssueCodeDuplicateRoute     = "DUPLICATE_ROUTE"
	IssueCodeAdapterUnavailable = "ADAPTER_UNAVAILABLE"
	IssueCodeScopeTombstoned    = "SCOPE_TOMBSTONED"
	IssueCodeScopeConflict      = "SCOPE_STATE_CONFLICT"
	IssueCodeOrphanScope        = "ORPHAN_SCOPE"
	IssueCodeSysAPIDrift        = "SYS_API_DRIFT"
)

type IssueSeverity string

const (
	SeverityError   IssueSeverity = "error"
	SeverityWarning IssueSeverity = "warning"
)

type ValidationIssue struct {
	Code     string        `json:"code"`
	Scope    string        `json:"scope,omitempty"`
	Severity IssueSeverity `json:"severity"`
	Message  string        `json:"message"`
}

type ValidationReport struct {
	Errors   []ValidationIssue `json:"errors,omitempty"`
	Warnings []ValidationIssue `json:"warnings,omitempty"`
}

func (r ValidationReport) Valid() bool { return len(r.Errors) == 0 }

type SysAPIAsset struct {
	ID      uint
	Path    string
	Method  string
	Deleted bool
}

type SysAPIResolver interface {
	Resolve(context.Context, string, string) (SysAPIAsset, error)
}

type Operation struct {
	Scope           string `json:"scope"`
	Name            string `json:"name,omitempty"`
	Method          string `json:"method"`
	ExternalPath    string `json:"externalPath"`
	AdapterKey      string `json:"adapterKey"`
	ContractVersion string `json:"contractVersion"`
	SysAPIID        uint   `json:"sysApiId,omitempty"`
	SysAPIPath      string `json:"sysApiPath,omitempty"`
	SysAPIMethod    string `json:"sysApiMethod,omitempty"`
	Risk            string `json:"risk,omitempty"`
}

type Draft struct {
	Version         string      `json:"version,omitempty"`
	Operations      []Operation `json:"operations"`
	TombstoneScopes []string    `json:"tombstoneScopes,omitempty"`
	OrphanScopes    []string    `json:"orphanScopes,omitempty"`
}

// ScopeState distinguishes a scope that was intentionally removed from one
// that only remains as a compatibility record from old client grants.
type ScopeState string

const (
	ScopeActive     ScopeState = "active"
	ScopeTombstoned ScopeState = "tombstoned"
	ScopeOrphan     ScopeState = "orphan"
	ScopeUnknown    ScopeState = "unknown"
)

type Release struct {
	ID          string    `json:"id"`
	Version     string    `json:"version"`
	Digest      string    `json:"digest"`
	PublishedAt time.Time `json:"publishedAt"`

	operations []Operation
	tombstones []string
	orphans    []string
	warnings   []ValidationIssue
}

// RuntimeIdentity identifies the durable catalog state that produced the
// process-local release. It is deliberately separate from Release.ID: the
// latter is an in-memory publication identifier, while ReleaseID here is the
// database release primary key used by the runtime-state pointer.
type RuntimeIdentity struct {
	ReleaseID    int64
	Version      int64
	SnapshotHash string
	RuntimeEpoch int64
}

func (identity RuntimeIdentity) valid() bool {
	return identity.ReleaseID > 0 && identity.Version > 0 && identity.RuntimeEpoch > 0 && strings.TrimSpace(identity.SnapshotHash) != ""
}

// PublishedOperation is named separately to make the draft-to-release
// boundary explicit while preserving the same contract fields.
type PublishedOperation = Operation

func (r *Release) Operation(scope string) (PublishedOperation, bool) {
	if r == nil {
		return PublishedOperation{}, false
	}
	for _, operation := range r.operations {
		if operation.Scope == scope {
			return operation, true
		}
	}
	return PublishedOperation{}, false
}

func (r *Release) Operations() []PublishedOperation {
	if r == nil {
		return nil
	}
	return append([]PublishedOperation(nil), r.operations...)
}

func (r *Release) TombstoneScopes() []string {
	if r == nil {
		return nil
	}
	return append([]string(nil), r.tombstones...)
}

func (r *Release) OrphanScopes() []string {
	if r == nil {
		return nil
	}
	return append([]string(nil), r.orphans...)
}

func (r *Release) Warnings() []ValidationIssue {
	if r == nil {
		return nil
	}
	return append([]ValidationIssue(nil), r.warnings...)
}

type CatalogRuntime struct {
	registry *AdapterRegistry
	resolver SysAPIResolver

	mu         sync.RWMutex
	active     *Release
	identity   RuntimeIdentity
	history    []*Release
	sequence   uint64
	tombstones map[string]struct{}
	orphans    map[string]struct{}
	now        func() time.Time
}

func NewCatalogRuntime(registry *AdapterRegistry, resolver SysAPIResolver) *CatalogRuntime {
	if registry == nil {
		registry = NewAdapterRegistry()
	}
	return &CatalogRuntime{registry: registry, resolver: resolver, tombstones: make(map[string]struct{}), orphans: make(map[string]struct{}), now: func() time.Time { return time.Now().UTC() }}
}

func (r *CatalogRuntime) Validate(ctx context.Context, draft Draft) ValidationReport {
	if r == nil {
		return ValidationReport{Errors: []ValidationIssue{{Code: IssueCodeInvalidRoute, Severity: SeverityError, Message: "catalog runtime is unavailable"}}}
	}
	ctx = normalizeContext(ctx)
	r.mu.RLock()
	tombstones := cloneSet(r.tombstones)
	orphans := cloneSet(r.orphans)
	var active *Release
	if r.active != nil {
		active = cloneRelease(r.active)
	}
	r.mu.RUnlock()
	return r.validate(ctx, draft, active, tombstones, orphans)
}

// Publish validates the complete draft before taking the write lock's state
// transition. The active pointer is replaced exactly once after all checks
// succeed, so an invalid draft can never leave a partially published catalog.
func (r *CatalogRuntime) Publish(ctx context.Context, draft Draft) (*Release, ValidationReport, error) {
	if r == nil {
		return nil, ValidationReport{Errors: []ValidationIssue{{Code: IssueCodeInvalidRoute, Severity: SeverityError, Message: "catalog runtime is unavailable"}}}, ErrDraftInvalid
	}
	ctx = normalizeContext(ctx)
	r.mu.Lock()
	defer r.mu.Unlock()
	var active *Release
	if r.active != nil {
		active = cloneRelease(r.active)
	}
	report := r.validate(ctx, draft, active, r.tombstones, r.orphans)
	if !report.Valid() {
		return nil, report, fmt.Errorf("%w: %s", ErrDraftInvalid, report.Errors[0].Message)
	}

	newTombstones := cloneSet(r.tombstones)
	newOrphans := cloneSet(r.orphans)
	for _, scope := range normalizedScopes(draft.TombstoneScopes) {
		newTombstones[scope] = struct{}{}
	}
	for _, scope := range normalizedScopes(draft.OrphanScopes) {
		newOrphans[scope] = struct{}{}
	}
	if active != nil {
		current := make(map[string]struct{}, len(draft.Operations))
		for _, operation := range draft.Operations {
			current[operation.Scope] = struct{}{}
		}
		for _, operation := range active.operations {
			if _, exists := current[operation.Scope]; !exists {
				newTombstones[operation.Scope] = struct{}{}
			}
		}
	}
	for scope := range newTombstones {
		delete(newOrphans, scope)
	}

	r.sequence++
	version := strings.TrimSpace(draft.Version)
	if version == "" {
		version = fmt.Sprintf("release-%d", r.sequence)
	}
	operations := cloneOperations(normalizedOperations(draft.Operations))
	canonical := canonicalReleaseData{Version: version, Operations: operations, Tombstones: sortedSet(newTombstones), Orphans: sortedSet(newOrphans)}
	raw, _ := json.Marshal(canonical)
	hash := sha256.Sum256(raw)
	release := &Release{ID: fmt.Sprintf("release-%d-%s", r.sequence, hex.EncodeToString(hash[:])[:12]), Version: version, Digest: hex.EncodeToString(hash[:]), PublishedAt: r.now().UTC(), operations: operations, tombstones: canonical.Tombstones, orphans: canonical.Orphans, warnings: append([]ValidationIssue(nil), report.Warnings...)}
	if r.active != nil {
		r.history = append(r.history, r.active)
	}
	r.active = release
	// A process-local publication is not a durable runtime transition. Any
	// prior DB binding is invalid until the repository hydrates and binds the
	// persisted active release again.
	r.identity = RuntimeIdentity{}
	r.tombstones = newTombstones
	r.orphans = newOrphans
	return cloneRelease(release), report, nil
}

// BindIdentity associates the active in-memory release with the durable
// runtime-state row loaded by the repository. It is intentionally a separate
// operation from Publish so an in-memory Rollback cannot masquerade as a
// persisted active pointer.
func (r *CatalogRuntime) BindIdentity(identity RuntimeIdentity) error {
	if r == nil {
		return ErrRuntimeIdentityInvalid
	}
	if !identity.valid() {
		return ErrRuntimeIdentityInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active == nil {
		return ErrNoActiveRelease
	}
	r.identity = identity
	return nil
}

// Identity returns the durable identity bound during Hydrate. A missing
// identity means this runtime is either a legacy in-memory runtime or has been
// changed without a durable publication and must not dispatch in authoritative
// catalog mode.
func (r *CatalogRuntime) Identity() (RuntimeIdentity, bool) {
	if r == nil {
		return RuntimeIdentity{}, false
	}
	r.mu.RLock()
	identity := r.identity
	valid := identity.valid()
	r.mu.RUnlock()
	return identity, valid
}

// ReadyIdentity returns the durable identity only when the same read-locked
// runtime snapshot has an active release and a valid identity. Keeping these
// checks under one lock prevents a concurrent in-memory publication from
// pairing the previous identity with the newly published release.
func (r *CatalogRuntime) ReadyIdentity() (RuntimeIdentity, bool) {
	if r == nil {
		return RuntimeIdentity{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.active == nil || !r.identity.valid() {
		return RuntimeIdentity{}, false
	}
	return r.identity, true
}

func (r *CatalogRuntime) Active() *Release {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return cloneRelease(r.active)
}

func (r *CatalogRuntime) Rollback() (*Release, error) {
	if r == nil {
		return nil, ErrRollbackUnavailable
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active == nil || len(r.history) == 0 {
		return nil, ErrRollbackUnavailable
	}
	r.active = r.history[len(r.history)-1]
	r.history = r.history[:len(r.history)-1]
	r.tombstones = setFromSlice(r.active.tombstones)
	r.orphans = setFromSlice(r.active.orphans)
	r.identity = RuntimeIdentity{}
	return cloneRelease(r.active), nil
}

func (r *CatalogRuntime) RollbackTo(releaseID string) (*Release, error) {
	if r == nil || strings.TrimSpace(releaseID) == "" {
		return nil, ErrRollbackUnavailable
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for index := len(r.history) - 1; index >= 0; index-- {
		if r.history[index].ID != releaseID {
			continue
		}
		r.active = r.history[index]
		r.history = r.history[:index]
		r.tombstones = setFromSlice(r.active.tombstones)
		r.orphans = setFromSlice(r.active.orphans)
		r.identity = RuntimeIdentity{}
		return cloneRelease(r.active), nil
	}
	return nil, ErrRollbackUnavailable
}

func (r *CatalogRuntime) ScopeStatus(scope string) ScopeState {
	if r == nil {
		return ScopeUnknown
	}
	scope = strings.TrimSpace(scope)
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.active != nil {
		if _, ok := r.active.Operation(scope); ok {
			return ScopeActive
		}
	}
	if _, ok := r.tombstones[scope]; ok {
		return ScopeTombstoned
	}
	if _, ok := r.orphans[scope]; ok {
		return ScopeOrphan
	}
	return ScopeUnknown
}

func (r *CatalogRuntime) Resolve(method, path string) (PublishedOperation, error) {
	operation, _, err := r.Match(method, path)
	return operation, err
}

// Match resolves a concrete external request path against the active route
// snapshot. Path parameters are extracted from template segments and returned
// separately so adapters do not need to parse router syntax themselves.
func (r *CatalogRuntime) Match(method, path string) (PublishedOperation, map[string]string, error) {
	if r == nil {
		return PublishedOperation{}, nil, ErrNoActiveRelease
	}
	request, err := parseExternalPath(path)
	if err != nil {
		return PublishedOperation{}, nil, ErrRouteNotFound
	}
	for _, segment := range request {
		if segment.isParameter {
			return PublishedOperation{}, nil, ErrRouteNotFound
		}
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.active == nil {
		return PublishedOperation{}, nil, ErrNoActiveRelease
	}
	for _, operation := range r.active.operations {
		if operation.Method != method {
			continue
		}
		pattern, patternErr := parseExternalPath(operation.ExternalPath)
		if patternErr != nil {
			continue
		}
		params, matched := matchExternalPath(pattern, request)
		if matched {
			return operation, params, nil
		}
	}
	return PublishedOperation{}, nil, ErrRouteNotFound
}

func (r *CatalogRuntime) Dispatch(ctx context.Context, scope string, invocation Invocation) (any, error) {
	if r == nil {
		return nil, ErrNoActiveRelease
	}
	ctx = normalizeContext(ctx)
	r.mu.RLock()
	active := r.active
	if active == nil {
		r.mu.RUnlock()
		return nil, ErrNoActiveRelease
	}
	operation, ok := active.Operation(scope)
	state := ScopeUnknown
	if !ok {
		if _, exists := r.tombstones[scope]; exists {
			state = ScopeTombstoned
		} else if _, exists := r.orphans[scope]; exists {
			state = ScopeOrphan
		}
	}
	r.mu.RUnlock()
	if !ok {
		if state == ScopeTombstoned || state == ScopeOrphan {
			return nil, fmt.Errorf("%w: %s", ErrScopeNotDispatchable, state)
		}
		return nil, ErrScopeNotDispatchable
	}
	if invocation.Method != "" && strings.ToUpper(invocation.Method) != operation.Method {
		return nil, ErrRouteNotFound
	}
	if r.registry == nil {
		return nil, fmt.Errorf("%w: adapter registry is unavailable", ErrScopeNotDispatchable)
	}
	adapter, err := r.registry.Resolve(operation.AdapterKey, operation.ContractVersion)
	if err != nil {
		return nil, fmt.Errorf("%w: %s@%s", ErrScopeNotDispatchable, operation.AdapterKey, operation.ContractVersion)
	}
	invocation.Scope = operation.Scope
	if invocation.Method == "" {
		invocation.Method = operation.Method
	}
	if invocation.Path == "" {
		invocation.Path = operation.ExternalPath
	} else {
		request, pathErr := parseExternalPath(invocation.Path)
		if pathErr != nil {
			return nil, ErrRouteNotFound
		}
		pattern, patternErr := parseExternalPath(operation.ExternalPath)
		if patternErr != nil {
			return nil, ErrRouteNotFound
		}
		params, matched := matchExternalPath(pattern, request)
		if !matched {
			return nil, ErrRouteNotFound
		}
		if invocation.Params == nil {
			invocation.Params = make(map[string]string, len(params))
		}
		for name, value := range params {
			invocation.Params[name] = value
		}
	}
	return adapter.Execute(ctx, invocation)
}

func (r *CatalogRuntime) validate(ctx context.Context, draft Draft, active *Release, tombstones, orphans map[string]struct{}) ValidationReport {
	report := ValidateDraftStructure(draft, sortedSet(tombstones), sortedSet(orphans))
	for _, operation := range draft.Operations {
		scope := strings.TrimSpace(operation.Scope)
		if r.registry == nil || !validToken(strings.TrimSpace(operation.AdapterKey), 64) || !validToken(strings.TrimSpace(operation.ContractVersion), 32) || !r.registry.Has(operation.AdapterKey, operation.ContractVersion) {
			report.Errors = append(report.Errors, issue(IssueCodeAdapterUnavailable, scope, SeverityError, "adapter key and contract version are not registered"))
		}
		if r.resolver != nil && strings.TrimSpace(operation.SysAPIPath) != "" {
			asset, resolveErr := r.resolver.Resolve(ctx, operation.SysAPIPath, strings.ToUpper(strings.TrimSpace(operation.SysAPIMethod)))
			expectedMethod := strings.ToUpper(strings.TrimSpace(operation.SysAPIMethod))
			if resolveErr != nil || asset.Deleted || asset.Path != operation.SysAPIPath || strings.ToUpper(asset.Method) != expectedMethod {
				report.Warnings = append(report.Warnings, issue(IssueCodeSysAPIDrift, scope, SeverityWarning, "sys_api asset drift is recorded; published adapter remains dispatchable"))
			}
		}
	}
	_ = active // active is retained for future release validators and auto-tombstone calculation.
	return report
}

// ValidateDraftStructure validates the persistence-level invariants that do
// not depend on the process-local adapter registry. Repository publication
// uses this before writing release rows, so a direct repository caller cannot
// bypass tombstone, scope, or route checks by omitting a CatalogRuntime.
func ValidateDraftStructure(draft Draft, tombstoneScopes, orphanScopes []string) ValidationReport {
	report := ValidationReport{}
	seenScopes := make(map[string]struct{}, len(draft.Operations))
	seenRoutes := make(map[string]struct{}, len(draft.Operations))
	declaredRoutes := make([]declaredRoute, 0, len(draft.Operations))
	tombstones := setFromSlice(normalizedScopes(tombstoneScopes))
	orphans := setFromSlice(normalizedScopes(orphanScopes))
	draftTombstones := make(map[string]struct{}, len(draft.TombstoneScopes))
	draftOrphans := make(map[string]struct{}, len(draft.OrphanScopes))
	for _, scope := range draft.TombstoneScopes {
		scope = strings.TrimSpace(scope)
		if !validScope(scope) {
			report.Errors = append(report.Errors, issue(IssueCodeInvalidScope, scope, SeverityError, "scope is invalid"))
			continue
		}
		draftTombstones[scope] = struct{}{}
	}
	for _, scope := range draft.OrphanScopes {
		scope = strings.TrimSpace(scope)
		if !validScope(scope) {
			report.Errors = append(report.Errors, issue(IssueCodeInvalidScope, scope, SeverityError, "scope is invalid"))
			continue
		}
		draftOrphans[scope] = struct{}{}
		if _, exists := orphans[scope]; !exists {
			report.Warnings = append(report.Warnings, issue(IssueCodeOrphanScope, scope, SeverityWarning, "scope is retained as an orphan and is not dispatchable"))
		}
	}
	for scope := range draftTombstones {
		if _, exists := draftOrphans[scope]; exists {
			report.Errors = append(report.Errors, issue(IssueCodeScopeConflict, scope, SeverityError, "scope cannot be both tombstoned and orphaned"))
		}
		if _, exists := orphans[scope]; exists {
			report.Errors = append(report.Errors, issue(IssueCodeScopeConflict, scope, SeverityError, "orphan scope cannot be tombstoned"))
		}
	}
	for _, operation := range draft.Operations {
		scope := strings.TrimSpace(operation.Scope)
		if !validScope(scope) {
			report.Errors = append(report.Errors, issue(IssueCodeInvalidScope, scope, SeverityError, "scope is invalid"))
			continue
		}
		if _, exists := seenScopes[scope]; exists {
			report.Errors = append(report.Errors, issue(IssueCodeDuplicateScope, scope, SeverityError, "scope is declared more than once"))
			continue
		}
		seenScopes[scope] = struct{}{}
		if _, exists := tombstones[scope]; exists {
			report.Errors = append(report.Errors, issue(IssueCodeScopeTombstoned, scope, SeverityError, "scope has been tombstoned and cannot be reused"))
		}
		if _, exists := draftTombstones[scope]; exists {
			report.Errors = append(report.Errors, issue(IssueCodeScopeConflict, scope, SeverityError, "scope is both active and tombstoned"))
		}
		if _, exists := orphans[scope]; exists {
			report.Errors = append(report.Errors, issue(IssueCodeScopeConflict, scope, SeverityError, "orphan scope cannot be reactivated"))
		}
		if _, exists := draftOrphans[scope]; exists {
			report.Errors = append(report.Errors, issue(IssueCodeScopeConflict, scope, SeverityError, "scope is both active and orphaned"))
		}
		if !validToken(strings.TrimSpace(operation.AdapterKey), 64) || !validToken(strings.TrimSpace(operation.ContractVersion), 32) {
			report.Errors = append(report.Errors, issue(IssueCodeAdapterUnavailable, scope, SeverityError, "adapter key and contract version are invalid"))
		}
		method := strings.ToUpper(strings.TrimSpace(operation.Method))
		canonical, err := canonicalExternalPath(operation.ExternalPath)
		if err != nil || !validHTTPMethod(method) {
			report.Errors = append(report.Errors, issue(IssueCodeInvalidRoute, scope, SeverityError, "external method or path is invalid"))
			continue
		}
		routeKey := method + " " + canonical
		if _, exists := seenRoutes[routeKey]; exists {
			report.Errors = append(report.Errors, issue(IssueCodeDuplicateRoute, scope, SeverityError, "method and external path are declared more than once"))
		}
		seenRoutes[routeKey] = struct{}{}
		for _, declared := range declaredRoutes {
			if declared.method != method || declared.canonical == canonical {
				continue
			}
			if externalPathsOverlap(operation.ExternalPath, declared.path) {
				report.Errors = append(report.Errors, issue(IssueCodeDuplicateRoute, scope, SeverityError, "method and external path overlap another declared route"))
				break
			}
		}
		declaredRoutes = append(declaredRoutes, declaredRoute{method: method, path: operation.ExternalPath, canonical: canonical})
	}
	return report
}

type canonicalReleaseData struct {
	Version    string      `json:"version"`
	Operations []Operation `json:"operations"`
	Tombstones []string    `json:"tombstones"`
	Orphans    []string    `json:"orphans"`
}

func issue(code, scope string, severity IssueSeverity, message string) ValidationIssue {
	return ValidationIssue{Code: code, Scope: scope, Severity: severity, Message: message}
}

func validScope(scope string) bool {
	if scope == "" || len(scope) > 64 {
		return false
	}
	if scope != strings.ToLower(scope) {
		return false
	}
	parts := strings.Split(scope, ":")
	for _, part := range parts {
		if !validToken(part, 64) {
			return false
		}
	}
	return true
}

func validHTTPMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func canonicalExternalPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/openapi/v1/") || strings.Contains(path, "//") || strings.ContainsAny(path, "?*") {
		return "", errors.New("invalid external path")
	}
	if len(path) > 512 {
		return "", errors.New("external path is too long")
	}
	segments := strings.Split(strings.TrimSuffix(path, "/"), "/")
	for index, segment := range segments {
		if index == 0 || segment == "" {
			continue
		}
		if strings.ContainsAny(segment, "{}") {
			if len(segment) < 3 || segment[0] != '{' || segment[len(segment)-1] != '}' || !validToken(segment[1:len(segment)-1], 64) {
				return "", errors.New("invalid external path parameter")
			}
			segments[index] = "{}"
		}
	}
	return strings.Join(segments, "/"), nil
}

type routeSegment struct {
	literal       string
	parameterName string
	isParameter   bool
}

type declaredRoute struct {
	method    string
	path      string
	canonical string
}

func parseExternalPath(path string) ([]routeSegment, error) {
	if _, err := canonicalExternalPath(path); err != nil {
		return nil, err
	}
	path = strings.TrimSuffix(strings.TrimSpace(path), "/")
	rawSegments := strings.Split(path, "/")
	segments := make([]routeSegment, len(rawSegments))
	for index, raw := range rawSegments {
		if index == 0 || raw == "" {
			segments[index] = routeSegment{literal: raw}
			continue
		}
		if raw[0] == '{' && raw[len(raw)-1] == '}' {
			segments[index] = routeSegment{isParameter: true, parameterName: raw[1 : len(raw)-1]}
			continue
		}
		segments[index] = routeSegment{literal: raw}
	}
	return segments, nil
}

func matchExternalPath(pattern, request []routeSegment) (map[string]string, bool) {
	if len(pattern) != len(request) {
		return nil, false
	}
	params := make(map[string]string)
	for index, expected := range pattern {
		actual := request[index]
		if expected.isParameter {
			if actual.isParameter || actual.literal == "" {
				return nil, false
			}
			if previous, exists := params[expected.parameterName]; exists && previous != actual.literal {
				return nil, false
			}
			params[expected.parameterName] = actual.literal
			continue
		}
		if actual.isParameter || expected.literal != actual.literal {
			return nil, false
		}
	}
	return params, true
}

func externalPathsOverlap(left, right string) bool {
	leftSegments, leftErr := parseExternalPath(left)
	rightSegments, rightErr := parseExternalPath(right)
	if leftErr != nil || rightErr != nil || len(leftSegments) != len(rightSegments) {
		return false
	}
	for index := range leftSegments {
		leftSegment, rightSegment := leftSegments[index], rightSegments[index]
		if !leftSegment.isParameter && !rightSegment.isParameter && leftSegment.literal != rightSegment.literal {
			return false
		}
	}
	return true
}

func cloneOperations(operations []Operation) []Operation {
	result := append([]Operation(nil), operations...)
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Scope == result[j].Scope {
			return result[i].Method+" "+result[i].ExternalPath < result[j].Method+" "+result[j].ExternalPath
		}
		return result[i].Scope < result[j].Scope
	})
	return result
}

func normalizedOperations(operations []Operation) []Operation {
	result := make([]Operation, 0, len(operations))
	for _, operation := range operations {
		operation.Scope = strings.TrimSpace(operation.Scope)
		operation.Name = strings.TrimSpace(operation.Name)
		operation.Method = strings.ToUpper(strings.TrimSpace(operation.Method))
		operation.ExternalPath = strings.TrimSpace(operation.ExternalPath)
		operation.AdapterKey = strings.TrimSpace(operation.AdapterKey)
		operation.ContractVersion = strings.TrimSpace(operation.ContractVersion)
		operation.SysAPIPath = strings.TrimSpace(operation.SysAPIPath)
		operation.SysAPIMethod = strings.ToUpper(strings.TrimSpace(operation.SysAPIMethod))
		operation.Risk = strings.TrimSpace(operation.Risk)
		result = append(result, operation)
	}
	return result
}

func normalizedScopes(scopes []string) []string {
	result := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		result = append(result, strings.TrimSpace(scope))
	}
	return result
}

func cloneRelease(release *Release) *Release {
	if release == nil {
		return nil
	}
	clone := *release
	clone.operations = cloneOperations(release.operations)
	clone.tombstones = append([]string(nil), release.tombstones...)
	clone.orphans = append([]string(nil), release.orphans...)
	clone.warnings = append([]ValidationIssue(nil), release.warnings...)
	return &clone
}

func cloneSet(input map[string]struct{}) map[string]struct{} {
	result := make(map[string]struct{}, len(input))
	for key := range input {
		result[key] = struct{}{}
	}
	return result
}

func sortedSet(input map[string]struct{}) []string {
	result := make([]string, 0, len(input))
	for key := range input {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func setFromSlice(input []string) map[string]struct{} {
	result := make(map[string]struct{}, len(input))
	for _, key := range input {
		result[key] = struct{}{}
	}
	return result
}

func normalizeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
