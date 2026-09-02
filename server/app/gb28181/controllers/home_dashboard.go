package controllers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	basecontrollers "uvplatform.cn/uvp-gb28181/app/controllers"
	"uvplatform.cn/uvp-gb28181/app/gb28181/dashboard"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/management"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/datascope"
)

const maxDashboardLayoutBody = 64 << 10
const homeDashboardSectionTimeout = 4 * time.Second

type HomeDashboardController struct {
	basecontrollers.Common
	db         func() *gorm.DB
	overviewMu sync.RWMutex
	overview   HomeDashboardOverview
	mediaRead  func(*gin.Context) bool
}

type HomeDashboardOverview interface {
	GetOverview(context.Context) (management.OverviewResult, error)
}

func NewHomeDashboardController(provider ...func() *gorm.DB) *HomeDashboardController {
	db := func() *gorm.DB { return app.DB() }
	if len(provider) > 0 && provider[0] != nil {
		db = provider[0]
	}
	return &HomeDashboardController{db: db, mediaRead: canReadMediaOverview}
}

func (controller *HomeDashboardController) SetOverview(provider HomeDashboardOverview) {
	controller.overviewMu.Lock()
	controller.overview = provider
	controller.overviewMu.Unlock()
}

func (controller *HomeDashboardController) Summary(c *gin.Context) {
	groups, nodeID, err := parseHomeSummaryQuery(c)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": 1, "message": err.Error()})
		return
	}
	now := time.Now()
	db := controller.db()
	if db == nil {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"code": 1, "message": "仪表盘聚合服务未初始化"})
		return
	}
	asOf := now.Format(time.RFC3339Nano)
	response := make(gin.H)
	var responseMu sync.Mutex
	var group sync.WaitGroup
	setResponse := func(name string, result any) {
		responseMu.Lock()
		response[name] = result
		responseMu.Unlock()
	}
	launch := func(name string, query func(context.Context) any) {
		group.Add(1)
		go func() {
			defer group.Done()
			ctx, cancel := context.WithTimeout(c.Request.Context(), homeDashboardSectionTimeout)
			defer cancel()
			result := query(ctx)
			setResponse(name, result)
		}()
	}

	scopeContext := c.Copy()
	assetService := dashboard.NewAssetSummaryService(db, time.Local)
	deviceScope := datascope.VisibilityScope(scopeContext, "gb_device.owner_dept_id", "gb_device.device_id")
	channelScope := datascope.VisibilityScope(scopeContext, "gb_channel.owner_dept_id", "gb_channel.device_id")
	trafficScope := datascope.VisibilityScope(scopeContext, "traffic.owner_dept_id", "traffic.device_code")
	playScope := datascope.VisibilityScope(scopeContext, "gb_device.owner_dept_id", "gb_device.device_id")

	if groups["assets"] {
		launch("assets", func(ctx context.Context) any {
			assets, queryErr := assetService.OnlineSummary(ctx, now, deviceScope, channelScope)
			return assetOnlineSection(asOf, assets, queryErr)
		})
	}
	if groups["aggregate"] {
		launch("sip", func(ctx context.Context) any {
			sip, queryErr := dashboard.NewSIPMetricQuery(db, time.Local).Summary(ctx, now)
			return homeSection(asOf, dashboard.Scope{Type: dashboard.ScopePlatform}, sip, queryErr)
		})
		launch("play", func(ctx context.Context) any {
			play, queryErr := dashboard.NewPlayAttemptStore(db).Last24HoursScoped(ctx, now, playScope)
			return homeSection(asOf, dashboard.Scope{Type: dashboard.ScopeUserVisible}, play, queryErr)
		})
		launch("traffic", func(ctx context.Context) any {
			traffic, queryErr := assetService.TrafficSummary(ctx, now, trafficScope)
			return homeSection(asOf, dashboard.Scope{Type: dashboard.ScopeUserVisible}, traffic, queryErr)
		})
	}

	if groups["realtime"] {
		mediaScope := dashboard.Scope{Type: dashboard.ScopePlatform}
		if nodeID > 0 {
			mediaScope = dashboard.Scope{Type: dashboard.ScopeNode, NodeID: nodeID}
		}
		if !controller.mediaRead(c) {
			setResponse("media", dashboard.SectionEnvelope[any]{Status: dashboard.StatusForbidden, AsOf: asOf, Scope: mediaScope, Coverage: dashboard.CoverageNotStarted})
		} else {
			controller.overviewMu.RLock()
			overview := controller.overview
			controller.overviewMu.RUnlock()
			if overview == nil {
				setResponse("media", dashboard.SectionEnvelope[any]{Status: dashboard.StatusDisabled, AsOf: asOf, Scope: mediaScope, Coverage: dashboard.CoverageNotStarted})
			} else {
				launch("media", func(ctx context.Context) any {
					raw, queryErr := overview.GetOverview(ctx)
					if queryErr != nil {
						return homeSection(asOf, mediaScope, dashboard.MediaDashboard{}, queryErr)
					}
					if nodeID > 0 {
						raw = overviewForNode(raw, nodeID)
					}
					media := dashboard.BuildMediaDashboard(raw)
					return mediaSection(asOf, mediaScope, media)
				})
			}
		}
	}
	group.Wait()
	controller.Success(c, response)
}

func parseHomeSummaryQuery(c *gin.Context) (map[string]bool, int64, error) {
	groups := map[string]bool{"realtime": true, "assets": true, "aggregate": true}
	if raw := strings.TrimSpace(c.Query("groups")); raw != "" {
		groups = make(map[string]bool)
		for _, value := range strings.Split(raw, ",") {
			value = strings.TrimSpace(value)
			if value != "realtime" && value != "assets" && value != "aggregate" {
				return nil, 0, fmt.Errorf("未知仪表盘分组 %q", value)
			}
			groups[value] = true
		}
	}
	var nodeID int64
	if raw := strings.TrimSpace(c.Query("nodeId")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 {
			return nil, 0, errors.New("nodeId 必须为正整数")
		}
		nodeID = parsed
	}
	return groups, nodeID, nil
}

