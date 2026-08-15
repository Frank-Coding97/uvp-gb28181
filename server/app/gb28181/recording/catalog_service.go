package recording

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

var (
	ErrCatalogAccessRevoked     = errors.New("cloud recording access revoked")
	ErrCatalogNodeMissing       = errors.New("cloud recording node missing")
	ErrCatalogNodeOffline       = errors.New("cloud recording node offline")
	ErrCatalogFileMissing       = errors.New("cloud recording file missing")
	ErrCatalogAccessUnavailable = errors.New("cloud recording access unavailable")
)

type CatalogAccess struct {
	FullAccess bool
	DeptIDs    []uint
}

type CatalogAccessResolver func(context.Context, uint) (CatalogAccess, error)
type CatalogPermissionChecker func(context.Context, uint, string, string) (bool, error)
type CatalogDownloaderFactory func(*node.Node) ContentDownloader

type CatalogScheduler interface {
	Enqueue(string, []int64, *time.Time, *time.Time) ([]int64, error)
}

type CatalogQueryRepository interface {
	ListCatalogFiles(context.Context, FileQuery) (FilePage, error)
	CatalogOptions(context.Context, FileQuery) (CatalogOptions, error)
	GetCatalogFile(context.Context, uint64, []uint, bool) (*models.GbRecordingFile, error)
	ListActiveCatalogSessions(context.Context, []uint, bool) ([]ActiveCatalogSession, error)
	ListCatalogReconcileStates(context.Context) ([]models.GbRecordingReconcileState, error)
}

type CatalogServiceConfig struct {
	Repo            CatalogQueryRepository
	Nodes           CatalogNodeLookup
	Scheduler       CatalogScheduler
	Signer          *CapabilitySigner
	ResolveAccess   CatalogAccessResolver
	CheckPermission CatalogPermissionChecker
	NewDownloader   CatalogDownloaderFactory
	Proxy           *ContentProxy
	Downloads       *DownloadRegistry
}

type CatalogService struct {
	repo            CatalogQueryRepository
	nodes           CatalogNodeLookup
	scheduler       CatalogScheduler
	signer          *CapabilitySigner
	resolveAccess   CatalogAccessResolver
	checkPermission CatalogPermissionChecker
	newDownloader   CatalogDownloaderFactory
	proxy           *ContentProxy
	downloads       *DownloadRegistry
}

type FileDTOPage struct {
	List     []FileDTO `json:"list"`
	Total    int64     `json:"total"`
	Page     int       `json:"page"`
	PageSize int       `json:"pageSize"`
}

func NewCatalogService(config CatalogServiceConfig) *CatalogService {
	proxy := config.Proxy
	if proxy == nil {
		proxy = NewContentProxy()
	}
	return &CatalogService{
		repo: config.Repo, nodes: config.Nodes, scheduler: config.Scheduler, signer: config.Signer,
		resolveAccess: config.ResolveAccess, checkPermission: config.CheckPermission,
		newDownloader: config.NewDownloader, proxy: proxy,
		downloads: config.Downloads,
	}
}

func (s *CatalogService) CreateDownload(ctx context.Context, userID uint, fileID uint64) (DownloadTaskView, string, error) {
	if s.downloads == nil {
		return DownloadTaskView{}, "", ErrCatalogAccessUnavailable
	}
	if _, err := s.downloadableFile(ctx, userID, fileID); err != nil {
		return DownloadTaskView{}, "", err
	}
	return s.downloads.Create(userID, strconv.FormatUint(fileID, 10))
}

func (s *CatalogService) DownloadStatus(_ context.Context, userID uint, taskID string) (DownloadTaskView, error) {
	if s.downloads == nil {
		return DownloadTaskView{}, ErrCatalogAccessUnavailable
	}
	return s.downloads.Get(taskID, userID)
}

