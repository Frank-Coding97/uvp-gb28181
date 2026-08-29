package management

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playback"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/repo"
)

// OwnershipType identifies the business source that can keep a media target
// alive. It is intentionally a finite vocabulary: callers cannot smuggle an
// arbitrary API name or a secret-bearing label into an impact response.
type OwnershipType string

const (
	OwnershipTypeRealtimePlayback    OwnershipType = "realtime_playback"
	OwnershipTypeDevicePlayback      OwnershipType = "device_playback"
	OwnershipTypeTalk                OwnershipType = "talk"
	OwnershipTypeCascade             OwnershipType = "cascade"
	OwnershipTypeRecordingPlan       OwnershipType = "recording_plan"
	OwnershipTypeContinuousRecording OwnershipType = "continuous_recording"
	OwnershipTypeRecordingSession    OwnershipType = "recording_session"
	OwnershipTypeManaged             OwnershipType = "managed"
	OwnershipTypeUnknown             OwnershipType = "unknown"
)

// OwnershipConfidence distinguishes an exact public-API match from a
// presence-only observation. Uncertain evidence is always fail-closed for a
// normal close operation.
type OwnershipConfidence string

const (
	OwnershipConfidenceProven    OwnershipConfidence = "proven"
	OwnershipConfidenceUncertain OwnershipConfidence = "uncertain"
)

// OwnershipStatus is the aggregate decision for one media target.
type OwnershipStatus string

const (
	OwnershipStatusAbsent     OwnershipStatus = "absent"
	OwnershipStatusManaged    OwnershipStatus = "managed"
	OwnershipStatusOwned      OwnershipStatus = "owned"
	OwnershipStatusUnknown    OwnershipStatus = "unknown"
	OwnershipStatusConflicted OwnershipStatus = "conflicted"
)

// OwnershipTarget identifies one media object on one ZLM node. The complete
// media tuple is retained; app/stream are never treated as URL path segments.
type OwnershipTarget struct {
	NodeID int64         `json:"nodeId"`
	Media  MediaIdentity `json:"media"`
}

func (t OwnershipTarget) Validate() error {
	fields := make(map[string]string)
	if t.NodeID <= 0 {
		fields["nodeId"] = "must be positive"
	}
	if err := t.Media.Validate(); err != nil {
		var validationErr *ValidationError
		if errors.As(err, &validationErr) {
			for key, value := range validationErr.Fields {
				fields["media."+key] = value
			}
		} else {
			fields["media"] = "is invalid"
		}
	}
	if len(fields) != 0 {
		return NewValidationError(fields)
	}
	return nil
}

// OwnershipEvidence is one read-only ownership fact. Fingerprint is an
// internal safe digest (for example, the managed-resource ledger digest) and
// is intentionally omitted from JSON responses.
type OwnershipEvidence struct {
	Type         OwnershipType       `json:"type"`
	ResourceType string              `json:"resourceType,omitempty"`
	Key          string              `json:"key,omitempty"`
	Owner        string              `json:"owner,omitempty"`
	Confidence   OwnershipConfidence `json:"confidence"`
	Reason       string              `json:"reason,omitempty"`
	Fingerprint  string              `json:"-"`
	Conflict     bool                `json:"-"`
}

// OwnershipSnapshot is a side-effect-free view used by preview, close
// preflight and audit adapters. Present is false when ZLM explicitly reports
// absence; PresenceKnown separates that state from an unavailable probe.
type OwnershipSnapshot struct {
	Target        OwnershipTarget     `json:"target"`
	Present       bool                `json:"present"`
	PresenceKnown bool                `json:"presenceKnown"`
	Status        OwnershipStatus     `json:"status"`
	Owners        []OwnershipEvidence `json:"owners,omitempty"`
	Impacts       []Impact            `json:"impacts,omitempty"`
	Fingerprint   string              `json:"fingerprint"`
}

// CanNormalClose is deliberately narrow. Only a managed resource with a
// known ZLM presence state can enter the ordinary idempotent cleanup path.
// When Present is false, the action may only reconcile/tombstone the ledger;
// it must not treat the resource as online or invoke a live-media close.
func (s OwnershipSnapshot) CanNormalClose() bool {
	return s.Status == OwnershipStatusManaged && s.PresenceKnown
}

