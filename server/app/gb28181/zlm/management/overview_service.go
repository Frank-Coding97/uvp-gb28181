package management

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

const (
	// DefaultOverviewNodeTimeout bounds one active-node collection. It is kept
	// equal to the executor's default so a request cannot accidentally turn an
	// overview into an unbounded upstream fan-out.
	DefaultOverviewNodeTimeout = 3 * time.Second
	DefaultOverviewWorkers     = 4
	DefaultHeartbeatStaleAfter = 90 * time.Second
)

// RuntimeFreshness describes the freshness of one independently collected
// data source. HeartbeatFreshness is intentionally separate from runtime
// Freshness: registry heartbeat counters are not a substitute for a current
// RuntimeReader/ZLM collection.
type RuntimeFreshness string

const (
	RuntimeFreshnessFresh       RuntimeFreshness = "fresh"
	RuntimeFreshnessStale       RuntimeFreshness = "stale"
	RuntimeFreshnessUnavailable RuntimeFreshness = "unavailable"
	RuntimeFreshnessMaintenance RuntimeFreshness = "maintenance"
)

type RuntimeNodeStatus string

const (
	RuntimeNodeStatusFresh       RuntimeNodeStatus = "fresh"
	RuntimeNodeStatusPartial     RuntimeNodeStatus = "partial"
	RuntimeNodeStatusUnavailable RuntimeNodeStatus = "unavailable"
	RuntimeNodeStatusMaintenance RuntimeNodeStatus = "maintenance"
)

// OverviewNodeRegistry is deliberately narrower than node.Registry. The
// overview only needs immutable node snapshots and must not gain lifecycle
// mutation access.
type OverviewNodeRegistry interface {
	List() []*node.Node
}

// OverviewRuntimeReader is the T4 typed runtime surface needed by overview.
// RuntimeReader implements it; no generic API name or raw response is passed
// through this boundary.
type OverviewRuntimeReader interface {
	GetStatistic(context.Context, int64) (zlm.Statistic, error)
	GetAllSessions(context.Context, int64, zlm.SessionFilter) ([]zlm.Session, error)
	GetThreadsLoad(context.Context, int64) (float64, error)
	GetWorkThreadsLoad(context.Context, int64) (float64, error)
}

type overviewRuntimeThreadDetailReader interface {
	GetThreadsLoadDetail(context.Context, int64) ([]zlm.ThreadLoad, error)
}

type overviewMediaTrafficReader interface {
	GetMediaTrafficStatistic(context.Context, int64) (zlm.MediaTrafficStatistic, error)
}

// OverviewMediaListReader is the narrow adapter needed until T4 grows a
// typed media-list method. Its node ID argument keeps the collection bound to
// the exact node; implementations must return only typed MediaInfo values.
type OverviewMediaListReader interface {
	GetMediaList(context.Context, int64) ([]zlm.MediaInfo, error)
}

type OverviewDependencies struct {
	Registry OverviewNodeRegistry
	Runtime  OverviewRuntimeReader
	Media    OverviewMediaListReader
}

type OverviewOption func(*OverviewService)

func WithOverviewClock(clock func() time.Time) OverviewOption {
	return func(service *OverviewService) {
		if clock != nil {
			service.now = clock
		}
	}
}

// WithOverviewNodeTimeout accepts a shorter test or deployment deadline but
// never allows a per-node collection to exceed the contract's three seconds.
func WithOverviewNodeTimeout(timeout time.Duration) OverviewOption {
	return func(service *OverviewService) {
		if timeout <= 0 {
			return
		}
		if timeout > DefaultOverviewNodeTimeout {
			timeout = DefaultOverviewNodeTimeout
		}
		service.nodeTimeout = timeout
	}
}

func WithOverviewWorkerLimit(limit int) OverviewOption {
	return func(service *OverviewService) {
		if limit < 1 {
			return
		}
		if limit > DefaultOverviewWorkers {
			limit = DefaultOverviewWorkers
		}
		service.workers = limit
	}
}

func WithOverviewHeartbeatStaleAfter(after time.Duration) OverviewOption {
	return func(service *OverviewService) {
		if after > 0 {
			service.heartbeatStaleAfter = after
		}
	}
}

type OverviewService struct {
	registry            OverviewNodeRegistry
	runtime             OverviewRuntimeReader
	media               OverviewMediaListReader
	now                 func() time.Time
	nodeTimeout         time.Duration
	heartbeatStaleAfter time.Duration
	workers             int
}