func (s *CatalogService) CancelDownload(_ context.Context, userID uint, taskID string) (DownloadTaskView, error) {
	if s.downloads == nil {
		return DownloadTaskView{}, ErrCatalogAccessUnavailable
	}
	return s.downloads.Cancel(taskID, userID)
}

func (s *CatalogService) CloseDownloads() {
	if s != nil && s.downloads != nil {
		s.downloads.Close()
	}
}

func (s *CatalogService) ClaimDownload(ctx context.Context, writer http.ResponseWriter, taskID, ticket, byteRange string, onClaimed func()) error {
	if s.downloads == nil {
		return ErrCatalogAccessUnavailable
	}
	if err := validateDownloadRange(byteRange); err != nil {
		return err
	}
	view, taskCtx, _, err := s.downloads.Claim(taskID, ticket)
	if err != nil {
		return err
	}
	if onClaimed != nil {
		onClaimed()
	}
	defer func() {
		if recoverValue := recover(); recoverValue != nil {
			s.downloads.Finish(taskID, DownloadStatusFailed, "internal")
			panic(recoverValue)
		}
	}()
	transferCtx, transferCancel := context.WithCancel(ctx)
	defer transferCancel()
	go func() {
		select {
		case <-taskCtx.Done():
			transferCancel()
		case <-transferCtx.Done():
		}
	}()
	fileID, err := strconv.ParseUint(view.FileID, 10, 64)
	if err != nil || fileID == 0 {
		s.downloads.Finish(taskID, DownloadStatusFailed, "unavailable")
		return ErrCatalogAccessRevoked
	}
	ownerUserID, ok := s.downloads.Owner(taskID)
	if !ok {
		s.downloads.Finish(taskID, DownloadStatusFailed, "unavailable")
		return ErrCatalogAccessRevoked
	}
	file, err := s.downloadableFile(transferCtx, ownerUserID, fileID)
	if err != nil {
		s.downloads.Finish(taskID, DownloadStatusFailed, downloadErrorCode(err))
		return err
	}
	nodeValue, ok := s.nodes.Get(file.NodeID)
	if !ok {
		s.downloads.Finish(taskID, DownloadStatusFailed, "node_unavailable")
		return ErrCatalogNodeMissing
	}
	if file.FileSize != nil {
		s.downloads.SetTotal(taskID, *file.FileSize)
	}
	err = s.proxy.StreamDownload(transferCtx, writer, s.newDownloader(nodeValue), ContentRequest{FilePath: file.FilePath, FileName: file.FileName, FileSize: file.FileSize, Mode: CapabilityModeDownload}, func(written uint64) { s.downloads.AddBytes(taskID, written) })
	if err != nil {
		if errors.Is(err, context.Canceled) {
			s.downloads.Finish(taskID, DownloadStatusCancelled, "cancelled")
		} else {
			s.downloads.Finish(taskID, DownloadStatusFailed, downloadErrorCode(err))
		}
		return err
	}
	s.downloads.Finish(taskID, DownloadStatusCompleted, "")
	return nil
}

func (s *CatalogService) downloadableFile(ctx context.Context, userID uint, fileID uint64) (*models.GbRecordingFile, error) {
	if s.checkPermission == nil || s.newDownloader == nil {
		return nil, ErrCatalogAccessUnavailable
	}
	allowed, err := s.checkPermission(ctx, userID, "/api/gb28181/cloud-recordings/files/:id/downloads", http.MethodPost)
	if err != nil || !allowed {
		return nil, ErrCatalogAccessRevoked
	}
	access, err := s.access(ctx, userID)
	if err != nil {
		return nil, ErrCatalogAccessRevoked
	}
	file, err := s.repo.GetCatalogFile(ctx, fileID, access.DeptIDs, access.FullAccess)
	if errors.Is(err, ErrRecordingFileNotFound) {
		return nil, ErrCatalogAccessRevoked
	}
	if err != nil {
		return nil, err
	}
	nodes := s.nodeSnapshot()
	if err := catalogAvailabilityError(CatalogAvailability(*file, nodes.known, nodes.offline, nodes.accessible)); err != nil {
		return nil, err
	}
	return file, nil
}

func downloadErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrCatalogAccessRevoked):
		return "access_revoked"
	case errors.Is(err, ErrCatalogFileMissing), errors.Is(err, ErrRecordingFileNotFound):
		return "file_missing"
	case errors.Is(err, ErrCatalogNodeMissing), errors.Is(err, ErrCatalogNodeOffline), errors.Is(err, zlm.ErrRecordingNodeUnavailable):
		return "node_unavailable"
	case errors.Is(err, ErrContentTimeout), errors.Is(err, zlm.ErrRecordingResponseTimeout), errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, ErrContentRangeInvalid):
		return "range_invalid"
	default:
		return "transfer_failed"
	}
}

func (s *CatalogService) ActiveSessions(ctx context.Context, userID uint) ([]ActiveSessionDTO, error) {
	access, err := s.access(ctx, userID)
	if err != nil {
		return nil, err
	}
	sessions, err := s.repo.ListActiveCatalogSessions(ctx, access.DeptIDs, access.FullAccess)
	if err != nil {
		return nil, err
	}
	nodes := s.nodeSnapshot()
	result := make([]ActiveSessionDTO, 0, len(sessions))
	for _, session := range sessions {
		result = append(result, ActiveSessionDTO{
			ID: strconv.FormatUint(session.ID, 10), ChannelID: strconv.FormatUint(uint64(session.ChannelID), 10),
			ChannelCode: session.ChannelCode, ChannelName: session.ChannelName, DeviceID: session.DeviceID,
			Node: nodes.dto(session.NodeID), State: session.State, StartedAt: cloneTime(session.StartedAt), UpdatedAt: session.UpdatedAt,
		})
	}
	return result, nil
}

func (s *CatalogService) Reconciliations(ctx context.Context) ([]ReconciliationDTO, error) {
	if s.repo == nil || s.nodes == nil {
		return nil, ErrCatalogAccessUnavailable
	}
	states, err := s.repo.ListCatalogReconcileStates(ctx)
	if err != nil {
		return nil, err
	}
	nodes := s.nodeSnapshot()
	result := make([]ReconciliationDTO, 0, len(states))
	for _, state := range states {
		result = append(result, ReconciliationDTO{
			Node: nodes.dto(state.NodeID), Status: state.Status, TriggerSource: state.TriggerSource,
			EffectiveStart: cloneTime(state.EffectiveStart), EffectiveEnd: cloneTime(state.EffectiveEnd),
			StartedAt: cloneTime(state.StartedAt), FinishedAt: cloneTime(state.FinishedAt),
			CandidateCount: state.CandidateCount, SuccessCount: state.SuccessCount, FailureCount: state.FailureCount,
			DiscoveredCount: state.DiscoveredCount, InsertedCount: state.InsertedCount, UpdatedCount: state.UpdatedCount,
			MissingCount: state.MissingCount, UnattributedCount: state.UnattributedCount, UpdatedAt: state.UpdatedAt,
		})
	}
	return result, nil
}

func (s *CatalogService) TriggerReconciliation(nodeIDs []int64, start, end *time.Time) ([]int64, error) {
	if s.scheduler == nil {
		return nil, ErrCatalogAccessUnavailable
	}
	return s.scheduler.Enqueue(ReconcileTriggerManual, nodeIDs, start, end)
}