// MediaPresenceReader is the only runtime input needed by the ownership
// resolver. An error is represented as unknown presence and never converted
// into online state.
type MediaPresenceReader interface {
	IsPresent(context.Context, OwnershipTarget) (bool, error)
}

// OwnershipSource is a read-only adapter for one business or provenance
// source. Implementations must not stop, close, release or clean up anything.
type OwnershipSource interface {
	Resolve(context.Context, OwnershipTarget) ([]OwnershipEvidence, error)
}

type OwnershipDependencies struct {
	Presence MediaPresenceReader
	Sources  []OwnershipSource
}

type OwnershipResolver struct {
	presence MediaPresenceReader
	sources  []OwnershipSource
}

func NewOwnershipResolver(dependencies OwnershipDependencies) *OwnershipResolver {
	sources := make([]OwnershipSource, 0, len(dependencies.Sources))
	for _, source := range dependencies.Sources {
		if source != nil {
			sources = append(sources, source)
		}
	}
	return &OwnershipResolver{presence: dependencies.Presence, sources: sources}
}

func (r *OwnershipResolver) Resolve(ctx context.Context, target OwnershipTarget) (OwnershipSnapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := target.Validate(); err != nil {
		return OwnershipSnapshot{}, err
	}

	snapshot := OwnershipSnapshot{Target: target, Owners: make([]OwnershipEvidence, 0)}
	if r != nil && r.presence != nil {
		present, err := r.presence.IsPresent(ctx, target)
		if err == nil {
			snapshot.Present, snapshot.PresenceKnown = present, true
		}
	}

	if r != nil {
		for _, source := range r.sources {
			evidence, err := source.Resolve(ctx, target)
			if err != nil {
				snapshot.Owners = append(snapshot.Owners, OwnershipEvidence{
					Type: OwnershipTypeUnknown, Confidence: OwnershipConfidenceUncertain,
					Reason: "ownership source unavailable",
				})
				continue
			}
			for _, item := range evidence {
				item = normalizeOwnershipEvidence(item)
				if item.Type == "" {
					item.Type = OwnershipTypeUnknown
				}
				if item.Confidence == "" {
					item.Confidence = OwnershipConfidenceUncertain
				}
				snapshot.Owners = appendUniqueEvidence(snapshot.Owners, item)
			}
		}
	}

	sortOwnershipEvidence(snapshot.Owners)
	snapshot.Status = deriveOwnershipStatus(snapshot)
	snapshot.Impacts = impactsForOwnership(snapshot.Target, snapshot.Owners)
	snapshot.Fingerprint = FingerprintOwnershipSnapshot(snapshot)
	return snapshot, nil
}

// OwnershipPreflight is an immutable confirmation token for one target. The
// resolver re-reads all adapters before Execute invokes the supplied action.
type OwnershipPreflight struct {
	Target      OwnershipTarget
	Snapshot    OwnershipSnapshot
	Fingerprint string
}

func (r *OwnershipResolver) Preflight(ctx context.Context, target OwnershipTarget) (OwnershipPreflight, error) {
	snapshot, err := r.Resolve(ctx, target)
	if err != nil {
		return OwnershipPreflight{}, err
	}
	return OwnershipPreflight{Target: target, Snapshot: snapshot, Fingerprint: snapshot.Fingerprint}, nil
}

// Execute runs an ordinary action only after an exact, second ownership read.
// A changed target/owner/presence returns 409 and the action is never called.
func (r *OwnershipResolver) Execute(ctx context.Context, preflight OwnershipPreflight, action func(context.Context, OwnershipTarget) error) error {
	if action == nil {
		return NewValidationError(map[string]string{"action": "is required"})
	}
	if strings.TrimSpace(preflight.Fingerprint) == "" {
		return NewValidationError(map[string]string{"fingerprint": "is required"})
	}
	current, err := r.Preflight(ctx, preflight.Target)
	if err != nil {
		return err
	}
	if current.Fingerprint != preflight.Fingerprint {
		return ownershipConflict(preflight.Target, current.Snapshot, "ownership fingerprint changed; reconfirm required")
	}
	if !current.Snapshot.CanNormalClose() {
		return ownershipConflict(preflight.Target, current.Snapshot, "resource is not an unmanaged resource")
	}
	return action(ctx, current.Target)
}