func NewOverviewService(dependencies OverviewDependencies, options ...OverviewOption) *OverviewService {
	service := &OverviewService{
		registry:            dependencies.Registry,
		runtime:             dependencies.Runtime,
		media:               dependencies.Media,
		now:                 time.Now,
		nodeTimeout:         DefaultOverviewNodeTimeout,
		heartbeatStaleAfter: DefaultHeartbeatStaleAfter,
		workers:             DefaultOverviewWorkers,
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

type RuntimeMedia struct {
	NodeID           int64         `json:"nodeId"`
	Media            MediaIdentity `json:"media"`
	Online           bool          `json:"online"`
	AliveSecond      uint64        `json:"aliveSecond"`
	BytesSpeed       uint64        `json:"bytesSpeed"`
	ReaderCount      int           `json:"readerCount"`
	TotalReaderCount int           `json:"totalReaderCount"`
	OriginType       int           `json:"originType"`
	OriginTypeName   string        `json:"originTypeName,omitempty"`
	RecordingMP4     bool          `json:"recordingMp4"`
	RecordingHLS     bool          `json:"recordingHls"`
	TrackCount       int           `json:"trackCount"`
}

type NodeRuntimeMetrics struct {
	MediaSourceCount           uint64                  `json:"mediaSourceCount"`
	MultiMediaSourceMuxerCount uint64                  `json:"multiMediaSourceMuxerCount"`
	TCPServerCount             uint64                  `json:"tcpServerCount"`
	TCPSessionCount            uint64                  `json:"tcpSessionCount"`
	UDPServerCount             uint64                  `json:"udpServerCount"`
	UDPSessionCount            uint64                  `json:"udpSessionCount"`
	TCPClientCount             uint64                  `json:"tcpClientCount"`
	SocketCount                uint64                  `json:"socketCount"`
	NetworkSessionCount        int                     `json:"networkSessionCount"`
	NetThreadLoad              float64                 `json:"netThreadLoad"`
	WorkThreadLoad             float64                 `json:"workThreadLoad"`
	EventThreadLoads           []RuntimeThreadLoad     `json:"eventThreadLoads,omitempty"`
	ObjectStatistics           RuntimeObjectStatistics `json:"objectStatistics"`
	UpstreamBytesPerSecond     uint64                  `json:"upstreamBytesPerSecond"`
	DownstreamBytesPerSecond   uint64                  `json:"downstreamBytesPerSecond"`
	MediaTrafficAvailable      bool                    `json:"mediaTrafficAvailable"`
}

type RuntimeThreadLoad struct {
	Name    string `json:"name"`
	Load    int    `json:"load"`
	FDCount int    `json:"fdCount"`
}

type RuntimeObjectStatistics struct {
	MediaSource           uint64 `json:"mediaSource"`
	MultiMediaSourceMuxer uint64 `json:"multiMediaSourceMuxer"`
	TCPServer             uint64 `json:"tcpServer"`
	TCPSession            uint64 `json:"tcpSession"`
	UDPServer             uint64 `json:"udpServer"`
	UDPSession            uint64 `json:"udpSession"`
	TCPClient             uint64 `json:"tcpClient"`
	Socket                uint64 `json:"socket"`
	FrameImp              uint64 `json:"frameImp"`
	Frame                 uint64 `json:"frame"`
	Buffer                uint64 `json:"buffer"`
	BufferRaw             uint64 `json:"bufferRaw"`
	BufferLikeString      uint64 `json:"bufferLikeString"`
	BufferList            uint64 `json:"bufferList"`
	RtpPacket             uint64 `json:"rtpPacket"`
	RtmpPacket            uint64 `json:"rtmpPacket"`
}

type RuntimeNodeError struct {
	Stage     string              `json:"stage"`
	Code      ManagementErrorCode `json:"code"`
	Message   string              `json:"message"`
	Retryable bool                `json:"retryable"`
}

type NodeRuntimeView struct {
	NodeID             int64              `json:"nodeId"`
	Name               string             `json:"name"`
	State              node.State         `json:"state"`
	Status             RuntimeNodeStatus  `json:"status"`
	Freshness          RuntimeFreshness   `json:"freshness"`
	AsOf               time.Time          `json:"asOf"`
	HeartbeatAsOf      time.Time          `json:"heartbeatAsOf,omitempty"`
	HeartbeatFreshness RuntimeFreshness   `json:"heartbeatFreshness"`
	Metrics            NodeRuntimeMetrics `json:"metrics"`
	MetricsComplete    bool               `json:"metricsComplete"`
	MediaFreshness     RuntimeFreshness   `json:"mediaFreshness"`
	Streams            []RuntimeMedia     `json:"streams,omitempty"`
	Error              *RuntimeNodeError  `json:"error,omitempty"`
	Errors             []RuntimeNodeError `json:"errors,omitempty"`
}

type OverviewMetrics struct {
	SampledNodeCount           int64                   `json:"sampledNodeCount"`
	MediaSourceCount           uint64                  `json:"mediaSourceCount"`
	MultiMediaSourceMuxerCount uint64                  `json:"multiMediaSourceMuxerCount"`
	TCPServerCount             uint64                  `json:"tcpServerCount"`
	TCPSessionCount            uint64                  `json:"tcpSessionCount"`
	UDPServerCount             uint64                  `json:"udpServerCount"`
	UDPSessionCount            uint64                  `json:"udpSessionCount"`
	TCPClientCount             uint64                  `json:"tcpClientCount"`
	SocketCount                uint64                  `json:"socketCount"`
	NetworkSessionCount        int64                   `json:"networkSessionCount"`
	NetThreadLoadAvg           float64                 `json:"netThreadLoadAvg"`
	WorkThreadLoadAvg          float64                 `json:"workThreadLoadAvg"`
	StreamCount                int64                   `json:"streamCount"`
	ObjectStatistics           RuntimeObjectStatistics `json:"objectStatistics"`
	UpstreamBytesPerSecond     uint64                  `json:"upstreamBytesPerSecond"`
	DownstreamBytesPerSecond   uint64                  `json:"downstreamBytesPerSecond"`
	MediaTrafficSampledNodes   int64                   `json:"mediaTrafficSampledNodes"`
}

type NodeRuntimeFailure struct {
	NodeID int64            `json:"nodeId"`
	Error  RuntimeNodeError `json:"error"`
}

type OverviewResult struct {
	Nodes   []NodeRuntimeView `json:"nodes"`
	Streams []RuntimeMedia    `json:"streams"`
	Metrics OverviewMetrics   `json:"metrics"`
	// MediaRateSamples is the shared backend-owned five-minute history. It is
	// intentionally absent from raw OverviewService results and attached by
	// OverviewSampler at the HTTP boundary.
	MediaRateSamples []MediaRateSample `json:"mediaRateSamples"`
	Partial          bool              `json:"partial"`
	AsOf             time.Time         `json:"asOf"`
	// The two scopes are intentionally separate: runtime counters may be
	// current even when a media-list read fails, and callers must not infer
	// either scope from SuccessfulNodeIDs or FailedNodeIDs.
	MetricsSampledNodeIDs []int64              `json:"metricsSampledNodeIds"`
	MediaSampledNodeIDs   []int64              `json:"mediaSampledNodeIds"`
	SuccessfulNodeIDs     []int64              `json:"successfulNodeIds"`
	FailedNodeIDs         []int64              `json:"failedNodeIds"`
	Errors                []NodeRuntimeFailure `json:"errors,omitempty"`
}

type StreamFilter struct {
	NodeID int64  `json:"nodeId,omitempty"`
	Schema string `json:"schema,omitempty"`
	Vhost  string `json:"vhost,omitempty"`
	App    string `json:"app,omitempty"`
	Stream string `json:"stream,omitempty"`
}

type StreamDistribution struct {
	List              []RuntimeMedia       `json:"list"`
	Total             int64                `json:"total"`
	Page              int                  `json:"page"`
	PageSize          int                  `json:"pageSize"`
	Truncated         bool                 `json:"truncated"`
	Partial           bool                 `json:"partial"`
	AsOf              time.Time            `json:"asOf"`
	SuccessfulNodeIDs []int64              `json:"successfulNodeIds"`
	FailedNodeIDs     []int64              `json:"failedNodeIds"`
	Errors            []NodeRuntimeFailure `json:"errors,omitempty"`
}

type overviewStageError struct {
	stage string
	err   error
}

type overviewNodeCollection struct {
	index int
	node  *node.Node
}

type overviewMediaCollection struct {
	index int
	node  *node.Node
}

// GetOverview returns every registered node, including maintenance and
// offline nodes. Only active nodes enter the bounded ZLM collection pool.
func (s *OverviewService) GetOverview(ctx context.Context) (OverviewResult, error) {
	if s == nil || s.registry == nil {
		return OverviewResult{}, NewInternalError("", "overview service is not configured")
	}
	nodes := s.registry.List()
	asOf := s.clock()
	views := s.collectNodeViews(ctx, nodes)
	result := OverviewResult{
		Nodes:                 views,
		Streams:               make([]RuntimeMedia, 0),
		AsOf:                  asOf,
		MetricsSampledNodeIDs: make([]int64, 0),
		MediaSampledNodeIDs:   make([]int64, 0),
		SuccessfulNodeIDs:     make([]int64, 0),
		FailedNodeIDs:         make([]int64, 0),
		Errors:                make([]NodeRuntimeFailure, 0),
	}
	loadNodes := int64(0)
	for _, view := range views {
		if view.Error != nil {
			result.Errors = append(result.Errors, NodeRuntimeFailure{NodeID: view.NodeID, Error: *view.Error})
		}
		if view.State != node.StateActive {
			continue
		}
		if view.MetricsComplete {
			result.MetricsSampledNodeIDs = append(result.MetricsSampledNodeIDs, view.NodeID)
			addOverviewMetrics(&result.Metrics, view.Metrics)
			loadNodes++
		}
		if view.MediaFreshness == RuntimeFreshnessFresh {
			result.MediaSampledNodeIDs = append(result.MediaSampledNodeIDs, view.NodeID)
			result.Streams = append(result.Streams, view.Streams...)
		}
		if view.Status == RuntimeNodeStatusFresh {
			result.SuccessfulNodeIDs = append(result.SuccessfulNodeIDs, view.NodeID)
		} else {
			result.FailedNodeIDs = append(result.FailedNodeIDs, view.NodeID)
			result.Partial = true
		}
	}
	if loadNodes > 0 {
		result.Metrics.NetThreadLoadAvg /= float64(loadNodes)
		result.Metrics.WorkThreadLoadAvg /= float64(loadNodes)
	}
	sort.Slice(result.MetricsSampledNodeIDs, func(i, j int) bool {
		return result.MetricsSampledNodeIDs[i] < result.MetricsSampledNodeIDs[j]
	})
	sort.Slice(result.MediaSampledNodeIDs, func(i, j int) bool {
		return result.MediaSampledNodeIDs[i] < result.MediaSampledNodeIDs[j]
	})
	sort.Slice(result.SuccessfulNodeIDs, func(i, j int) bool { return result.SuccessfulNodeIDs[i] < result.SuccessfulNodeIDs[j] })
	sort.Slice(result.FailedNodeIDs, func(i, j int) bool { return result.FailedNodeIDs[i] < result.FailedNodeIDs[j] })
	sort.Slice(result.Errors, func(i, j int) bool { return result.Errors[i].NodeID < result.Errors[j].NodeID })
	sortRuntimeMedia(result.Streams)
	result.Metrics.StreamCount = int64(len(result.Streams))
	return result, nil
}

// Overview is a descriptive alias for callers that prefer the domain noun.
func (s *OverviewService) Overview(ctx context.Context) (OverviewResult, error) {
	return s.GetOverview(ctx)
}

func (s *OverviewService) GetNodeRuntime(ctx context.Context, nodeID int64) (NodeRuntimeView, error) {
	if s == nil || s.registry == nil {
		return NodeRuntimeView{}, NewInternalError(nodeIDString(nodeID), "overview service is not configured")
	}
	for _, current := range s.registry.List() {
		if current != nil && current.ID == nodeID {
			return s.collectNodeView(ctx, current), nil
		}
	}
	return NodeRuntimeView{}, NewNodeNotFoundError(nodeIDString(nodeID))
}

// Runtime is a descriptive alias for single-node callers.
func (s *OverviewService) Runtime(ctx context.Context, nodeID int64) (NodeRuntimeView, error) {
	return s.GetNodeRuntime(ctx, nodeID)
}

func (s *OverviewService) ListStreams(ctx context.Context, filter StreamFilter, request PageRequest) (StreamDistribution, error) {
	if err := filter.validate(); err != nil {
		return StreamDistribution{}, err
	}
	if s == nil || s.registry == nil {
		return StreamDistribution{}, NewInternalError("", "overview service is not configured")
	}
	nodes := s.registry.List()
	if filter.NodeID != 0 {
		nodes = filterNodesByID(nodes, filter.NodeID)
	}
	collections := s.collectMediaCollections(ctx, nodes)
	result := StreamDistribution{
		List:              make([]RuntimeMedia, 0),
		AsOf:              s.clock(),
		SuccessfulNodeIDs: make([]int64, 0),
		FailedNodeIDs:     make([]int64, 0),
		Errors:            make([]NodeRuntimeFailure, 0),
	}
	for _, collection := range collections {
		if collection.err != nil {
			result.Errors = append(result.Errors, NodeRuntimeFailure{NodeID: collection.nodeID, Error: collection.errValue})
		}
		if collection.state != node.StateActive {
			continue
		}
		if collection.freshness == RuntimeFreshnessFresh {
			result.SuccessfulNodeIDs = append(result.SuccessfulNodeIDs, collection.nodeID)
			for _, media := range collection.media {
				if filter.matches(media) {
					result.List = append(result.List, media)
				}
			}
		} else {
			result.FailedNodeIDs = append(result.FailedNodeIDs, collection.nodeID)
			result.Partial = true
		}
	}
	sort.Slice(result.SuccessfulNodeIDs, func(i, j int) bool { return result.SuccessfulNodeIDs[i] < result.SuccessfulNodeIDs[j] })
	sort.Slice(result.FailedNodeIDs, func(i, j int) bool { return result.FailedNodeIDs[i] < result.FailedNodeIDs[j] })
	sort.Slice(result.Errors, func(i, j int) bool { return result.Errors[i].NodeID < result.Errors[j].NodeID })
	sortRuntimeMedia(result.List)
	page := Paginate(result.List, request)
	result.List, result.Total, result.Page, result.PageSize, result.Truncated = page.List, page.Total, page.Page, page.PageSize, page.Truncated
	if int64(len(page.List)) < page.Total {
		result.Truncated = true
	}
	return result, nil
}

type collectedMediaNode struct {
	nodeID    int64
	state     node.State
	freshness RuntimeFreshness
	media     []RuntimeMedia
	err       error
	errValue  RuntimeNodeError
}

func (s *OverviewService) collectNodeViews(ctx context.Context, nodes []*node.Node) []NodeRuntimeView {
	if len(nodes) == 0 {
		return []NodeRuntimeView{}
	}
	views := make([]NodeRuntimeView, len(nodes))
	jobs := make(chan overviewNodeCollection, len(nodes))
	for index, current := range nodes {
		jobs <- overviewNodeCollection{index: index, node: current}
	}
	close(jobs)
	workers := s.workerCount(len(nodes))
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				views[job.index] = s.collectNodeView(ctx, job.node)
			}
		}()
	}
	wg.Wait()
	sort.SliceStable(views, func(i, j int) bool {
		if views[i].NodeID != views[j].NodeID {
			return views[i].NodeID < views[j].NodeID
		}
		return views[i].Name < views[j].Name
	})
	return views
}

