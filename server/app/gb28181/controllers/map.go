package controllers

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	gbdirectory "uvplatform.cn/uvp-gb28181/app/gb28181/directory"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/datascope"
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
		q = q.Where("gb_channel.status = ?", gbmodels.ChannelStatusOnline)
	} else if s == "offline" {
		q = q.Where("gb_channel.status = ?", gbmodels.ChannelStatusOffline)
	}
	if kw := strings.TrimSpace(c.Query("q")); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("gb_channel.name LIKE ? OR gb_channel.alias LIKE ? OR gb_channel.channel_id LIKE ?", like, like, like)
	}
	if nodeIDStr := c.Query("nodeId"); nodeIDStr != "" {
		if id, err := strconv.ParseUint(nodeIDStr, 10, 64); err == nil {
			var root gbmodels.GbCatalogNode
			if db.WithContext(c.Request.Context()).Scopes(ownerDeptScope(c)).Where("id = ?", id).Limit(1).Find(&root).RowsAffected > 0 {
				var channelIDs []uint
				db.WithContext(c.Request.Context()).Model(&gbmodels.GbCatalogNode{}).Scopes(ownerDeptScope(c)).
					Where("node_type = ? AND path LIKE ?", gbmodels.NodeTypeChannel, root.Path+"%").Pluck("channel_id", &channelIDs)
				if len(channelIDs) > 0 {
					q = q.Where("gb_channel.id IN ?", channelIDs)
				} else {
					q = q.Where("1=0")
				}
			}
		}
	}
	return q
}

func applyMapDirectoryFilter(c *gin.Context, db *gorm.DB, q *gorm.DB) (*gorm.DB, error) {
	if c.Query("directoryView") == "" && c.Query("directoryKey") == "" {
		return q, nil
	}
	if c.Query("nodeId") != "" {
		return q, gbdirectory.ErrDirectoryFilterInvalid
	}
	scope, err := resolveDirectoryDeviceScope(c, db)
	if err != nil {
		return q, err
	}
	if len(scope.Codes) == 0 {
		return q.Where("1=0"), nil
	}
	return q.Where("gb_channel.device_id IN ?", scope.Codes), nil
}