// ExecuteForce is intentionally a separate method from Execute. It requires
// an explicit reason and still rechecks the preflight fingerprint, but it does
// not treat business ownership as permission for the ordinary close path.
func (r *OwnershipResolver) ExecuteForce(ctx context.Context, preflight OwnershipPreflight, reason string, action func(context.Context, OwnershipTarget) error) error {
	if strings.TrimSpace(reason) == "" {
		return NewValidationError(map[string]string{"reason": "is required"})
	}
	if action == nil {
		return NewValidationError(map[string]string{"action": "is required"})
	}
	if strings.TrimSpace(preflight.Fingerprint) == "" {
		return NewValidationError(map[string]string{"fingerprint": "is required"})
	}
	current, err := r.Preflight(ctx, preflight.Target)
	if err != nil {
		return err
	}
	if current.Fingerprint != preflight.Fingerprint {
		return ownershipConflict(preflight.Target, current.Snapshot, "ownership fingerprint changed; reconfirm required")
	}
	return action(ctx, current.Target)
}

// OwnershipBatchPreflight binds a sorted collection of targets and their
// impacts. It is useful for a batch close endpoint that must perform no action
// when any target changes after the confirmation dialog.
type OwnershipBatchPreflight struct {
	Targets     []OwnershipTarget
	Snapshots   []OwnershipSnapshot
	Fingerprint string
}

func (r *OwnershipResolver) PreflightBatch(ctx context.Context, targets []OwnershipTarget) (OwnershipBatchPreflight, error) {
	if len(targets) == 0 {
		return OwnershipBatchPreflight{}, NewValidationError(map[string]string{"targets": "must not be empty"})
	}
	items := make([]OwnershipSnapshot, 0, len(targets))
	for _, target := range targets {
		snapshot, err := r.Resolve(ctx, target)
		if err != nil {
			return OwnershipBatchPreflight{}, err
		}
		items = append(items, snapshot)
	}
	sortOwnershipSnapshots(items)
	sortedTargets := make([]OwnershipTarget, 0, len(items))
	for _, item := range items {
		sortedTargets = append(sortedTargets, item.Target)
	}
	return OwnershipBatchPreflight{Targets: sortedTargets, Snapshots: items, Fingerprint: FingerprintOwnershipSnapshots(items)}, nil
}

func (r *OwnershipResolver) ExecuteBatch(ctx context.Context, preflight OwnershipBatchPreflight, action func(context.Context, OwnershipTarget) error) error {
	if action == nil {
		return NewValidationError(map[string]string{"action": "is required"})
	}
	if len(preflight.Targets) == 0 || strings.TrimSpace(preflight.Fingerprint) == "" {
		return NewValidationError(map[string]string{"preflight": "is required"})
	}
	current, err := r.PreflightBatch(ctx, preflight.Targets)
	if err != nil {
		return err
	}
	if current.Fingerprint != preflight.Fingerprint {
		return NewOwnershipConflictError(strconv.FormatInt(preflight.Targets[0].NodeID, 10), "ownership fingerprint changed; reconfirm required", ErrOwnershipConflict).WithImpacts(flattenSnapshotImpacts(current.Snapshots))
	}
	for _, snapshot := range current.Snapshots {
		if !snapshot.CanNormalClose() {
			return NewOwnershipConflictError(strconv.FormatInt(snapshot.Target.NodeID, 10), "resource is not an unmanaged resource", ErrOwnershipConflict).WithImpacts(snapshot.Impacts)
		}
	}
	for _, target := range current.Targets {
		if err := action(ctx, target); err != nil {
			return err
		}
	}
	return nil
}