func (s *OverviewService) collectMediaCollections(ctx context.Context, nodes []*node.Node) []collectedMediaNode {
	if len(nodes) == 0 {
		return []collectedMediaNode{}
	}
	collections := make([]collectedMediaNode, len(nodes))
	jobs := make(chan overviewMediaCollection, len(nodes))
	for index, current := range nodes {
		jobs <- overviewMediaCollection{index: index, node: current}
	}
	close(jobs)
	workers := s.workerCount(len(nodes))
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				collections[job.index] = s.collectMediaNode(ctx, job.node)
			}
		}()
	}
	wg.Wait()
	sort.SliceStable(collections, func(i, j int) bool { return collections[i].nodeID < collections[j].nodeID })
	return collections
}

func (s *OverviewService) clock() time.Time {
	if s == nil || s.now == nil {
		return time.Now()
	}
	return s.now()
}

func (s *OverviewService) workerCount(nodeCount int) int {
	if nodeCount < 1 {
		return 0
	}
	workers := DefaultOverviewWorkers
	if s != nil && s.workers > 0 {
		workers = s.workers
	}
	if workers > DefaultOverviewWorkers {
		workers = DefaultOverviewWorkers
	}
	if workers > nodeCount {
		workers = nodeCount
	}
	if workers < 1 {
		return 1
	}
	return workers
}

