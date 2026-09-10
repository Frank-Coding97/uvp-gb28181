package controllers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
	"uvplatform.cn/uvp-gb28181/app/utils/response"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/traffic"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/common"
	"uvplatform.cn/uvp-gb28181/app/utils/datascope"
)

type TrafficLocationLookup interface {
	Lookup(string) (int64, bool)
}

type trafficNodeRegistry interface {
	Get(int64) (*node.Node, bool)
}

type trafficNodeClient interface {
	GetMediaList(context.Context, string, string, string) ([]zlm.MediaInfo, error)
	GetMediaPlayerList(context.Context, string, string, string, string) ([]zlm.MediaPlayer, error)
	KickSession(context.Context, string) error
}

type DeviceTrafficController struct {
	controllers.Common
	db        *gorm.DB
	realtime  *traffic.RealtimeStore
	nodes     trafficNodeRegistry
	locations TrafficLocationLookup
	clientFor func(*node.Node) trafficNodeClient
}

func NewDeviceTrafficController(db *gorm.DB, realtime *traffic.RealtimeStore, nodes trafficNodeRegistry, locations TrafficLocationLookup) *DeviceTrafficController {
	return &DeviceTrafficController{
		db: db, realtime: realtime, nodes: nodes, locations: locations,
		clientFor: func(mediaNode *node.Node) trafficNodeClient { return zlm.NewClientForNode(mediaNode) },
	}
}

type trafficScope struct {
	DeviceCode  string
	ChannelCode string
	Channel     *gbmodels.GbChannel
}

var trafficAccountingLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

func (dc *DeviceTrafficController) scope(c *gin.Context, requireChannel bool) (trafficScope, bool) {
	if dc == nil || dc.db == nil {
		dc.FailAndAbort(c, "流量统计服务尚未装配", nil)
		return trafficScope{}, false
	}
	deviceCode := strings.TrimSpace(c.Query("deviceId"))
	channelCode := strings.TrimSpace(c.Query("channelId"))
	if deviceCode == "" {
		dc.FailAndAbort(c, "deviceId 不能为空", nil)
		return trafficScope{}, false
	}
	var device gbmodels.GbDevice
	result := dc.db.WithContext(c.Request.Context()).Scopes(datascope.VisibilityScope(c, "owner_dept_id", "device_id")).
		Where("device_id = ?", deviceCode).Limit(1).Find(&device)
	if result.Error != nil || result.RowsAffected == 0 {
		dc.FailAndAbort(c, "设备不存在", result.Error)
		return trafficScope{}, false
	}
	scope := trafficScope{DeviceCode: device.DeviceID, ChannelCode: channelCode}
	if channelCode == "" {
		if requireChannel {
			dc.FailAndAbort(c, "channelId 不能为空", nil)
			return trafficScope{}, false
		}
		return scope, true
	}
	var channel gbmodels.GbChannel
	result = dc.db.WithContext(c.Request.Context()).Scopes(datascope.VisibilityScope(c, "owner_dept_id", "device_id")).
		Where("device_id = ? AND channel_id = ?", device.DeviceID, channelCode).Limit(1).Find(&channel)
	if result.Error != nil || result.RowsAffected == 0 {
		dc.FailAndAbort(c, "通道不存在", result.Error)
		return trafficScope{}, false
	}
	scope.Channel = &channel
	return scope, true
}