func (s *CatalogService) ListFiles(ctx context.Context, userID uint, query FileQuery) (FileDTOPage, error) {
	access, err := s.access(ctx, userID)
	if err != nil {
		return FileDTOPage{}, err
	}
	query.AllowedDeptIDs, query.FullAccess = access.DeptIDs, access.FullAccess
	nodes := s.nodeSnapshot()
	query.KnownNodeIDs, query.OfflineNodeIDs, query.AccessibleNodeIDs = nodes.known, nodes.offline, nodes.accessible
	page, err := s.repo.ListCatalogFiles(ctx, query)
	if err != nil {
		return FileDTOPage{}, err
	}
	list := make([]FileDTO, 0, len(page.Files))
	for _, file := range page.Files {
		list = append(list, NewFileDTO(file, nodes.dto(file.NodeID), CatalogAvailability(file, nodes.known, nodes.offline, nodes.accessible)))
	}
	return FileDTOPage{List: list, Total: page.Total, Page: page.Page, PageSize: page.PageSize}, nil
}

func (s *CatalogService) FileOptions(ctx context.Context, userID uint, query FileQuery) (CatalogOptionsDTO, error) {
	access, err := s.access(ctx, userID)
	if err != nil {
		return CatalogOptionsDTO{}, err
	}
	query.AllowedDeptIDs, query.FullAccess = access.DeptIDs, access.FullAccess
	nodes := s.nodeSnapshot()
	query.KnownNodeIDs, query.OfflineNodeIDs, query.AccessibleNodeIDs = nodes.known, nodes.offline, nodes.accessible
	options, err := s.repo.CatalogOptions(ctx, query)
	if err != nil {
		return CatalogOptionsDTO{}, err
	}
	result := CatalogOptionsDTO{
		Channels: make([]CatalogChannelOptionDTO, 0),
		Devices:  make([]CatalogDeviceOptionDTO, 0),
		Nodes:    make([]NodeDTO, 0, len(options.NodeIDs)),
	}
	for _, nodeID := range options.NodeIDs {
		result.Nodes = append(result.Nodes, nodes.dto(nodeID))
	}
	return result, nil
}

func (s *CatalogService) FileDetail(ctx context.Context, userID uint, fileID uint64) (FileDTO, error) {
	access, err := s.access(ctx, userID)
	if err != nil {
		return FileDTO{}, err
	}
	file, err := s.repo.GetCatalogFile(ctx, fileID, access.DeptIDs, access.FullAccess)
	if err != nil {
		return FileDTO{}, err
	}
	nodes := s.nodeSnapshot()
	return NewFileDTO(*file, nodes.dto(file.NodeID), CatalogAvailability(*file, nodes.known, nodes.offline, nodes.accessible)), nil
}

func (s *CatalogService) IssueAccess(ctx context.Context, userID uint, fileID uint64, mode string) (AccessDTO, error) {
	if mode != CapabilityModePlay {
		return AccessDTO{}, ErrCapabilityInvalid
	}
	if s.signer == nil {
		return AccessDTO{}, ErrCatalogAccessUnavailable
	}
	access, err := s.access(ctx, userID)
	if err != nil {
		return AccessDTO{}, err
	}
	file, err := s.repo.GetCatalogFile(ctx, fileID, access.DeptIDs, access.FullAccess)
	if err != nil {
		return AccessDTO{}, err
	}
	nodes := s.nodeSnapshot()
	if err := catalogAvailabilityError(CatalogAvailability(*file, nodes.known, nodes.offline, nodes.accessible)); err != nil {
		return AccessDTO{}, err
	}
	grant, err := s.signer.Issue(strconv.FormatUint(file.ID, 10), userID, mode, file.TimeLen)
	if err != nil {
		return AccessDTO{}, err
	}
	return AccessDTO{Mode: mode, Capability: grant.Token, ExpiresAt: grant.ExpiresAt}, nil
}