func (s *OverviewService) collectNodeView(ctx context.Context, current *node.Node) NodeRuntimeView {
	now := s.clock()
	view := NodeRuntimeView{
		AsOf:               now,
		Freshness:          RuntimeFreshnessUnavailable,
		HeartbeatFreshness: RuntimeFreshnessUnavailable,
		MediaFreshness:     RuntimeFreshnessUnavailable,
		Streams:            make([]RuntimeMedia, 0),
		Errors:             make([]RuntimeNodeError, 0),
	}
	if current == nil {
		view.Error = s.nodeError(0, "node", NewInternalError("", "node snapshot is unavailable"))
		view.Errors = append(view.Errors, *view.Error)
		return view
	}
	view.NodeID = current.ID
	view.Name = current.Name
	view.State = current.State
	view.HeartbeatAsOf = current.Stats.LastHeartbeatAt
	view.HeartbeatFreshness = heartbeatFreshness(now, current.Stats.LastHeartbeatAt, s.heartbeatStaleAfter)

	switch current.State {
	case node.StateMaintenance:
		view.Status = RuntimeNodeStatusMaintenance
		view.Freshness = RuntimeFreshnessMaintenance
		view.MediaFreshness = RuntimeFreshnessMaintenance
		view.Error = s.nodeError(current.ID, "node", NewNodeMaintenanceError(nodeIDString(current.ID)))
		view.Errors = append(view.Errors, *view.Error)
		return view
	case node.StateOffline:
		view.Status = RuntimeNodeStatusUnavailable
		view.Freshness = RuntimeFreshnessUnavailable
		view.MediaFreshness = RuntimeFreshnessUnavailable
		view.Error = s.nodeError(current.ID, "node", NewNodeOfflineError(nodeIDString(current.ID)))
		view.Errors = append(view.Errors, *view.Error)
		return view
	case node.StateActive:
		return s.collectActiveNodeView(ctx, current, view)
	default:
		view.Status = RuntimeNodeStatusUnavailable
		view.Error = s.nodeError(current.ID, "node", NewInternalError(nodeIDString(current.ID), "node state is unavailable"))
		view.Errors = append(view.Errors, *view.Error)
		return view
	}
}

