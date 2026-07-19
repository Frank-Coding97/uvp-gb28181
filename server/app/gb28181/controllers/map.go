package controllers

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// MapController 地图视图相关接口(plan §4.2 B3)
//
//	GET /map/markers?minLat&maxLat&minLng&maxLng&limit
//	GET /map/clusters?zoom&minLat&maxLat&minLng&maxLng
//	GET /map/no-coord-count
type MapController struct {
	controllers.Common
	db func() *gorm.DB
}

func NewMapController() *MapController {
	return &MapController{db: func() *gorm.DB { return app.GormDbMysql }}
}

func (mc *MapController) SetDB(p func() *gorm.DB) { mc.db = p }

// applyMapFilters keeps map results consistent with the channel list filters.
func applyMapFilters(c *gin.Context, db *gorm.DB, q *gorm.DB) *gorm.DB {
	if s := c.Query("status"); s == "online" {
		q = q.Where("status = ?", gbmodels.ChannelStatusOnline)
	} else if s == "offline" {
		q = q.Where("status = ?", gbmodels.ChannelStatusOffline)
	}
	if kw := strings.TrimSpace(c.Query("q")); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("name LIKE ? OR alias LIKE ? OR channel_id LIKE ?", like, like, like)
	}
	if nodeIDStr := c.Query("nodeId"); nodeIDStr != "" {
		if id, err := strconv.ParseUint(nodeIDStr, 10, 64); err == nil {
			var root gbmodels.GbCatalogNode
			if db.WithContext(c).Scopes(ownerDeptScope(c)).Where("id = ?", id).Limit(1).Find(&root).RowsAffected > 0 {
				var channelIDs []uint
				db.WithContext(c).Model(&gbmodels.GbCatalogNode{}).Scopes(ownerDeptScope(c)).
					Where("node_type = ? AND path LIKE ?", gbmodels.NodeTypeChannel, root.Path+"%").Pluck("channel_id", &channelIDs)
				if len(channelIDs) > 0 {
					q = q.Where("id IN ?", channelIDs)
				} else {
					q = q.Where("1=0")
				}
			}
		}
	}
	return q
}