func trafficRange(c *gin.Context) (time.Time, time.Time, error) {
	now := time.Now().UTC()
	to := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	from := to.AddDate(0, 0, -6)
	var err error
	if raw := strings.TrimSpace(c.Query("from")); raw != "" {
		from, err = time.Parse("2006-01-02", raw)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	if raw := strings.TrimSpace(c.Query("to")); raw != "" {
		to, err = time.Parse("2006-01-02", raw)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	if to.Before(from) || to.Sub(from) > 366*24*time.Hour {
		return time.Time{}, time.Time{}, errors.New("日期范围不合法或超过 366 天")
	}
	return from.UTC(), to.UTC(), nil
}

func (dc *DeviceTrafficController) dailyQuery(c *gin.Context, scope trafficScope, from, to time.Time) *gorm.DB {
	query := dc.db.WithContext(c.Request.Context()).Model(&gbmodels.GbDeviceTrafficDaily{}).
		Where("device_code = ? AND stat_date >= ? AND stat_date <= ?", scope.DeviceCode, from, to)
	if scope.ChannelCode != "" {
		query = query.Where("channel_code = ?", scope.ChannelCode)
	}
	return query
}

func trafficHourlyRange(now time.Time) (time.Time, time.Time) {
	to := now.In(trafficAccountingLocation).Truncate(time.Hour)
	return to.Add(-23 * time.Hour), to
}

func (dc *DeviceTrafficController) hourlyQuery(c *gin.Context, scope trafficScope, from, to time.Time) *gorm.DB {
	query := dc.db.WithContext(c.Request.Context()).Model(&gbmodels.GbDeviceTrafficHourly{}).
		Where("device_code = ? AND stat_hour >= ? AND stat_hour <= ?", scope.DeviceCode, from, to)
	if scope.ChannelCode != "" {
		query = query.Where("channel_code = ?", scope.ChannelCode)
	}
	return query
}

type trafficTotal struct {
	UpstreamBytes, DownstreamBytes                     uint64
	UpstreamDurationSeconds, DownstreamDurationSeconds int64
	UpstreamSessions, DownstreamSessions               int64
}

func trafficTotalSelect() string {
	return "COALESCE(SUM(upstream_bytes),0) AS upstream_bytes, COALESCE(SUM(downstream_bytes),0) AS downstream_bytes, " +
		"COALESCE(SUM(upstream_duration_seconds),0) AS upstream_duration_seconds, COALESCE(SUM(downstream_duration_seconds),0) AS downstream_duration_seconds, " +
		"COALESCE(SUM(upstream_sessions),0) AS upstream_sessions, COALESCE(SUM(downstream_sessions),0) AS downstream_sessions"
}

func trafficSummaryPayload(total trafficTotal, from, to, timezone, granularity string) gin.H {
	return gin.H{
		"upstreamBytes": total.UpstreamBytes, "downstreamBytes": total.DownstreamBytes,
		"totalBytes":              total.UpstreamBytes + total.DownstreamBytes,
		"upstreamDurationSeconds": total.UpstreamDurationSeconds, "downstreamDurationSeconds": total.DownstreamDurationSeconds,
		"upstreamSessions": total.UpstreamSessions, "downstreamSessions": total.DownstreamSessions,
		"from": from, "to": to, "timezone": timezone, "granularity": granularity,
	}
}

func (dc *DeviceTrafficController) Summary(c *gin.Context) {
	scope, ok := dc.scope(c, false)
	if !ok {
		return
	}
	if c.Query("granularity") == "hour" {
		from, to := trafficHourlyRange(time.Now())
		var total trafficTotal
		if err := dc.hourlyQuery(c, scope, from, to).Select(trafficTotalSelect()).Scan(&total).Error; err != nil {
			dc.FailAndAbort(c, "查询小时流量汇总失败", err)
			return
		}
		dc.Success(c, trafficSummaryPayload(total, from.Format(time.RFC3339), to.Add(time.Hour).Format(time.RFC3339), "Asia/Shanghai", "hour"))
		return
	}
	from, to, err := trafficRange(c)
	if err != nil {
		dc.FailAndAbort(c, "日期范围不合法", err)
		return
	}
	var total trafficTotal
	err = dc.dailyQuery(c, scope, from, to).Select(trafficTotalSelect()).Scan(&total).Error
	if err != nil {
		dc.FailAndAbort(c, "查询流量汇总失败", err)
		return
	}
	dc.Success(c, trafficSummaryPayload(total, from.Format("2006-01-02"), to.Format("2006-01-02"), "Asia/Shanghai", "day"))
}

type trafficTrendRow struct {
	StatDate        time.Time `json:"-"`
	UpstreamBytes   uint64    `json:"upstreamBytes"`
	DownstreamBytes uint64    `json:"downstreamBytes"`
}

type trafficHourlyTrendRow struct {
	StatHour        time.Time
	UpstreamBytes   uint64
	DownstreamBytes uint64
}

func (dc *DeviceTrafficController) Trend(c *gin.Context) {
	scope, ok := dc.scope(c, false)
	if !ok {
		return
	}
	if c.Query("granularity") == "hour" {
		from, to := trafficHourlyRange(time.Now())
		var rows []trafficHourlyTrendRow
		err := dc.hourlyQuery(c, scope, from, to).Select(
			"stat_hour, COALESCE(SUM(upstream_bytes),0) AS upstream_bytes, COALESCE(SUM(downstream_bytes),0) AS downstream_bytes").
			Group("stat_hour").Order("stat_hour").Scan(&rows).Error
		if err != nil {
			dc.FailAndAbort(c, "查询小时流量趋势失败", err)
			return
		}
		rowByHour := make(map[string]trafficHourlyTrendRow, len(rows))
		for _, row := range rows {
			rowByHour[row.StatHour.In(trafficAccountingLocation).Format("2006-01-02T15")] = row
		}
		list := make([]gin.H, 0, 24)
		for hour := from; !hour.After(to); hour = hour.Add(time.Hour) {
			row := rowByHour[hour.Format("2006-01-02T15")]
			list = append(list, gin.H{
				"bucket": hour.Format(time.RFC3339), "date": hour.Format(time.RFC3339),
				"upstreamBytes": row.UpstreamBytes, "downstreamBytes": row.DownstreamBytes,
				"totalBytes": row.UpstreamBytes + row.DownstreamBytes,
			})
		}
		dc.Success(c, gin.H{"list": list, "from": from.Format(time.RFC3339), "to": to.Add(time.Hour).Format(time.RFC3339), "granularity": "hour", "timezone": "Asia/Shanghai"})
		return
	}
	from, to, err := trafficRange(c)
	if err != nil {
		dc.FailAndAbort(c, "日期范围不合法", err)
		return
	}
	var rows []trafficTrendRow
	err = dc.dailyQuery(c, scope, from, to).Select(
		"stat_date, COALESCE(SUM(upstream_bytes),0) AS upstream_bytes, COALESCE(SUM(downstream_bytes),0) AS downstream_bytes").
		Group("stat_date").Order("stat_date").Scan(&rows).Error
	if err != nil {
		dc.FailAndAbort(c, "查询流量趋势失败", err)
		return
	}
	list := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		list = append(list, gin.H{"bucket": row.StatDate.Format("2006-01-02"), "date": row.StatDate.Format("2006-01-02"), "upstreamBytes": row.UpstreamBytes,
			"downstreamBytes": row.DownstreamBytes, "totalBytes": row.UpstreamBytes + row.DownstreamBytes})
	}
	dc.Success(c, gin.H{"list": list, "from": from.Format("2006-01-02"), "to": to.Format("2006-01-02"), "granularity": "day", "timezone": "Asia/Shanghai"})
}

func (dc *DeviceTrafficController) Realtime(c *gin.Context) {
	scope, ok := dc.scope(c, false)
	if !ok {
		return
	}
	items := []traffic.RealtimeSnapshot{}
	if dc.realtime != nil {
		items = dc.realtime.List(scope.DeviceCode, scope.ChannelCode, time.Now().UTC())
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ChannelCode < items[j].ChannelCode })
	dc.Success(c, gin.H{"list": items, "estimated": true})
}