func (s *OverviewService) collectActiveNodeView(ctx context.Context, current *node.Node, view NodeRuntimeView) NodeRuntimeView {
	operationCtx, cancel := context.WithTimeout(nonNilContext(ctx), s.effectiveNodeTimeout())
	defer cancel()

	var statistic zlm.Statistic
	var sessions []zlm.Session
	var eventThreadLoads []zlm.ThreadLoad
	var netLoad, workLoad float64
	var statisticErr, sessionsErr, netLoadErr, workLoadErr error
	var mediaTraffic zlm.MediaTrafficStatistic
	var mediaTrafficErr error
	var mediaInfos []zlm.MediaInfo
	var mediaErr error

	var wg sync.WaitGroup
	if s.runtime == nil {
		runtimeErr := NewUnsupportedCapabilityError(nodeIDString(current.ID), "runtime")
		statisticErr, sessionsErr, netLoadErr, workLoadErr = runtimeErr, runtimeErr, runtimeErr, runtimeErr
	} else {
		wg.Add(4)
		go func() {
			defer wg.Done()
			statistic, statisticErr = s.runtime.GetStatistic(operationCtx, current.ID)
		}()
		if trafficReader, ok := s.runtime.(overviewMediaTrafficReader); ok {
			wg.Add(1)
			go func() {
				defer wg.Done()
				mediaTraffic, mediaTrafficErr = trafficReader.GetMediaTrafficStatistic(operationCtx, current.ID)
			}()
		}
		go func() {
			defer wg.Done()
			sessions, sessionsErr = s.runtime.GetAllSessions(operationCtx, current.ID, zlm.SessionFilter{})
		}()
		go func() {
			defer wg.Done()
			if detailReader, ok := s.runtime.(overviewRuntimeThreadDetailReader); ok {
				eventThreadLoads, netLoadErr = detailReader.GetThreadsLoadDetail(operationCtx, current.ID)
				netLoad = zlm.AverageThreadLoad(eventThreadLoads)
				return
			}
			netLoad, netLoadErr = s.runtime.GetThreadsLoad(operationCtx, current.ID)
		}()
		go func() {
			defer wg.Done()
			workLoad, workLoadErr = s.runtime.GetWorkThreadsLoad(operationCtx, current.ID)
		}()
	}
	if s.media == nil {
		mediaErr = NewUnsupportedCapabilityError(nodeIDString(current.ID), "media-list")
	} else {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mediaInfos, mediaErr = s.media.GetMediaList(operationCtx, current.ID)
		}()
	}
	wg.Wait()

	if statisticErr == nil && sessionsErr == nil && netLoadErr == nil && workLoadErr == nil {
		view.Metrics = runtimeMetrics(statistic, len(sessions), netLoad, workLoad, eventThreadLoads)
		if mediaTrafficErr == nil {
			applyMediaTraffic(&view.Metrics, mediaTraffic)
		} else {
			view.Errors = append(view.Errors, *s.nodeError(current.ID, "media-traffic", mediaTrafficErr))
		}
		view.MetricsComplete = true
		view.Freshness = RuntimeFreshnessFresh
	} else {
		view.Freshness = RuntimeFreshnessUnavailable
		for _, item := range []overviewStageError{
			{stage: "statistic", err: statisticErr},
			{stage: "sessions", err: sessionsErr},
			{stage: "threads", err: netLoadErr},
			{stage: "workload", err: workLoadErr},
		} {
			if item.err != nil {
				view.Errors = append(view.Errors, *s.nodeError(current.ID, item.stage, item.err))
			}
		}
	}
	if mediaErr == nil {
		view.Streams, mediaErr = runtimeMediaList(current.ID, mediaInfos)
		if mediaErr == nil {
			view.MediaFreshness = RuntimeFreshnessFresh
		}
	}
	if mediaErr != nil {
		view.MediaFreshness = RuntimeFreshnessUnavailable
		view.Errors = append(view.Errors, *s.nodeError(current.ID, "media", mediaErr))
	}
	sortOverviewErrors(view.Errors)
	if len(view.Errors) > 0 {
		first := view.Errors[0]
		view.Error = &first
	}
	switch {
	case view.MetricsComplete && view.MediaFreshness == RuntimeFreshnessFresh && len(view.Errors) == 0:
		view.Status = RuntimeNodeStatusFresh
	case view.MetricsComplete:
		view.Status = RuntimeNodeStatusPartial
	default:
		view.Status = RuntimeNodeStatusUnavailable
	}
	return view
}