func deriveOwnershipStatus(snapshot OwnershipSnapshot) OwnershipStatus {
	provenBusiness := 0
	uncertain := false
	managed := false
	for _, owner := range snapshot.Owners {
		if owner.Conflict {
			return OwnershipStatusConflicted
		}
		if owner.Type == OwnershipTypeManaged {
			if owner.Confidence != OwnershipConfidenceProven {
				uncertain = true
				continue
			}
			managed = true
			continue
		}
		if owner.Type == OwnershipTypeUnknown || owner.Confidence != OwnershipConfidenceProven {
			uncertain = true
			continue
		}
		provenBusiness++
	}
	// Different business holders may legitimately share one source: a live
	// viewer can coexist with a recording plan, continuous recording or a
	// cascade consumer. They remain owned and fail closed without being called
	// a conflict. A source marks Conflict only when it has direct evidence of
	// mutually inconsistent identity/generation state.
	if provenBusiness > 0 {
		return OwnershipStatusOwned
	}
	if uncertain {
		return OwnershipStatusUnknown
	}
	if managed && snapshot.PresenceKnown {
		return OwnershipStatusManaged
	}
	if !snapshot.PresenceKnown {
		return OwnershipStatusUnknown
	}
	if !snapshot.Present {
		return OwnershipStatusAbsent
	}
	return OwnershipStatusUnknown
}

func normalizeOwnershipEvidence(item OwnershipEvidence) OwnershipEvidence {
	item.Type = OwnershipType(strings.TrimSpace(string(item.Type)))
	if !knownOwnershipType(item.Type) {
		item.Type = OwnershipTypeUnknown
		item.Confidence = OwnershipConfidenceUncertain
		if item.Reason == "" {
			item.Reason = "ownership source type is not recognized"
		}
	}
	if item.Confidence != OwnershipConfidenceProven {
		item.Confidence = OwnershipConfidenceUncertain
	}
	item.ResourceType = sanitizeErrorText(strings.TrimSpace(item.ResourceType))
	item.Key = sanitizeErrorText(strings.TrimSpace(item.Key))
	item.Owner = sanitizeErrorText(strings.TrimSpace(item.Owner))
	item.Reason = sanitizeErrorText(strings.TrimSpace(item.Reason))
	return item
}

func knownOwnershipType(value OwnershipType) bool {
	switch value {
	case OwnershipTypeRealtimePlayback, OwnershipTypeDevicePlayback, OwnershipTypeTalk,
		OwnershipTypeCascade, OwnershipTypeRecordingPlan, OwnershipTypeContinuousRecording,
		OwnershipTypeRecordingSession, OwnershipTypeManaged, OwnershipTypeUnknown:
		return true
	default:
		return false
	}
}

func appendUniqueEvidence(items []OwnershipEvidence, item OwnershipEvidence) []OwnershipEvidence {
	for _, existing := range items {
		if existing.Type == item.Type && existing.ResourceType == item.ResourceType && existing.Key == item.Key &&
			existing.Owner == item.Owner && existing.Confidence == item.Confidence && existing.Reason == item.Reason && existing.Fingerprint == item.Fingerprint {
			return items
		}
	}
	return append(items, item)
}

func sortOwnershipEvidence(items []OwnershipEvidence) {
	sort.SliceStable(items, func(i, j int) bool {
		left, right := ownershipTypePriority(items[i].Type), ownershipTypePriority(items[j].Type)
		if left != right {
			return left < right
		}
		return canonicalOwnershipEvidence(items[i]) < canonicalOwnershipEvidence(items[j])
	})
}

func ownershipTypePriority(value OwnershipType) int {
	switch value {
	case OwnershipTypeRealtimePlayback:
		return 1
	case OwnershipTypeDevicePlayback, OwnershipTypeTalk, OwnershipTypeCascade:
		return 2
	case OwnershipTypeRecordingPlan, OwnershipTypeContinuousRecording, OwnershipTypeRecordingSession:
		return 3
	case OwnershipTypeManaged:
		return 4
	case OwnershipTypeUnknown:
		return 5
	default:
		return 6
	}
}

func canonicalOwnershipEvidence(item OwnershipEvidence) string {
	return canonicalParts(
		string(item.Type), item.ResourceType, item.Key, item.Owner,
		string(item.Confidence), item.Reason, item.Fingerprint, strconv.FormatBool(item.Conflict),
	)
}