func (dc *DeviceTrafficController) Sessions(c *gin.Context) {
	scope, ok := dc.scope(c, false)
	if !ok {
		return
	}
	page := parsePositiveInt(c.DefaultQuery("page", "1"), 1)
	pageSize := parsePositiveInt(c.DefaultQuery("pageSize", "20"), 20)
	if pageSize > 100 {
		pageSize = 100
	}
	query := dc.db.WithContext(c.Request.Context()).Model(&gbmodels.GbDeviceTrafficSession{}).Where("device_code = ?", scope.DeviceCode)
	if scope.ChannelCode != "" {
		query = query.Where("channel_code = ?", scope.ChannelCode)
	}
	if rawFrom, rawTo := strings.TrimSpace(c.Query("from")), strings.TrimSpace(c.Query("to")); rawFrom != "" || rawTo != "" {
		from, fromErr := time.Parse(time.RFC3339, rawFrom)
		to, toErr := time.Parse(time.RFC3339, rawTo)
		if fromErr != nil || toErr != nil || !to.After(from) {
			dc.FailAndAbort(c, "明细时间范围不合法", errors.Join(fromErr, toErr))
			return
		}
		query = query.Where(
			"COALESCE(started_at, created_at) < ? AND COALESCE(ended_at, last_seen_at, started_at, created_at) >= ?",
			to.UTC(), from.UTC(),
		)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		dc.FailAndAbort(c, "统计流量会话失败", err)
		return
	}
	var list []gbmodels.GbDeviceTrafficSession
	if err := query.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		dc.FailAndAbort(c, "查询流量会话失败", err)
		return
	}
	dc.Success(c, gin.H{"list": list, "total": total, "page": page, "pageSize": pageSize})
}

