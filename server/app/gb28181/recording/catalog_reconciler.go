package recording

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

const (
	ReconcileTriggerBootstrap = "bootstrap"
	ReconcileTriggerScheduled = "scheduled"
	ReconcileTriggerManual    = "manual"
)

var ErrCatalogReconcileRunning = errors.New("cloud recording reconciliation already running")

type CatalogRecordClient interface {
	GetMP4RecordFiles(context.Context, string, string, string, string) ([]zlm.MP4RecordFile, error)
}

type CatalogNodeLookup interface {
	Get(int64) (*node.Node, bool)
	List() []*node.Node
}

type CatalogReconcileRepo interface {
	ListReconcileCandidates(context.Context) ([]ReconcileCandidate, error)
	ApplyReconcileUnit(context.Context, ReconcileCandidate, time.Time, []zlm.MP4RecordFile, time.Time) (ReconcileUnitResult, error)
	SaveReconcileState(context.Context, *models.GbRecordingReconcileState) error
}

type CatalogReconciler struct {
	repo      CatalogReconcileRepo
	locations LocationLookup
	nodes     CatalogNodeLookup
	client    func(*node.Node) CatalogRecordClient
	now       func() time.Time

	mu      sync.Mutex
	running map[int64]struct{}
}

func NewCatalogReconciler(repo CatalogReconcileRepo, locations LocationLookup, nodes CatalogNodeLookup, client func(*node.Node) CatalogRecordClient) *CatalogReconciler {
	return &CatalogReconciler{repo: repo, locations: locations, nodes: nodes, client: client, now: time.Now, running: make(map[int64]struct{})}
}

func (r *CatalogReconciler) RunNode(ctx context.Context, nodeID int64, trigger string, start, end *time.Time) (models.GbRecordingReconcileState, error) {
	if !r.beginNode(nodeID) {
		return models.GbRecordingReconcileState{NodeID: nodeID, Status: models.RecordingReconcileRunning}, ErrCatalogReconcileRunning
	}
	defer r.endNode(nodeID)

	now := r.currentTime()
	requestedStart, requestedEnd, effectiveStart, effectiveEnd := reconcileWindow(now, trigger, start, end)
	state := models.GbRecordingReconcileState{
		NodeID: nodeID, Status: models.RecordingReconcileRunning, TriggerSource: trigger,
		RequestedStart: &requestedStart, RequestedEnd: &requestedEnd,
		EffectiveStart: &effectiveStart, EffectiveEnd: &effectiveEnd,
		StartedAt: &now, UpdatedAt: now,
	}
	if err := r.repo.SaveReconcileState(ctx, &state); err != nil {
		return state, err
	}

	n, ok := r.nodes.Get(nodeID)
	if !ok || n.State == node.StateOffline {
		return r.finishFailure(ctx, state, errors.New("recording node unavailable"))
	}
	candidates, err := r.candidatesForNode(ctx, nodeID)
	if err != nil {
		return r.finishFailure(ctx, state, err)
	}
	state.CandidateCount = len(candidates)
	client := r.client(n)
	dates := calendarDates(effectiveStart, effectiveEnd)
	var scanErrors []error
	for _, candidate := range candidates {
		for _, date := range dates {
			files, scanErr := client.GetMP4RecordFiles(ctx, candidate.VHost, candidate.App, candidate.Stream, date.Format("2006-01-02"))
			if errors.Is(scanErr, zlm.ErrRecordingNotFound) {
				files, scanErr = nil, nil
			}
			if scanErr != nil {
				state.FailureCount++
				scanErrors = append(scanErrors, scanErr)
				continue
			}
			unit, applyErr := r.repo.ApplyReconcileUnit(ctx, candidate, date, files, now)
			if applyErr != nil {
				state.FailureCount++
				scanErrors = append(scanErrors, applyErr)
				continue
			}
			state.SuccessCount++
			state.DiscoveredCount += unit.Discovered
			state.InsertedCount += unit.Inserted
			state.UpdatedCount += unit.Updated
			state.MissingCount += unit.Missing
		}
	}

	finished := r.currentTime()
	state.FinishedAt = &finished
	state.UpdatedAt = finished
	switch {
	case state.FailureCount == 0:
		state.Status = models.RecordingReconcileSucceeded
	case state.SuccessCount == 0:
		state.Status = models.RecordingReconcileFailed
	default:
		state.Status = models.RecordingReconcilePartial
	}
	if state.FailureCount > 0 {
		state.LastError = "one or more recording scan units failed"
	}
	if err := r.repo.SaveReconcileState(ctx, &state); err != nil {
		scanErrors = append(scanErrors, err)
	}
	return state, errors.Join(scanErrors...)
}