func (s *CatalogService) StreamContent(ctx context.Context, writer http.ResponseWriter, fileID, capability, byteRange string) error {
	if s.signer == nil || s.checkPermission == nil || s.newDownloader == nil {
		return ErrCatalogAccessUnavailable
	}
	claims, err := s.signer.Verify(capability, fileID, CapabilityModePlay)
	if err != nil {
		return err
	}
	allowed, err := s.checkPermission(ctx, claims.UserID, fmt.Sprintf("/api/gb28181/cloud-recordings/files/%s/access", fileID), http.MethodPost)
	if err != nil || !allowed {
		return ErrCatalogAccessRevoked
	}
	access, err := s.access(ctx, claims.UserID)
	if err != nil {
		return ErrCatalogAccessRevoked
	}
	id, err := strconv.ParseUint(fileID, 10, 64)
	if err != nil || id == 0 {
		return ErrCapabilityInvalid
	}
	file, err := s.repo.GetCatalogFile(ctx, id, access.DeptIDs, access.FullAccess)
	if errors.Is(err, ErrRecordingFileNotFound) {
		return ErrCatalogAccessRevoked
	}
	if err != nil {
		return err
	}
	nodes := s.nodeSnapshot()
	if err := catalogAvailabilityError(CatalogAvailability(*file, nodes.known, nodes.offline, nodes.accessible)); err != nil {
		return err
	}
	n, ok := s.nodes.Get(file.NodeID)
	if !ok {
		return ErrCatalogNodeMissing
	}
	return s.proxy.Stream(ctx, writer, s.newDownloader(n), ContentRequest{
		FilePath: file.FilePath, FileName: file.FileName, FileSize: file.FileSize, Mode: claims.Mode, Range: byteRange,
	})
}

func (s *CatalogService) access(ctx context.Context, userID uint) (CatalogAccess, error) {
	if s.repo == nil || s.nodes == nil || s.resolveAccess == nil || userID == 0 {
		return CatalogAccess{}, ErrCatalogAccessRevoked
	}
	access, err := s.resolveAccess(ctx, userID)
	if err != nil {
		return CatalogAccess{}, err
	}
	return access, nil
}

type catalogNodeSnapshot struct {
	known      []int64
	offline    []int64
	accessible []int64
	byID       map[int64]*node.Node
}

func (s *CatalogService) nodeSnapshot() catalogNodeSnapshot {
	snapshot := catalogNodeSnapshot{byID: make(map[int64]*node.Node)}
	if s.nodes == nil {
		return snapshot
	}
	for _, n := range s.nodes.List() {
		if n == nil {
			continue
		}
		snapshot.byID[n.ID] = n
		snapshot.known = append(snapshot.known, n.ID)
		if n.State == node.StateOffline {
			snapshot.offline = append(snapshot.offline, n.ID)
		}
		if s.signer != nil && n.State != node.StateOffline && n.Host != "" && n.APIPort > 0 && n.APISecret != "" {
			snapshot.accessible = append(snapshot.accessible, n.ID)
		}
	}
	sort.Slice(snapshot.known, func(i, j int) bool { return snapshot.known[i] < snapshot.known[j] })
	sort.Slice(snapshot.offline, func(i, j int) bool { return snapshot.offline[i] < snapshot.offline[j] })
	sort.Slice(snapshot.accessible, func(i, j int) bool { return snapshot.accessible[i] < snapshot.accessible[j] })
	return snapshot
}

func (s catalogNodeSnapshot) dto(nodeID int64) NodeDTO {
	n, ok := s.byID[nodeID]
	if !ok {
		return NodeDTO{ID: strconv.FormatInt(nodeID, 10), State: AvailabilityNodeMissing}
	}
	return NodeDTO{ID: strconv.FormatInt(nodeID, 10), Name: n.Name, State: string(n.State)}
}

func catalogAvailabilityError(availability string) error {
	switch availability {
	case AvailabilityAvailable:
		return nil
	case AvailabilityNodeMissing:
		return ErrCatalogNodeMissing
	case AvailabilityNodeOffline:
		return ErrCatalogNodeOffline
	case AvailabilityFileMissing:
		return ErrCatalogFileMissing
	default:
		return ErrCatalogAccessUnavailable
	}
}