func (s *OverviewService) collectMediaNode(ctx context.Context, current *node.Node) collectedMediaNode {
	collection := collectedMediaNode{
		freshness: RuntimeFreshnessUnavailable,
		media:     make([]RuntimeMedia, 0),
	}
	if current == nil {
		collection.err = NewInternalError("", "node snapshot is unavailable")
		collection.errValue = *s.nodeError(0, "node", collection.err)
		return collection
	}
	collection.nodeID = current.ID
	collection.state = current.State
	switch current.State {
	case node.StateMaintenance:
		collection.freshness = RuntimeFreshnessMaintenance
		collection.err = NewNodeMaintenanceError(nodeIDString(current.ID))
		collection.errValue = *s.nodeError(current.ID, "node", collection.err)
		return collection
	case node.StateOffline:
		collection.err = NewNodeOfflineError(nodeIDString(current.ID))
		collection.errValue = *s.nodeError(current.ID, "node", collection.err)
		return collection
	case node.StateActive:
		// Continue to the typed adapter below.
	default:
		collection.err = NewInternalError(nodeIDString(current.ID), "node state is unavailable")
		collection.errValue = *s.nodeError(current.ID, "node", collection.err)
		return collection
	}
	if s.media == nil {
		collection.err = NewUnsupportedCapabilityError(nodeIDString(current.ID), "media-list")
		collection.errValue = *s.nodeError(current.ID, "media", collection.err)
		return collection
	}
	operationCtx, cancel := context.WithTimeout(nonNilContext(ctx), s.effectiveNodeTimeout())
	defer cancel()
	infos, err := s.media.GetMediaList(operationCtx, current.ID)
	if err != nil {
		collection.err = err
		collection.errValue = *s.nodeError(current.ID, "media", err)
		return collection
	}
	collection.media, err = runtimeMediaList(current.ID, infos)
	if err != nil {
		collection.err = err
		collection.errValue = *s.nodeError(current.ID, "media", err)
		return collection
	}
	collection.freshness = RuntimeFreshnessFresh
	return collection
}

func (s *OverviewService) effectiveNodeTimeout() time.Duration {
	if s == nil || s.nodeTimeout <= 0 {
		return DefaultOverviewNodeTimeout
	}
	if s.nodeTimeout > DefaultOverviewNodeTimeout {
		return DefaultOverviewNodeTimeout
	}
	return s.nodeTimeout
}