func (r *CatalogReconciler) candidatesForNode(ctx context.Context, nodeID int64) ([]ReconcileCandidate, error) {
	candidates, err := r.repo.ListReconcileCandidates(ctx)
	if err != nil {
		return nil, err
	}
	unique := make(map[string]ReconcileCandidate)
	for _, candidate := range candidates {
		if candidate.NodeID == 0 {
			if r.locations == nil {
				continue
			}
			locatedNodeID, ok := r.locations.Lookup(candidate.Stream)
			if !ok {
				continue
			}
			candidate.NodeID = locatedNodeID
		}
		if candidate.NodeID != nodeID {
			continue
		}
		key := reconcileCandidateKey(candidate)
		if existing, found := unique[key]; !found || (existing.SessionID == nil && candidate.SessionID != nil) {
			unique[key] = candidate
		}
	}
	result := make([]ReconcileCandidate, 0, len(unique))
	for _, candidate := range unique {
		result = append(result, candidate)
	}
	sort.Slice(result, func(i, j int) bool { return reconcileCandidateKey(result[i]) < reconcileCandidateKey(result[j]) })
	return result, nil
}

func reconcileWindow(now time.Time, trigger string, start, end *time.Time) (time.Time, time.Time, time.Time, time.Time) {
	today := calendarDate(now)
	requestedEnd := today
	requestedStart := today.AddDate(0, 0, -6)
	if trigger == ReconcileTriggerScheduled {
		requestedStart = today.AddDate(0, 0, -1)
	}
	if start != nil {
		requestedStart = calendarDate(*start)
	}
	if end != nil {
		requestedEnd = calendarDate(*end)
	}
	if requestedEnd.Before(requestedStart) {
		requestedStart, requestedEnd = requestedEnd, requestedStart
	}
	return requestedStart, requestedEnd, requestedStart.AddDate(0, 0, -1), requestedEnd.AddDate(0, 0, 1)
}

func calendarDate(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, value.Location())
}

func calendarDates(start, end time.Time) []time.Time {
	result := make([]time.Time, 0, int(end.Sub(start).Hours()/24)+1)
	for date := start; !date.After(end); date = date.AddDate(0, 0, 1) {
		result = append(result, date)
	}
	return result
}

func (r *CatalogReconciler) finishFailure(ctx context.Context, state models.GbRecordingReconcileState, cause error) (models.GbRecordingReconcileState, error) {
	finished := r.currentTime()
	state.Status = models.RecordingReconcileFailed
	state.FailureCount++
	state.LastError = "recording reconciliation failed"
	state.FinishedAt = &finished
	state.UpdatedAt = finished
	if err := r.repo.SaveReconcileState(ctx, &state); err != nil {
		return state, errors.Join(cause, err)
	}
	return state, cause
}

func (r *CatalogReconciler) beginNode(nodeID int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.running[nodeID]; exists {
		return false
	}
	r.running[nodeID] = struct{}{}
	return true
}

func (r *CatalogReconciler) endNode(nodeID int64) {
	r.mu.Lock()
	delete(r.running, nodeID)
	r.mu.Unlock()
}

func (r *CatalogReconciler) currentTime() time.Time {
	if r.now == nil {
		return time.Now()
	}
	return r.now()
}