func canReadMediaOverview(c *gin.Context) bool {
	userID := commonUserID(c)
	if userID == 0 {
		return false
	}
	if app.ConfigYml != nil {
		for _, skipped := range app.ConfigYml.GetUintSlice("server.notcheckuser") {
			if skipped == userID {
				return true
			}
		}
	}
	if app.CasbinV2 == nil {
		return false
	}
	allowed, err := app.CasbinV2.Enforce(fmt.Sprintf("user_%d", userID), "/api/gb28181/zlm/overview", http.MethodGet, "*")
	return err == nil && allowed
}

func commonUserID(c *gin.Context) uint {
	claims := basecontrollers.Common{}.GetClaims(c)
	if claims == nil {
		return 0
	}
	return claims.UserID
}

func overviewForNode(result management.OverviewResult, nodeID int64) management.OverviewResult {
	filtered := management.OverviewResult{AsOf: result.AsOf}
	for _, current := range result.Nodes {
		if current.NodeID != nodeID {
			continue
		}
		filtered.Nodes = append(filtered.Nodes, current)
		filtered.Metrics.NetworkSessionCount = int64(current.Metrics.NetworkSessionCount)
		filtered.Metrics.NetThreadLoadAvg = current.Metrics.NetThreadLoad
		filtered.Metrics.WorkThreadLoadAvg = current.Metrics.WorkThreadLoad
		filtered.Partial = current.Status == management.RuntimeNodeStatusPartial || current.Status == management.RuntimeNodeStatusUnavailable
		break
	}
	for _, stream := range result.Streams {
		if stream.NodeID == nodeID {
			filtered.Streams = append(filtered.Streams, stream)
		}
	}
	filtered.Metrics.StreamCount = int64(len(filtered.Streams))
	return filtered
}

func assetOnlineSection(asOf string, data dashboard.AssetOnlineSummary, err error) dashboard.SectionEnvelope[dashboard.AssetOnlineSummary] {
	section := homeSection(asOf, dashboard.Scope{Type: dashboard.ScopeUserVisible}, data, err)
	if err == nil && data.Devices.Total == 0 && data.Channels.Total == 0 {
		section.Status = dashboard.StatusEmpty
	}
	return section
}

func mediaSection(asOf string, scope dashboard.Scope, media dashboard.MediaDashboard) dashboard.SectionEnvelope[dashboard.MediaDashboard] {
	status := dashboard.StatusOK
	if media.Coverage == dashboard.CoveragePartial {
		status = dashboard.StatusPartial
	}
	if media.Coverage == dashboard.CoverageNotStarted {
		status = dashboard.StatusEmpty
	}
	return dashboard.SectionEnvelope[dashboard.MediaDashboard]{Status: status, AsOf: asOf, Scope: scope, Coverage: media.Coverage, Data: media}
}

func homeSection[T any](asOf string, scope dashboard.Scope, data T, err error) dashboard.SectionEnvelope[T] {
	if err != nil {
		return dashboard.SectionEnvelope[T]{Status: dashboard.StatusUnavailable, AsOf: asOf, Scope: scope, Coverage: dashboard.CoveragePartial, Data: data}
	}
	status, coverage := dashboard.StatusOK, dashboard.CoverageComplete
	switch value := any(data).(type) {
	case dashboard.SIPMetricSummary:
		status, coverage = value.Status, value.Coverage
	case dashboard.PlaySuccessSummary:
		status, coverage = value.Status, value.Coverage
	case dashboard.AssetSummary:
		status, coverage = value.Traffic.Status, value.Traffic.Coverage
	case dashboard.TrafficTodaySummary:
		status, coverage = value.Status, value.Coverage
	}
	return dashboard.SectionEnvelope[T]{Status: status, AsOf: asOf, Scope: scope, Coverage: coverage, Data: data}
}

func (controller *HomeDashboardController) GetLayout(c *gin.Context) {
	result, err := dashboard.NewLayoutService(controller.db()).Get(c.Request.Context(), controller.GetCurrentUserID(c))
	controller.writeLayoutResult(c, result, err)
}

func (controller *HomeDashboardController) SaveLayout(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxDashboardLayoutBody)
	var request struct {
		Revision uint64           `json:"revision"`
		Layout   dashboard.Layout `json:"layout"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		controller.Fail(c, "仪表盘布局参数不合法", err, http.StatusBadRequest)
		return
	}
	result, err := dashboard.NewLayoutService(controller.db()).Save(c.Request.Context(), controller.GetCurrentUserID(c), request.Revision, request.Layout)
	controller.writeLayoutResult(c, result, err)
}

func (controller *HomeDashboardController) ResetLayout(c *gin.Context) {
	result, err := dashboard.NewLayoutService(controller.db()).Reset(c.Request.Context(), controller.GetCurrentUserID(c))
	controller.writeLayoutResult(c, result, err)
}

func (controller *HomeDashboardController) writeLayoutResult(c *gin.Context, result dashboard.StoredLayout, err error) {
	if err == nil {
		controller.Success(c, result)
		return
	}
	if errors.Is(err, dashboard.ErrLayoutRevisionConflict) {
		controller.Fail(c, "仪表盘布局已被其他页面更新，请刷新后重试", err, http.StatusConflict, 1, gin.H{"errorCode": "LAYOUT_REVISION_CONFLICT"})
		return
	}
	controller.Fail(c, "仪表盘布局操作失败", err, http.StatusBadRequest)
}