type markerVO struct {
	ID        uint    `json:"id"`
	ChannelID string  `json:"channelId"`
	Name      string  `json:"name"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Status    int8    `json:"status"`
}

// Markers 视野矩形内的 marker(plan §4.2 B3.1)
//
// 限制返回数(默认 ≤ 500)防 DoS;前端 zoom 越大客户端筛越细
func (mc *MapController) Markers(c *gin.Context) {
	db := mc.db()
	if db == nil {
		mc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	minLat, _ := strconv.ParseFloat(c.Query("minLat"), 64)
	maxLat, _ := strconv.ParseFloat(c.Query("maxLat"), 64)
	minLng, _ := strconv.ParseFloat(c.Query("minLng"), 64)
	maxLng, _ := strconv.ParseFloat(c.Query("maxLng"), 64)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "500"))
	if limit <= 0 || limit > 1000 {
		limit = 500
	}

	q := db.WithContext(c).Model(&gbmodels.GbChannel{}).Scopes(ownerDeptScope(c)).
		Where("latitude != 0 AND longitude != 0")
	q = applyMapFilters(c, db, q)
	if maxLat > minLat {
		q = q.Where("latitude BETWEEN ? AND ?", minLat, maxLat)
	}
	if maxLng > minLng {
		q = q.Where("longitude BETWEEN ? AND ?", minLng, maxLng)
	}

	var list []gbmodels.GbChannel
	if err := q.Limit(limit).Find(&list).Error; err != nil {
		mc.FailAndAbort(c, "查询 markers 失败", err)
		return
	}

	out := make([]markerVO, 0, len(list))
	for _, ch := range list {
		out = append(out, markerVO{
			ID:        ch.ID,
			ChannelID: ch.ChannelID,
			Name:      ch.Name,
			Latitude:  ch.Latitude,
			Longitude: ch.Longitude,
			Status:    ch.Status,
		})
	}
	mc.Success(c, gin.H{"list": out, "total": len(out)})
}

// Clusters 服务端 cluster(简化实现:zoom < 12 时按网格分组)
//
// Phase 1 用简单网格法,Phase 2 上 geohash 精确分级
func (mc *MapController) Clusters(c *gin.Context) {
	db := mc.db()
	if db == nil {
		mc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	zoom, _ := strconv.Atoi(c.DefaultQuery("zoom", "10"))
	if zoom <= 0 {
		zoom = 10
	}

	// 网格大小随 zoom 调整(经验值)
	// zoom 6 → 5 度, zoom 10 → 0.1 度, zoom 14 → 0.01 度
	gridSize := 5.0 / float64(int(1)<<uint(zoom/3))
	if gridSize < 0.01 {
		gridSize = 0.01
	}

	// 加载视野内所有 markers,服务端做分组
	minLat, _ := strconv.ParseFloat(c.Query("minLat"), 64)
	maxLat, _ := strconv.ParseFloat(c.Query("maxLat"), 64)
	minLng, _ := strconv.ParseFloat(c.Query("minLng"), 64)
	maxLng, _ := strconv.ParseFloat(c.Query("maxLng"), 64)

	q := db.WithContext(c).Model(&gbmodels.GbChannel{}).Scopes(ownerDeptScope(c)).
		Where("latitude != 0 AND longitude != 0")
	q = applyMapFilters(c, db, q)
	if maxLat > minLat {
		q = q.Where("latitude BETWEEN ? AND ?", minLat, maxLat)
	}
	if maxLng > minLng {
		q = q.Where("longitude BETWEEN ? AND ?", minLng, maxLng)
	}

	var list []gbmodels.GbChannel
	if err := q.Limit(5000).Find(&list).Error; err != nil {
		mc.FailAndAbort(c, "查询失败", err)
		return
	}

	type cluster struct {
		CenterLat   float64 `json:"centerLat"`
		CenterLng   float64 `json:"centerLng"`
		Count       int     `json:"count"`
		OnlineCount int     `json:"onlineCount"`
		OnlineRate  float64 `json:"onlineRate"`
	}
	grid := map[[2]int]*cluster{}
	for _, ch := range list {
		key := [2]int{int(ch.Latitude / gridSize), int(ch.Longitude / gridSize)}
		cl := grid[key]
		if cl == nil {
			cl = &cluster{CenterLat: ch.Latitude, CenterLng: ch.Longitude}
			grid[key] = cl
		} else {
			// 累积平均
			cl.CenterLat = (cl.CenterLat*float64(cl.Count) + ch.Latitude) / float64(cl.Count+1)
			cl.CenterLng = (cl.CenterLng*float64(cl.Count) + ch.Longitude) / float64(cl.Count+1)
		}
		cl.Count++
		if ch.Status == gbmodels.ChannelStatusOnline {
			cl.OnlineCount++
		}
	}

	clusters := make([]cluster, 0, len(grid))
	for _, cl := range grid {
		if cl.Count > 0 {
			cl.OnlineRate = float64(cl.OnlineCount) / float64(cl.Count)
		}
		clusters = append(clusters, *cl)
	}
	mc.Success(c, gin.H{"clusters": clusters, "zoom": zoom, "gridSize": gridSize})
}

// NoCoordCount 当前过滤下无坐标 channel 数(banner 提示用)
func (mc *MapController) NoCoordCount(c *gin.Context) {
	db := mc.db()
	if db == nil {
		mc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	var count int64
	q := db.WithContext(c).Model(&gbmodels.GbChannel{}).Scopes(ownerDeptScope(c))
	q = applyMapFilters(c, db, q).Where("(latitude = 0 OR longitude = 0)")
	if err := q.Count(&count).Error; err != nil {
		mc.FailAndAbort(c, "查询失败", err)
		return
	}
	mc.Success(c, gin.H{"count": count})
}
