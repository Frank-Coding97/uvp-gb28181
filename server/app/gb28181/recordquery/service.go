package recordquery

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
)

type Service struct {
	sender    TrackedSender
	options   Options
	registry  *Registry
	snapshots *ResultSnapshotStore
	lifecycle context.Context
	cancel    context.CancelFunc
	nextSN    atomic.Uint64
	closeOnce sync.Once
}

func NewService(sender TrackedSender, options Options) (*Service, error) {
	if sender == nil || options.Timeout <= 0 || options.MaxActiveQueries <= 0 ||
		options.MaxRecordsPerQuery <= 0 || options.ResultTTL <= 0 || options.Location == nil {
		return nil, queryError(ErrorCodeInvalidArgument, ErrInvalidArgument)
	}
	if options.CompletionQuiet <= 0 {
		options.CompletionQuiet = 100 * time.Millisecond
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	registry, err := NewRegistry(options.MaxActiveQueries)
	if err != nil {
		return nil, err
	}
	snapshots, err := NewResultSnapshotStore(options.ResultTTL, options.Now)
	if err != nil {
		return nil, err
	}
	lifecycle, cancel := context.WithCancel(context.Background())
	return &Service{sender: sender, options: options, registry: registry, snapshots: snapshots, lifecycle: lifecycle, cancel: cancel}, nil
}

func (s *Service) Query(ctx context.Context, request QueryRequest) (QueryResult, error) {
	if s == nil {
		return QueryResult{}, queryError(ErrorCodeUnavailable, ErrUnavailable)
	}
	startedAt := s.options.Now()
	result := QueryResult{StartedAt: startedAt}
	if err := validateQueryRequest(request); err != nil {
		return result, err
	}
	queryID, err := randomToken(18)
	if err != nil {
		return result, queryError(ErrorCodeUnavailable, err)
	}
	result.QueryID = queryID
	request.DeviceCode = normalizeIdentity(request.DeviceCode)
	request.ChannelCode = normalizeIdentity(request.ChannelCode)
	request.Destination = normalizeIdentity(request.Destination)

	sn := s.nextSequence()
	result.SN = sn
	entry, err := s.registry.register(request.DeviceCode, sn, s.options.MaxRecordsPerQuery, request.ChannelCode)
	if err != nil {
		return result, err
	}
	defer s.registry.remove(request.DeviceCode, sn)
	if err := s.snapshots.BeginQuery(request.OwnerUserID, request.ChannelID, queryID); err != nil {
		return result, err
	}

	body, err := manscdp.BuildRecordInfoQuery(manscdp.RecordInfoQuery{
		SN: sn, DeviceID: request.ChannelCode, StartTime: request.StartTime, EndTime: request.EndTime,
		Type: request.Type, Secrecy: request.Secrecy, RecorderID: request.RecorderID,
	})
	if err != nil {
		return result, queryError(ErrorCodeInvalidArgument, err)
	}
	sendCtx, cancelSend := context.WithCancel(ctx)
	stopLifecycleCancel := context.AfterFunc(s.lifecycle, cancelSend)
	sendResult, sendErr := s.sender.SendMessageTracked(sendCtx, request.DeviceCode, request.Destination, request.Transport, body)
	stopLifecycleCancel()
	cancelSend()
	if s.lifecycle.Err() != nil {
		result.Status = QueryStatusUnavailable
		result.FinishedAt = s.options.Now()
		return result, queryError(ErrorCodeUnavailable, ErrUnavailable)
	}
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return s.finishDeadline(result, request, entry.snapshot())
		}
		result, _ = s.finish(result, request, entry.snapshot(), QueryStatusCanceled, "")
		return result, ctx.Err()
	}
	if sendErr != nil || sendResult.StatusCode < 200 || sendResult.StatusCode >= 300 {
		result.Status = QueryStatusSendFailed
		result.FinishedAt = s.options.Now()
		if sendErr == nil {
			sendErr = fmt.Errorf("SIP MESSAGE status %d", sendResult.StatusCode)
		}
		return result, queryError(ErrorCodeSendFailed, errors.Join(ErrSendFailed, sendErr))
	}

	timeout := time.NewTimer(s.options.Timeout)
	defer stopTimer(timeout)
	var quiet *time.Timer
	var quietC <-chan time.Time
	defer func() { stopTimer(quiet) }()

	for {
		snapshot := entry.snapshot()
		if snapshot.explicitEmpty && len(snapshot.records) == 0 {
			return s.finish(result, request, snapshot, QueryStatusEmpty, "")
		}
		if snapshot.capacityReached {
			return s.finish(result, request, snapshot, QueryStatusPartial, ErrorCodeCapacity)
		}
		completeCandidate := snapshot.declaredTotal > 0 && len(snapshot.records) >= snapshot.declaredTotal
		if completeCandidate && quietC == nil {
			quiet = time.NewTimer(s.options.CompletionQuiet)
			quietC = quiet.C
		} else if !completeCandidate && quietC != nil {
			stopTimer(quiet)
			quiet = nil
			quietC = nil
		}

		select {
		case <-entry.updates:
			if quietC != nil {
				stopTimer(quiet)
				quiet = nil
				quietC = nil
			}
		case <-quietC:
			latest := entry.snapshot()
			if latest.declaredTotal > 0 && len(latest.records) >= latest.declaredTotal {
				return s.finish(result, request, latest, QueryStatusComplete, "")
			}
			quiet = nil
			quietC = nil
		case <-timeout.C:
			return s.finishDeadline(result, request, entry.snapshot())
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return s.finishDeadline(result, request, entry.snapshot())
			}
			result, _ = s.finish(result, request, entry.snapshot(), QueryStatusCanceled, "")
			return result, ctx.Err()
		case <-s.registry.Done():
			result, _ = s.finish(result, request, entry.snapshot(), QueryStatusUnavailable, ErrorCodeUnavailable)
			return result, queryError(ErrorCodeUnavailable, ErrUnavailable)
		}
	}
}