func (s *OverviewService) nodeError(nodeID int64, stage string, err error) *RuntimeNodeError {
	managementErr := NormalizeError(err, nodeIDString(nodeID))
	stable := RuntimeNodeError{
		Stage:   stage,
		Code:    CodeInternal,
		Message: "internal management error",
	}
	if normalized, ok := AsManagementError(managementErr); ok && normalized != nil {
		stable.Code = normalized.Code
		stable.Message = sanitizeErrorText(normalized.Message)
		stable.Retryable = normalized.Retryable
	}
	if stable.Message == "" {
		stable.Message = "internal management error"
	}
	return &stable
}

func heartbeatFreshness(now, heartbeat time.Time, staleAfter time.Duration) RuntimeFreshness {
	if heartbeat.IsZero() {
		return RuntimeFreshnessUnavailable
	}
	if staleAfter > 0 && now.Sub(heartbeat) > staleAfter {
		return RuntimeFreshnessStale
	}
	return RuntimeFreshnessFresh
}

func runtimeMetrics(statistic zlm.Statistic, sessionCount int, netLoad, workLoad float64, eventThreadLoads []zlm.ThreadLoad) NodeRuntimeMetrics {
	return NodeRuntimeMetrics{
		MediaSourceCount:           statistic.MediaSource,
		MultiMediaSourceMuxerCount: statistic.MultiMediaSourceMuxer,
		TCPServerCount:             statistic.TcpServer,
		TCPSessionCount:            statistic.TcpSession,
		UDPServerCount:             statistic.UdpServer,
		UDPSessionCount:            statistic.UdpSession,
		TCPClientCount:             statistic.TcpClient,
		SocketCount:                statistic.Socket,
		NetworkSessionCount:        sessionCount,
		NetThreadLoad:              netLoad,
		WorkThreadLoad:             workLoad,
		EventThreadLoads:           runtimeThreadLoads(eventThreadLoads),
		ObjectStatistics:           runtimeObjectStatistics(statistic),
	}
}

func applyMediaTraffic(metrics *NodeRuntimeMetrics, statistic zlm.MediaTrafficStatistic) {
	if metrics == nil {
		return
	}
	metrics.UpstreamBytesPerSecond = statistic.UpstreamBytesPerSecond
	metrics.DownstreamBytesPerSecond = statistic.DownstreamBytesPerSecond
	metrics.MediaTrafficAvailable = true
}

func runtimeThreadLoads(loads []zlm.ThreadLoad) []RuntimeThreadLoad {
	result := make([]RuntimeThreadLoad, 0, len(loads))
	for _, load := range loads {
		result = append(result, RuntimeThreadLoad{Name: load.Name, Load: load.Load, FDCount: load.FDCount})
	}
	return result
}

func runtimeObjectStatistics(statistic zlm.Statistic) RuntimeObjectStatistics {
	return RuntimeObjectStatistics{
		MediaSource: statistic.MediaSource, MultiMediaSourceMuxer: statistic.MultiMediaSourceMuxer,
		TCPServer: statistic.TcpServer, TCPSession: statistic.TcpSession,
		UDPServer: statistic.UdpServer, UDPSession: statistic.UdpSession,
		TCPClient: statistic.TcpClient, Socket: statistic.Socket,
		FrameImp: statistic.FrameImp, Frame: statistic.Frame,
		Buffer: statistic.Buffer, BufferRaw: statistic.BufferRaw,
		BufferLikeString: statistic.BufferLikeString, BufferList: statistic.BufferList,
		RtpPacket: statistic.RtpPacket, RtmpPacket: statistic.RtmpPacket,
	}
}

func runtimeMediaList(nodeID int64, infos []zlm.MediaInfo) ([]RuntimeMedia, error) {
	media := make([]RuntimeMedia, 0, len(infos))
	for _, info := range infos {
		identity := MediaIdentity{Schema: info.Schema, Vhost: info.VHost, App: info.App, Stream: info.Stream}
		if err := identity.Validate(); err != nil {
			return nil, NewInternalError(nodeIDString(nodeID), "media identity is invalid", err)
		}
		media = append(media, RuntimeMedia{
			NodeID:           nodeID,
			Media:            identity,
			Online:           true,
			AliveSecond:      info.AliveSecond,
			BytesSpeed:       info.BytesSpeed,
			ReaderCount:      info.ReaderCount,
			TotalReaderCount: info.TotalReaderCount,
			OriginType:       info.OriginType,
			OriginTypeName:   safeRuntimeLabel(info.OriginTypeStr),
			RecordingMP4:     info.IsRecordingMP4,
			RecordingHLS:     info.IsRecordingHLS,
			TrackCount:       len(info.Tracks),
		})
	}
	sortRuntimeMedia(media)
	return media, nil
}

func safeRuntimeLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = sanitizeErrorText(value)
	if len([]rune(value)) > 64 {
		value = string([]rune(value)[:64])
	}
	return value
}