func (dc *DeviceTrafficController) Coverage(c *gin.Context) {
	_, ok := dc.scope(c, false)
	if !ok {
		return
	}
	from, to, err := trafficRange(c)
	if err != nil {
		dc.FailAndAbort(c, "日期范围不合法", err)
		return
	}
	toExclusive := to.Add(24 * time.Hour)
	var gaps []gbmodels.GbDeviceTrafficGap
	if err := dc.db.WithContext(c.Request.Context()).Where("started_at < ? AND (ended_at IS NULL OR ended_at >= ?)", toExclusive, from).Order("started_at").Find(&gaps).Error; err != nil {
		dc.FailAndAbort(c, "查询采集覆盖范围失败", err)
		return
	}
	var startedAt *time.Time
	var first gbmodels.GbDeviceTrafficSession
	if result := dc.db.WithContext(c.Request.Context()).Order("created_at").Limit(1).Find(&first); result.Error == nil && result.RowsAffected > 0 {
		startedAt = &first.CreatedAt
	}
	coverage := "complete"
	if startedAt == nil {
		coverage = "not_started"
	} else if len(gaps) > 0 {
		coverage = "partial"
	}
	dc.Success(c, gin.H{"coverage": coverage, "statisticsStartedAt": startedAt, "gaps": gaps})
}

type currentViewer struct {
	ChannelID string `json:"channelId"`
	Schema    string `json:"schema"`
	Remote    string `json:"remote"`
	LocalPort int    `json:"localPort"`
	ID        string `json:"id"`
	Type      string `json:"type"`
	Kickable  bool   `json:"kickable"`
}

type currentViewerStream struct {
	ChannelID   string          `json:"channelId"`
	ChannelName string          `json:"channelName"`
	StartedAt   *time.Time      `json:"startedAt"`
	AliveSecond uint64          `json:"aliveSecond"`
	BitrateKbps float64         `json:"bitrateKbps"`
	TotalBytes  uint64          `json:"totalBytes"`
	ViewerCount int             `json:"viewerCount"`
	Status      string          `json:"status"`
	Viewers     []currentViewer `json:"viewers"`
}

type viewerChannel struct {
	ID   string
	Name string
}

func (dc *DeviceTrafficController) channelClient(c *gin.Context, scope trafficScope) (trafficNodeClient, *node.Node, bool) {
	if scope.Channel == nil || scope.Channel.StreamID == "" || dc.nodes == nil || dc.locations == nil {
		return nil, nil, false
	}
	nodeID, ok := dc.locations.Lookup(scope.Channel.StreamID)
	if !ok {
		return nil, nil, false
	}
	mediaNode, ok := dc.nodes.Get(nodeID)
	if !ok || mediaNode == nil {
		return nil, nil, false
	}
	return dc.clientFor(mediaNode), mediaNode, true
}

func viewerChannelName(channel gbmodels.GbChannel) string {
	if name := strings.TrimSpace(channel.Alias); name != "" {
		return name
	}
	if name := strings.TrimSpace(channel.Name); name != "" {
		return name
	}
	return channel.ChannelID
}