func (s *Service) Accept(senderDeviceCode string, response *manscdp.RecordInfoResponse) bool {
	return s != nil && s.registry.Accept(normalizeIdentity(senderDeviceCode), response)
}

// OnRecordInfo is the parsed-response sink used by the SIP MESSAGE handler.
// Unknown, stale, or mismatched responses are intentionally ignored.
func (s *Service) OnRecordInfo(_ context.Context, senderDeviceCode string, response *manscdp.RecordInfoResponse) error {
	s.Accept(senderDeviceCode, response)
	return nil
}

func (s *Service) Active() int {
	if s == nil {
		return 0
	}
	return s.registry.Active()
}

func (s *Service) Snapshots() *ResultSnapshotStore {
	if s == nil {
		return nil
	}
	return s.snapshots
}

func (s *Service) Close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		s.cancel()
		s.registry.Close()
		s.snapshots.Close()
	})
}

func (s *Service) finishDeadline(result QueryResult, request QueryRequest, snapshot entrySnapshot) (QueryResult, error) {
	if len(snapshot.records) == 0 {
		return s.finish(result, request, snapshot, QueryStatusTimeout, ErrorCodeTimeout)
	}
	result, _ = s.finish(result, request, snapshot, QueryStatusPartial, ErrorCodeTimeout)
	return result, nil
}

func (s *Service) finish(result QueryResult, request QueryRequest, snapshot entrySnapshot, status QueryStatus, reason ErrorCode) (QueryResult, error) {
	sortRecordItems(snapshot.records, s.options.Location)
	result.Status = status
	result.PartialReason = reason
	result.DeclaredTotal = snapshot.declaredTotal
	result.ReceivedCount = len(snapshot.records)
	result.Incomplete = status == QueryStatusPartial || status == QueryStatusTimeout || status == QueryStatusCanceled || status == QueryStatusUnavailable
	result.FinishedAt = s.options.Now()
	result.Records = make([]Record, 0, len(snapshot.records))
	issueSnapshots := status == QueryStatusComplete || status == QueryStatusPartial
	for _, item := range snapshot.records {
		start, startErr := ParseWallClock(item.StartTime, s.options.Location)
		end, endErr := ParseWallClock(item.EndTime, s.options.Location)
		if startErr != nil || endErr != nil || !end.After(start) {
			continue
		}
		if !issueSnapshots {
			result.Records = append(result.Records, Record{RecordInfoItem: item})
			continue
		}
		issued, issueErr := s.snapshots.Issue(SnapshotInput{
			OwnerUserID: request.OwnerUserID, ChannelID: request.ChannelID,
			DeviceCode: request.DeviceCode, ChannelCode: request.ChannelCode, QueryID: result.QueryID,
			SegmentStart: start, SegmentEnd: end, Record: item,
		})
		if issueErr != nil {
			result.Incomplete = true
			continue
		}
		result.Records = append(result.Records, Record{RecordInfoItem: item, RecordKey: issued.RecordKey})
	}
	result.ReceivedCount = len(result.Records)
	if reason == "" {
		return result, nil
	}
	if status == QueryStatusPartial {
		return result, nil
	}
	return result, queryError(reason, sentinelFor(reason))
}

func (s *Service) nextSequence() int {
	for {
		next := s.nextSN.Add(1)
		if next > 0 && next <= uint64(^uint(0)>>1) {
			return int(next)
		}
		s.nextSN.Store(0)
	}
}

func validateQueryRequest(request QueryRequest) error {
	if request.OwnerUserID == 0 || request.ChannelID == 0 || normalizeIdentity(request.DeviceCode) == "" ||
		normalizeIdentity(request.ChannelCode) == "" || normalizeIdentity(request.Destination) == "" ||
		request.StartTime.IsZero() || !request.EndTime.After(request.StartTime) {
		return queryError(ErrorCodeInvalidArgument, ErrInvalidArgument)
	}
	requestType := manscdp.RecordInfoQueryType(strings.ToLower(strings.TrimSpace(string(request.Type))))
	if requestType != "" && requestType != manscdp.RecordInfoQueryTypeAll && requestType != manscdp.RecordInfoQueryTypeManual && requestType != manscdp.RecordInfoQueryTypeAlarm {
		return queryError(ErrorCodeInvalidArgument, ErrInvalidArgument)
	}
	if request.Secrecy != 0 && request.Secrecy != 1 {
		return queryError(ErrorCodeInvalidArgument, ErrInvalidArgument)
	}
	return nil
}

func sortRecordItems(items []manscdp.RecordInfoItem, location *time.Location) {
	sort.SliceStable(items, func(i, j int) bool {
		leftStart, leftErr := ParseWallClock(items[i].StartTime, location)
		rightStart, rightErr := ParseWallClock(items[j].StartTime, location)
		if leftErr != nil || rightErr != nil {
			if leftErr == nil {
				return true
			}
			if rightErr == nil {
				return false
			}
			return recordDigest(items[i]) < recordDigest(items[j])
		}
		if !leftStart.Equal(rightStart) {
			return leftStart.Before(rightStart)
		}
		leftEnd, leftEndErr := ParseWallClock(items[i].EndTime, location)
		rightEnd, rightEndErr := ParseWallClock(items[j].EndTime, location)
		if leftEndErr == nil && rightEndErr == nil && !leftEnd.Equal(rightEnd) {
			return leftEnd.Before(rightEnd)
		}
		return recordDigest(items[i]) < recordDigest(items[j])
	})
}

func stopTimer(timer *time.Timer) {
	if timer == nil {
		return
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}