func ownershipConflict(target OwnershipTarget, snapshot OwnershipSnapshot, reason string) error {
	return NewOwnershipConflictError(strconv.FormatInt(target.NodeID, 10), reason, ErrOwnershipConflict).WithImpacts(snapshot.Impacts)
}

func impactsForOwnership(target OwnershipTarget, owners []OwnershipEvidence) []Impact {
	if len(owners) == 0 {
		return nil
	}
	impacts := make([]Impact, 0, len(owners))
	for _, owner := range owners {
		resourceType := owner.ResourceType
		if resourceType == "" {
			resourceType = string(owner.Type)
		}
		media := target.Media
		impacts = append(impacts, Impact{
			ResourceType:  resourceType,
			ResourceKey:   owner.Key,
			Owner:         owner.Owner,
			Reason:        owner.Reason,
			MediaIdentity: &media,
		})
	}
	return impacts
}

func flattenSnapshotImpacts(snapshots []OwnershipSnapshot) []Impact {
	impacts := make([]Impact, 0)
	for _, snapshot := range snapshots {
		impacts = append(impacts, snapshot.Impacts...)
	}
	return impacts
}

func sortOwnershipSnapshots(items []OwnershipSnapshot) {
	sort.SliceStable(items, func(i, j int) bool {
		return canonicalOwnershipTarget(items[i].Target) < canonicalOwnershipTarget(items[j].Target)
	})
}

// LiveSessionReader and LiveLocationReader are intentionally narrower than
// play.Service: ownership inspection needs only the public current generation
// and the versioned LocationMap lookup.
type LiveSessionReader interface {
	CurrentSession(string) (play.LiveSession, bool)
}

type LiveLocationReader interface {
	LookupCurrent(string) (stream.LiveRef, bool)
}

type liveOwnershipAdapter struct {
	sessions  LiveSessionReader
	locations LiveLocationReader
}

func NewLiveOwnershipAdapter(sessions LiveSessionReader, locations LiveLocationReader) OwnershipSource {
	return liveOwnershipAdapter{sessions: sessions, locations: locations}
}

func (a liveOwnershipAdapter) Resolve(_ context.Context, target OwnershipTarget) ([]OwnershipEvidence, error) {
	if a.sessions == nil {
		return nil, nil
	}
	session, ok := a.sessions.CurrentSession(target.Media.Stream)
	if !ok {
		return nil, nil
	}
	base := OwnershipEvidence{Type: OwnershipTypeRealtimePlayback, Key: target.Media.Stream, Owner: session.DeviceID + "/" + session.ChannelID}
	if target.Media.App != "rtp" || session.State != play.LiveStateReady || session.Generation == 0 {
		base.Confidence = OwnershipConfidenceUncertain
		base.Reason = "current live session cannot be matched to this media target"
		return []OwnershipEvidence{base}, nil
	}
	if session.StreamID != target.Media.Stream || session.NodeID == 0 {
		base.Confidence = OwnershipConfidenceUncertain
		base.Conflict = session.StreamID != "" && session.StreamID != target.Media.Stream
		base.Reason = "current live session identity conflicts with this media target"
		return []OwnershipEvidence{base}, nil
	}
	if session.NodeID != target.NodeID {
		base.Confidence = OwnershipConfidenceUncertain
		base.Conflict = true
		base.Reason = "current live session node conflicts with this media target"
		return []OwnershipEvidence{base}, nil
	}
	if a.locations == nil {
		base.Confidence = OwnershipConfidenceUncertain
		base.Reason = "current live location is unavailable"
		return []OwnershipEvidence{base}, nil
	}
	current, ok := a.locations.LookupCurrent(target.Media.Stream)
	if !ok {
		base.Confidence = OwnershipConfidenceUncertain
		base.Reason = "live generation does not match the current location"
		return []OwnershipEvidence{base}, nil
	}
	if current != session.Ref() {
		base.Confidence = OwnershipConfidenceUncertain
		base.Conflict = true
		base.Reason = "live generation conflicts with the current location"
		return []OwnershipEvidence{base}, nil
	}
	base.Confidence = OwnershipConfidenceProven
	base.Key = target.Media.Stream + "/" + strconv.FormatUint(session.Generation, 10)
	base.Reason = "current live generation"
	return []OwnershipEvidence{base}, nil
}