type markerVO struct {
	ID        uint    `json:"id"`
	ChannelID string  `json:"channelId"`
	Name      string  `json:"name"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Status    int8    `json:"status"`
	// PositionSource 这对坐标是谁写的(catalog/mobile/manual)，与通道列表同一口径。
	PositionSource string `json:"positionSource"`
	// PositionUpdatedAt 设备最近一次**位置上报**的时刻(取自位置表)，不是坐标的写入时刻。
	//
	// ⛔ 与 `ChannelVO.positionUpdatedAt`(= gb_channel.position_updated_at，坐标最后一次被谁改的时间)
	//	**同名不同义**，别互换：人工录入的坐标压根没有上报时间；目录声明的坐标也不更新这里。
	//	本字段只服务 `PositionStale` 这一个判据，展示给用户看的是 gb_channel 那一列。
	PositionUpdatedAt *time.Time `json:"positionUpdatedAt,omitempty"`
	PositionStale     bool       `json:"positionStale"`
}

// clusterSingle 是聚合里"落单的那一个通道"的身份，**刻意不是 markerVO 的替代品**。
//
// 为什么不直接嵌 markerVO:markerVO 带着 PositionUpdatedAt/PositionStale（位置新鲜度），
// 而聚合查询根本不查位置表。若这里嵌 markerVO，同一个通道会从 markers 接口和 clusters
// 接口拿到**两份新鲜度**，两边一旦不同就是本仓最忌讳的"同一字段多个说法"。
// 所以这里只放"在地图上画一个标记点"真正需要的东西,坐标由所在 cluster 的质心提供
// （Count==1 时质心就是它自己）。
type clusterSingle struct {
	ID        uint   `json:"id"`
	ChannelID string `json:"channelId"`
	Name      string `json:"name"`
	Status    int8   `json:"status"`
}

// latestPositionByChannel 取各通道最新的**移动位置**记录，只用于标注"位置是否新鲜"。
//
// ⛔ 别再拿它给坐标兜底（本函数历史上配合 COALESCE 干过这件事，2026-09-21 已撤）。
//
//	`gb_channel.longitude/latitude` 现在由三条通路共同回写（目录声明 / 位置订阅 /
//	人工录入），它就是坐标的**唯一展示落点**；再从位置表取一次值会让地图与列表分叉 ——
//	典型场景：设备报过位置后管理员人工改了坐标，位置表里那条旧值会把人工值盖掉。
//	位置表在本文件里只回答「设备上次报位置是什么时候」这一个问题。
func latestPositionByChannel(c *gin.Context, db *gorm.DB, ids []uint) map[uint]gbmodels.GbMobilePositionLatest {
	if len(ids) == 0 {
		return nil
	}
	var positions []gbmodels.GbMobilePositionLatest
	if err := db.WithContext(c.Request.Context()).Where("channel_id IN ?", ids).Find(&positions).Error; err != nil {
		return nil
	}
	out := make(map[uint]gbmodels.GbMobilePositionLatest, len(positions))
	for _, position := range positions {
		if position.ChannelID != nil {
			out[*position.ChannelID] = position
		}
	}
	return out
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

	q := db.WithContext(c.Request.Context()).Model(&gbmodels.GbChannel{}).Scopes(datascope.VisibilityScope(c, "gb_channel.owner_dept_id", "gb_channel.device_id")).
		Where("gb_channel.latitude != 0 AND gb_channel.longitude != 0")
	q = applyMapFilters(c, db, q)
	q, err := applyMapDirectoryFilter(c, db, q)
	if err != nil {
		mc.Fail(c, "目录筛选参数错误", err, 400)
		return
	}
	if maxLat > minLat {
		q = q.Where("gb_channel.latitude BETWEEN ? AND ?", minLat, maxLat)
	}
	if maxLng > minLng {
		q = q.Where("gb_channel.longitude BETWEEN ? AND ?", minLng, maxLng)
	}

	var list []gbmodels.GbChannel
	if err := q.Limit(limit).Find(&list).Error; err != nil {
		mc.FailAndAbort(c, "查询 markers 失败", err)
		return
	}

	ids := make([]uint, 0, len(list))
	for _, channel := range list {
		ids = append(ids, channel.ID)
	}
	positions := latestPositionByChannel(c, db, ids)
	now := time.Now()
	out := make([]markerVO, 0, len(list))
	for _, ch := range list {
		marker := markerVO{
			ID:             ch.ID,
			ChannelID:      ch.ChannelID,
			Name:           ch.Name,
			Latitude:       ch.Latitude,
			Longitude:      ch.Longitude,
			Status:         ch.Status,
			PositionSource: ch.PositionSource,
		}
		if position, ok := positions[ch.ID]; ok {
			marker.PositionUpdatedAt = &position.ReceivedAt
			marker.PositionStale = position.ReceivedAt.Before(now.Add(-90 * time.Second))
		}
		out = append(out, marker)
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

	q := db.WithContext(c.Request.Context()).Model(&gbmodels.GbChannel{}).Scopes(datascope.VisibilityScope(c, "gb_channel.owner_dept_id", "gb_channel.device_id")).
		Where("gb_channel.latitude != 0 AND gb_channel.longitude != 0")
	q = applyMapFilters(c, db, q)
	q, err := applyMapDirectoryFilter(c, db, q)
	if err != nil {
		mc.Fail(c, "目录筛选参数错误", err, 400)
		return
	}
	if maxLat > minLat {
		q = q.Where("gb_channel.latitude BETWEEN ? AND ?", minLat, maxLat)
	}
	if maxLng > minLng {
		q = q.Where("gb_channel.longitude BETWEEN ? AND ?", minLng, maxLng)
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
		// 簇的包围盒。点击聚合要"刚好框住这一簇"再放大:只回质心的话,
		// 放大后散在质心四周的点会跑出视野 —— 用户看到的是"点了,然后点没了"。
		// Count==1 时四个值与质心相同。
		MinLat float64 `json:"minLat"`
		MaxLat float64 `json:"maxLat"`
		MinLng float64 `json:"minLng"`
		MaxLng float64 `json:"maxLng"`
		// Single 只在 Count==1 时非空。
		//
		// 为什么要回身份:前端在低于标记点阈值的层级画的是聚合气泡,而"落单的通道"
		// 天然就是一个 Count==1 的分组。不回身份,前端只能把它画成一个写着"1"的气泡 ——
		// 用户看到"明明只有一路通道,却显示成聚合"。行业惯例是 count==1 不聚合:
		// 直接画成通道标记点。后端把身份给出来,前端才有得选。
		Single *clusterSingle `json:"single,omitempty"`
	}
	grid := map[[2]int]*cluster{}
	for _, ch := range list {
		key := [2]int{int(ch.Latitude / gridSize), int(ch.Longitude / gridSize)}
		cl := grid[key]
		if cl == nil {
			cl = &cluster{
				CenterLat: ch.Latitude, CenterLng: ch.Longitude,
				MinLat: ch.Latitude, MaxLat: ch.Latitude,
				MinLng: ch.Longitude, MaxLng: ch.Longitude,
			}
			grid[key] = cl
		} else {
			// 累积平均
			cl.CenterLat = (cl.CenterLat*float64(cl.Count) + ch.Latitude) / float64(cl.Count+1)
			cl.CenterLng = (cl.CenterLng*float64(cl.Count) + ch.Longitude) / float64(cl.Count+1)
			if ch.Latitude < cl.MinLat {
				cl.MinLat = ch.Latitude
			}
			if ch.Latitude > cl.MaxLat {
				cl.MaxLat = ch.Latitude
			}
			if ch.Longitude < cl.MinLng {
				cl.MinLng = ch.Longitude
			}
			if ch.Longitude > cl.MaxLng {
				cl.MaxLng = ch.Longitude
			}
		}
		cl.Count++
		if ch.Status == gbmodels.ChannelStatusOnline {
			cl.OnlineCount++
		}
		// 落单时带上身份;同一格子进来第二个通道就不再落单,把身份摘掉。
		if cl.Count == 1 {
			cl.Single = &clusterSingle{
				ID: ch.ID, ChannelID: ch.ChannelID, Name: ch.Name, Status: ch.Status,
			}
		} else {
			cl.Single = nil
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
	q := db.WithContext(c.Request.Context()).Model(&gbmodels.GbChannel{}).Scopes(datascope.VisibilityScope(c, "gb_channel.owner_dept_id", "gb_channel.device_id"))
	q = applyMapFilters(c, db, q).Where("(gb_channel.latitude = 0 OR gb_channel.longitude = 0)")
	q, err := applyMapDirectoryFilter(c, db, q)
	if err != nil {
		mc.Fail(c, "目录筛选参数错误", err, 400)
		return
	}
	if err := q.Count(&count).Error; err != nil {
		mc.FailAndAbort(c, "查询失败", err)
		return
	}
	mc.Success(c, gin.H{"count": count})
}