func addOverviewMetrics(total *OverviewMetrics, current NodeRuntimeMetrics) {
	if total == nil {
		return
	}
	total.SampledNodeCount++
	total.MediaSourceCount += current.MediaSourceCount
	total.MultiMediaSourceMuxerCount += current.MultiMediaSourceMuxerCount
	total.TCPServerCount += current.TCPServerCount
	total.TCPSessionCount += current.TCPSessionCount
	total.UDPServerCount += current.UDPServerCount
	total.UDPSessionCount += current.UDPSessionCount
	total.TCPClientCount += current.TCPClientCount
	total.SocketCount += current.SocketCount
	total.NetworkSessionCount += int64(current.NetworkSessionCount)
	if current.MediaTrafficAvailable {
		total.UpstreamBytesPerSecond += current.UpstreamBytesPerSecond
		total.DownstreamBytesPerSecond += current.DownstreamBytesPerSecond
		total.MediaTrafficSampledNodes++
	}
	total.NetThreadLoadAvg += current.NetThreadLoad
	total.WorkThreadLoadAvg += current.WorkThreadLoad
	addRuntimeObjectStatistics(&total.ObjectStatistics, current.ObjectStatistics)
}

func addRuntimeObjectStatistics(total *RuntimeObjectStatistics, current RuntimeObjectStatistics) {
	if total == nil {
		return
	}
	total.MediaSource += current.MediaSource
	total.MultiMediaSourceMuxer += current.MultiMediaSourceMuxer
	total.TCPServer += current.TCPServer
	total.TCPSession += current.TCPSession
	total.UDPServer += current.UDPServer
	total.UDPSession += current.UDPSession
	total.TCPClient += current.TCPClient
	total.Socket += current.Socket
	total.FrameImp += current.FrameImp
	total.Frame += current.Frame
	total.Buffer += current.Buffer
	total.BufferRaw += current.BufferRaw
	total.BufferLikeString += current.BufferLikeString
	total.BufferList += current.BufferList
	total.RtpPacket += current.RtpPacket
	total.RtmpPacket += current.RtmpPacket
}

func sortRuntimeMedia(media []RuntimeMedia) {
	sort.SliceStable(media, func(i, j int) bool {
		left, right := media[i], media[j]
		if left.NodeID != right.NodeID {
			return left.NodeID < right.NodeID
		}
		for _, pair := range [][2]string{
			{left.Media.Schema, right.Media.Schema},
			{left.Media.Vhost, right.Media.Vhost},
			{left.Media.App, right.Media.App},
			{left.Media.Stream, right.Media.Stream},
		} {
			if pair[0] != pair[1] {
				return pair[0] < pair[1]
			}
		}
		return false
	})
}

func sortOverviewErrors(errorsList []RuntimeNodeError) {
	sort.SliceStable(errorsList, func(i, j int) bool {
		left, right := errorsList[i], errorsList[j]
		leftRank, rightRank := overviewStageRank(left.Stage), overviewStageRank(right.Stage)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return left.Stage < right.Stage
	})
}

func overviewStageRank(stage string) int {
	switch stage {
	case "node":
		return 0
	case "statistic":
		return 1
	case "sessions":
		return 2
	case "threads":
		return 3
	case "workload":
		return 4
	case "media":
		return 5
	default:
		return 6
	}
}

func filterNodesByID(nodes []*node.Node, nodeID int64) []*node.Node {
	filtered := make([]*node.Node, 0, 1)
	for _, current := range nodes {
		if current != nil && current.ID == nodeID {
			filtered = append(filtered, current)
		}
	}
	return filtered
}

func (filter StreamFilter) validate() error {
	fields := make(map[string]string)
	if filter.NodeID < 0 {
		fields["nodeId"] = "must be zero or positive"
	}
	validateOverviewFilterField(fields, "schema", filter.Schema, true)
	validateOverviewFilterField(fields, "vhost", filter.Vhost, false)
	validateOverviewFilterField(fields, "app", filter.App, false)
	validateOverviewFilterField(fields, "stream", filter.Stream, false)
	if len(fields) > 0 {
		return NewValidationError(fields)
	}
	return nil
}

func validateOverviewFilterField(fields map[string]string, name, value string, schema bool) {
	if value == "" {
		return
	}
	if !utf8.ValidString(value) {
		fields[name] = "must be valid UTF-8"
		return
	}
	if strings.TrimSpace(value) == "" {
		fields[name] = "must not be blank"
		return
	}
	if utf8.RuneCountInString(value) > MaxMediaIdentityFieldLength {
		fields[name] = "exceeds the maximum length"
		return
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			fields[name] = "must not contain control characters"
			return
		}
	}
	if schema && !validSchema(value) {
		fields[name] = "must be a valid media schema"
	}
}

func (filter StreamFilter) matches(media RuntimeMedia) bool {
	if filter.NodeID != 0 && filter.NodeID != media.NodeID {
		return false
	}
	if filter.Schema != "" && filter.Schema != media.Media.Schema {
		return false
	}
	if filter.Vhost != "" && filter.Vhost != media.Media.Vhost {
		return false
	}
	if filter.App != "" && filter.App != media.Media.App {
		return false
	}
	return filter.Stream == "" || filter.Stream == media.Media.Stream
}
