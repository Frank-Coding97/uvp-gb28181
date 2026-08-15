package controllers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

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
	result := dc.db.WithContext(c).Scopes(datascope.VisibilityScope(c, "owner_dept_id", "device_id")).
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
	result = dc.db.WithContext(c).Scopes(datascope.VisibilityScope(c, "owner_dept_id", "device_id")).
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
	from := to.AddDate(0, 0, -29)
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
	query := dc.db.WithContext(c).Model(&gbmodels.GbDeviceTrafficDaily{}).
		Where("device_code = ? AND stat_date >= ? AND stat_date <= ?", scope.DeviceCode, from, to)
	if scope.ChannelCode != "" {
		query = query.Where("channel_code = ?", scope.ChannelCode)
	}
	return query
}

func (dc *DeviceTrafficController) Summary(c *gin.Context) {
	scope, ok := dc.scope(c, false)
	if !ok {
		return
	}
	from, to, err := trafficRange(c)
	if err != nil {
		dc.FailAndAbort(c, "日期范围不合法", err)
		return
	}
	var total struct {
		UpstreamBytes, DownstreamBytes                     uint64
		UpstreamDurationSeconds, DownstreamDurationSeconds int64
		UpstreamSessions, DownstreamSessions               int64
	}
	err = dc.dailyQuery(c, scope, from, to).Select(
		"COALESCE(SUM(upstream_bytes),0) AS upstream_bytes, COALESCE(SUM(downstream_bytes),0) AS downstream_bytes, " +
			"COALESCE(SUM(upstream_duration_seconds),0) AS upstream_duration_seconds, COALESCE(SUM(downstream_duration_seconds),0) AS downstream_duration_seconds, " +
			"COALESCE(SUM(upstream_sessions),0) AS upstream_sessions, COALESCE(SUM(downstream_sessions),0) AS downstream_sessions").Scan(&total).Error
	if err != nil {
		dc.FailAndAbort(c, "查询流量汇总失败", err)
		return
	}
	dc.Success(c, gin.H{
		"upstreamBytes": total.UpstreamBytes, "downstreamBytes": total.DownstreamBytes,
		"totalBytes":              total.UpstreamBytes + total.DownstreamBytes,
		"upstreamDurationSeconds": total.UpstreamDurationSeconds, "downstreamDurationSeconds": total.DownstreamDurationSeconds,
		"upstreamSessions": total.UpstreamSessions, "downstreamSessions": total.DownstreamSessions,
		"from": from.Format("2006-01-02"), "to": to.Format("2006-01-02"), "timezone": "UTC",
	})
}

type trafficTrendRow struct {
	StatDate        time.Time `json:"-"`
	UpstreamBytes   uint64    `json:"upstreamBytes"`
	DownstreamBytes uint64    `json:"downstreamBytes"`
}

func (dc *DeviceTrafficController) Trend(c *gin.Context) {
	scope, ok := dc.scope(c, false)
	if !ok {
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
		list = append(list, gin.H{"date": row.StatDate.Format("2006-01-02"), "upstreamBytes": row.UpstreamBytes,
			"downstreamBytes": row.DownstreamBytes, "totalBytes": row.UpstreamBytes + row.DownstreamBytes})
	}
	dc.Success(c, gin.H{"list": list, "from": from.Format("2006-01-02"), "to": to.Format("2006-01-02"), "timezone": "UTC"})
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
	query := dc.db.WithContext(c).Model(&gbmodels.GbDeviceTrafficSession{}).Where("device_code = ?", scope.DeviceCode)
	if scope.ChannelCode != "" {
		query = query.Where("channel_code = ?", scope.ChannelCode)
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
	if err := dc.db.WithContext(c).Where("started_at < ? AND (ended_at IS NULL OR ended_at >= ?)", toExclusive, from).Order("started_at").Find(&gaps).Error; err != nil {
		dc.FailAndAbort(c, "查询采集覆盖范围失败", err)
		return
	}
	var startedAt *time.Time
	var first gbmodels.GbDeviceTrafficSession
	if result := dc.db.WithContext(c).Order("created_at").Limit(1).Find(&first); result.Error == nil && result.RowsAffected > 0 {
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

func (dc *DeviceTrafficController) listViewers(c *gin.Context, scope trafficScope) ([]currentViewer, error) {
	client, _, ok := dc.channelClient(c, scope)
	if !ok {
		return []currentViewer{}, nil
	}
	media, err := client.GetMediaList(c.Request.Context(), "__defaultVhost__", "rtp", scope.Channel.StreamID)
	if err != nil {
		return nil, err
	}
	result := make([]currentViewer, 0)
	seen := make(map[string]struct{})
	for _, item := range media {
		if item.ReaderCount <= 0 || item.Schema == "" {
			continue
		}
		players, err := client.GetMediaPlayerList(c.Request.Context(), item.Schema, item.VHost, item.App, item.Stream)
		if err != nil {
			return nil, err
		}
		for _, player := range players {
			key := item.Schema + "\x00" + player.Identifier
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			typeID := strings.ToLower(player.TypeID)
			result = append(result, currentViewer{
				ChannelID: scope.ChannelCode, Schema: item.Schema,
				Remote: fmt.Sprintf("%s:%d", player.PeerIP, player.PeerPort), LocalPort: player.LocalPort,
				ID: player.Identifier, Type: player.TypeID,
				Kickable: player.Identifier != "" && typeID != "" && !strings.Contains(typeID, "udp"),
			})
		}
	}
	return result, nil
}

func (dc *DeviceTrafficController) Viewers(c *gin.Context) {
	scope, ok := dc.scope(c, true)
	if !ok {
		return
	}
	list, err := dc.listViewers(c, scope)
	if err != nil {
		dc.FailAndAbort(c, "查询当前观看连接失败", err)
		return
	}
	dc.Success(c, gin.H{"list": list, "total": len(list), "canKick": trafficSuperAdmin(c)})
}

func (dc *DeviceTrafficController) KickViewer(c *gin.Context) {
	if !trafficSuperAdmin(c) {
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
	if app.ZapLog != nil {
		app.ZapLog.Info("GB28181 当前观看连接已强退", zap.Uint("operatorId", common.GetCurrentUserID(c)),
			zap.String("deviceId", scope.DeviceCode), zap.String("channelId", scope.ChannelCode), zap.Int64("nodeId", mediaNode.ID))
	}
	dc.Success(c, gin.H{"kicked": true})
}

func trafficSuperAdmin(c *gin.Context) bool {
	return app.ConfigYml != nil && common.IsSkipAuthUser(common.GetCurrentUserID(c))
}