func (dc *DeviceTrafficController) listViewerStreams(c *gin.Context, scope trafficScope) ([]currentViewerStream, error) {
	channels := make([]gbmodels.GbChannel, 0)
	if scope.Channel != nil {
		channels = append(channels, *scope.Channel)
	} else if err := dc.db.WithContext(c.Request.Context()).Scopes(datascope.VisibilityScope(c, "owner_dept_id", "device_id")).
		Where("device_id = ? AND stream_id <> ''", scope.DeviceCode).Order("channel_id").Find(&channels).Error; err != nil {
		return nil, err
	}
	if dc.locations == nil || dc.nodes == nil {
		return []currentViewerStream{}, nil
	}
	type viewerNodeGroup struct {
		client          trafficNodeClient
		channelByStream map[string]viewerChannel
	}
	groups := make(map[int64]*viewerNodeGroup)
	for i := range channels {
		if channels[i].StreamID == "" {
			continue
		}
		nodeID, ok := dc.locations.Lookup(channels[i].StreamID)
		if !ok {
			continue
		}
		group := groups[nodeID]
		if group == nil {
			mediaNode, exists := dc.nodes.Get(nodeID)
			if !exists || mediaNode == nil {
				continue
			}
			group = &viewerNodeGroup{client: dc.clientFor(mediaNode), channelByStream: make(map[string]viewerChannel)}
			groups[nodeID] = group
		}
		group.channelByStream[channels[i].StreamID] = viewerChannel{ID: channels[i].ChannelID, Name: viewerChannelName(channels[i])}
	}
	nodeIDs := make([]int64, 0, len(groups))
	for nodeID := range groups {
		nodeIDs = append(nodeIDs, nodeID)
	}
	sort.Slice(nodeIDs, func(i, j int) bool { return nodeIDs[i] < nodeIDs[j] })
	result := make([]currentViewerStream, 0)
	for _, nodeID := range nodeIDs {
		group := groups[nodeID]
		streamFilter := ""
		if scope.Channel != nil {
			streamFilter = scope.Channel.StreamID
		}
		media, err := group.client.GetMediaList(c.Request.Context(), "__defaultVhost__", "rtp", streamFilter)
		if err != nil {
			return nil, err
		}
		streams, err := currentViewerStreamsFromMedia(c.Request.Context(), group.client, group.channelByStream, media)
		if err != nil {
			return nil, err
		}
		result = append(result, streams...)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ChannelID < result[j].ChannelID })
	return result, nil
}

func currentViewerStreamsFromMedia(ctx context.Context, client trafficNodeClient, channelByStream map[string]viewerChannel, media []zlm.MediaInfo) ([]currentViewerStream, error) {
	streamByChannel := make(map[string]*currentViewerStream)
	seenViewer := make(map[string]struct{})
	for _, item := range media {
		channel, belongsToDevice := channelByStream[item.Stream]
		if !belongsToDevice {
			continue
		}
		if item.ReaderCount <= 0 || item.Schema == "" {
			continue
		}
		stream := streamByChannel[channel.ID]
		if stream == nil {
			stream = &currentViewerStream{
				ChannelID: channel.ID, ChannelName: channel.Name, Status: "streaming", Viewers: make([]currentViewer, 0),
			}
			streamByChannel[channel.ID] = stream
		}
		stream.ViewerCount += item.ReaderCount
		if item.AliveSecond > stream.AliveSecond {
			stream.AliveSecond = item.AliveSecond
		}
		if bitrate := float64(item.BytesSpeed) * 8 / 1000; bitrate > stream.BitrateKbps {
			stream.BitrateKbps = bitrate
		}
		if item.TotalBytes > stream.TotalBytes {
			stream.TotalBytes = item.TotalBytes
		}
		if item.CreateStamp > 0 {
			startedAt := time.Unix(int64(item.CreateStamp), 0).UTC()
			if stream.StartedAt == nil || startedAt.Before(*stream.StartedAt) {
				stream.StartedAt = &startedAt
			}
		}
		players, err := client.GetMediaPlayerList(ctx, item.Schema, item.VHost, item.App, item.Stream)
		if err != nil {
			return nil, err
		}
		for _, player := range players {
			key := channel.ID + "\x00" + item.Schema + "\x00" + player.Identifier
			if _, exists := seenViewer[key]; exists {
				continue
			}
			seenViewer[key] = struct{}{}
			typeID := strings.ToLower(player.TypeID)
			stream.Viewers = append(stream.Viewers, currentViewer{
				ChannelID: channel.ID, Schema: item.Schema,
				Remote: fmt.Sprintf("%s:%d", player.PeerIP, player.PeerPort), LocalPort: player.LocalPort,
				ID: player.Identifier, Type: player.TypeID,
				Kickable: player.Identifier != "" && typeID != "" && !strings.Contains(typeID, "udp"),
			})
		}
	}
	result := make([]currentViewerStream, 0, len(streamByChannel))
	for _, stream := range streamByChannel {
		sort.Slice(stream.Viewers, func(i, j int) bool { return stream.Viewers[i].Remote < stream.Viewers[j].Remote })
		result = append(result, *stream)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ChannelID < result[j].ChannelID })
	return result, nil
}