type DevicePlaybackReader interface {
	GetByStreamID(string) (*playback.Session, bool)
}

type devicePlaybackOwnershipAdapter struct{ sessions DevicePlaybackReader }

func NewDevicePlaybackOwnershipAdapter(sessions DevicePlaybackReader) OwnershipSource {
	return devicePlaybackOwnershipAdapter{sessions: sessions}
}

func (a devicePlaybackOwnershipAdapter) Resolve(_ context.Context, target OwnershipTarget) ([]OwnershipEvidence, error) {
	if a.sessions == nil {
		return nil, nil
	}
	session, ok := a.sessions.GetByStreamID(target.Media.Stream)
	if !ok || session == nil || session.State.IsTerminal() {
		return nil, nil
	}
	base := OwnershipEvidence{Type: OwnershipTypeDevicePlayback, Key: session.ID, Owner: session.OwnerID}
	ownerNode, err := strconv.ParseInt(strings.TrimSpace(session.NodeID), 10, 64)
	if err != nil || ownerNode <= 0 || ownerNode != target.NodeID || target.Media.App != "rtp" {
		base.Confidence = OwnershipConfidenceUncertain
		base.Reason = "device playback session cannot be matched to this media target"
		return []OwnershipEvidence{base}, nil
	}
	base.Confidence = OwnershipConfidenceProven
	base.Reason = "active device recording playback session"
	return []OwnershipEvidence{base}, nil
}

type TalkSessionReader interface {
	ListNonterminal(context.Context) ([]gbmodels.GbTalkSession, error)
}

type talkOwnershipAdapter struct {
	sessions TalkSessionReader
	vhost    string
}

// NewTalkOwnershipAdapter requires the vhost proven by the talk media
// adapter. The empty value intentionally keeps matched sessions uncertain:
// GbTalkSession itself does not persist a vhost field.
func NewTalkOwnershipAdapter(sessions TalkSessionReader, provenVHost string) OwnershipSource {
	return talkOwnershipAdapter{sessions: sessions, vhost: strings.TrimSpace(provenVHost)}
}

func (a talkOwnershipAdapter) Resolve(ctx context.Context, target OwnershipTarget) ([]OwnershipEvidence, error) {
	if a.sessions == nil {
		return nil, nil
	}
	sessions, err := a.sessions.ListNonterminal(ctx)
	if err != nil {
		return nil, err
	}
	evidence := make([]OwnershipEvidence, 0)
	for _, session := range sessions {
		if session.State.IsTerminal() || session.NodeID != target.NodeID || session.App != target.Media.App {
			continue
		}
		matches := session.SourceStream == target.Media.Stream || session.RecvStream == target.Media.Stream
		if !matches {
			continue
		}
		item := OwnershipEvidence{Type: OwnershipTypeTalk, Key: session.SessionID, Owner: strconv.FormatUint(uint64(session.ChannelID), 10)}
		if a.vhost == "" || a.vhost != target.Media.Vhost {
			item.Confidence = OwnershipConfidenceUncertain
			item.Reason = "talk session vhost is not proven by the read-only record"
		} else {
			item.Confidence = OwnershipConfidenceProven
			item.Reason = "active talk session"
		}
		evidence = appendUniqueEvidence(evidence, item)
	}
	return evidence, nil
}

type RecordingSessionReader interface {
	FindSessionByMedia(context.Context, int64, string, string, string) (*gbmodels.GbRecordingSession, error)
}

type recordingOwnershipAdapter struct {
	sessions RecordingSessionReader
	kind     OwnershipType
}

func NewRecordingSessionOwnershipAdapter(sessions RecordingSessionReader) OwnershipSource {
	return recordingOwnershipAdapter{sessions: sessions, kind: OwnershipTypeRecordingSession}
}