func (dc *DeviceTrafficController) listViewers(c *gin.Context, scope trafficScope) ([]currentViewer, error) {
	streams, err := dc.listViewerStreams(c, scope)
	if err != nil {
		return nil, err
	}
	result := make([]currentViewer, 0)
	for i := range streams {
		result = append(result, streams[i].Viewers...)
	}
	return result, nil
}

func (dc *DeviceTrafficController) Viewers(c *gin.Context) {
	scope, ok := dc.scope(c, false)
	if !ok {
		return
	}
	list, err := dc.listViewerStreams(c, scope)
	if err != nil {
		dc.FailAndAbort(c, "查询当前观看连接失败", err)
		return
	}
	totalViewers := 0
	for i := range list {
		totalViewers += list[i].ViewerCount
	}
	dc.Success(c, gin.H{"list": list, "total": len(list), "totalViewers": totalViewers, "canKick": trafficSuperAdmin(c)})
}

func (dc *DeviceTrafficController) KickViewer(c *gin.Context) {
	if !trafficSuperAdmin(c) {
		response.SetBusinessResult(c, http.StatusForbidden, false)
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "msg": "仅超级管理员可强退观看连接"})
		return
	}
	scope, ok := dc.scope(c, true)
	if !ok {
		return
	}
	var body struct {
		ID     string `json:"id"`
		Schema string `json:"schema"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.ID) == "" || strings.TrimSpace(body.Schema) == "" {
		dc.FailAndAbort(c, "连接参数不合法", err)
		return
	}
	viewers, err := dc.listViewers(c, scope)
	if err != nil {
		dc.FailAndAbort(c, "重新确认观看连接失败", err)
		return
	}
	allowed := false
	for _, viewer := range viewers {
		if viewer.ID == body.ID && viewer.Schema == body.Schema && viewer.Kickable {
			allowed = true
			break
		}
	}
	if !allowed {
		response.SetBusinessResult(c, http.StatusConflict, false)
		c.AbortWithStatusJSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "msg": "连接已断开或当前协议不支持单连接强退"})
		return
	}
	client, mediaNode, available := dc.channelClient(c, scope)
	if !available {
		dc.FailAndAbort(c, "媒体节点不可用", nil)
		return
	}
	if err := client.KickSession(c.Request.Context(), body.ID); err != nil {
		dc.FailAndAbort(c, "强退观看连接失败", err)
		return
	}
	app.Log(c.Request.Context()).Info("GB28181 当前观看连接已强退", zap.String("event", "device_traffic.kickviewer.info"), zap.Uint("operatorId", common.GetCurrentUserID(c)),
		zap.String("deviceId", scope.DeviceCode), zap.String("channelId", scope.ChannelCode), zap.Int64("nodeId", mediaNode.ID))
	dc.Success(c, gin.H{"kicked": true})
}

func trafficSuperAdmin(c *gin.Context) bool {
	return app.ConfigYml != nil && common.IsSkipAuthUser(common.GetCurrentUserID(c))
}