func NewContinuousRecordingOwnershipAdapter(sessions RecordingSessionReader) OwnershipSource {
	return recordingOwnershipAdapter{sessions: sessions, kind: OwnershipTypeContinuousRecording}
}

func (a recordingOwnershipAdapter) Resolve(ctx context.Context, target OwnershipTarget) ([]OwnershipEvidence, error) {
	if a.sessions == nil {
		return nil, nil
	}
	session, err := a.sessions.FindSessionByMedia(ctx, target.NodeID, target.Media.Vhost, target.Media.App, target.Media.Stream)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, nil
	}
	switch session.State {
	case gbmodels.RecordingSessionStateStarting, gbmodels.RecordingSessionStateRecording, gbmodels.RecordingSessionStateStopping:
		return []OwnershipEvidence{{
			Type: a.kind, ResourceType: string(a.kind), Key: strconv.FormatUint(session.ID, 10),
			Owner: strconv.FormatUint(uint64(session.ChannelID), 10), Confidence: OwnershipConfidenceProven,
			Reason: "active recording session",
		}}, nil
	default:
		return nil, nil
	}
}

type ManagedResourceReader interface {
	List(context.Context, repo.ManagedResourceFilter) ([]gbmodels.GbZLMManagedResource, error)
}

type managedResourceOwnershipAdapter struct{ ledger ManagedResourceReader }

func NewManagedResourceOwnershipAdapter(ledger ManagedResourceReader) OwnershipSource {
	return managedResourceOwnershipAdapter{ledger: ledger}
}

func (a managedResourceOwnershipAdapter) Resolve(ctx context.Context, target OwnershipTarget) ([]OwnershipEvidence, error) {
	if a.ledger == nil {
		return nil, nil
	}
	rows, err := a.ledger.List(ctx, repo.ManagedResourceFilter{NodeID: target.NodeID})
	if err != nil {
		return nil, err
	}
	evidence := make([]OwnershipEvidence, 0)
	for _, row := range rows {
		if row.TombstonedAt != nil || row.NodeID != target.NodeID || row.App != target.Media.App || row.Stream != target.Media.Stream {
			continue
		}
		item := OwnershipEvidence{
			Type: OwnershipTypeManaged, ResourceType: row.ResourceType, Key: row.ResourceKey,
			Owner: fmt.Sprintf("user:%d", row.CreatedBy), Fingerprint: row.IdentityFingerprint,
		}
		if row.Schema == "" || row.Vhost == "" {
			item.Confidence = OwnershipConfidenceUncertain
			item.Reason = "management ledger row has incomplete media identity"
		} else if row.Schema != target.Media.Schema || row.Vhost != target.Media.Vhost {
			continue
		} else {
			item.Confidence = OwnershipConfidenceProven
			item.Reason = "management ledger provenance"
		}
		evidence = appendUniqueEvidence(evidence, item)
	}
	return evidence, nil
}

type LeasePresenceReader interface {
	HasLease(string) bool
}

type uncertainLeaseOwnershipAdapter struct {
	kind   OwnershipType
	leases LeasePresenceReader
}

// NewCascadeOwnershipAdapter and NewRecordingPlanOwnershipAdapter preserve
// fail-closed semantics when the existing lease API exposes only stream
// presence and cannot prove node/app/vhost identity.
func NewCascadeOwnershipAdapter(leases LeasePresenceReader) OwnershipSource {
	return uncertainLeaseOwnershipAdapter{kind: OwnershipTypeCascade, leases: leases}
}

func NewRecordingPlanOwnershipAdapter(leases LeasePresenceReader) OwnershipSource {
	return uncertainLeaseOwnershipAdapter{kind: OwnershipTypeRecordingPlan, leases: leases}
}

func (a uncertainLeaseOwnershipAdapter) Resolve(_ context.Context, target OwnershipTarget) ([]OwnershipEvidence, error) {
	if a.leases == nil || !a.leases.HasLease(target.Media.Stream) {
		return nil, nil
	}
	return []OwnershipEvidence{{
		Type: a.kind, Key: target.Media.Stream, Confidence: OwnershipConfidenceUncertain,
		Reason: "lease API does not expose complete media identity",
	}}, nil
}
