package controllers

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/controllers"
	gbdirectory "uvplatform.com/uvp-gb28181/app/gb28181/directory"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/subscribe"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/utils/datascope"
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
	// ⛔ 用app.DB()（按 usedbtype 分发到 MySQL/PG/SQLServer/SQLite），
	//   不能写死 app.GormDbMysql：那样在绿色包的 SQLite 模式下拿到 nil，
	//   设备/告警/地图等页面一律报「DB 未就绪」，而日志里看不出原因。
	return &MapController{db: func() *gorm.DB { return app.DB() }}
}

func (mc *MapController) SetDB(p func() *gorm.DB) { mc.db = p }

// applyMapFilters keeps map results consistent with the channel list filters.
func applyMapFilters(c *gin.Context, db *gorm.DB, q *gorm.DB) *gorm.DB {
	if deviceID := strings.TrimSpace(c.Query("deviceId")); deviceID != "" {
		q = q.Where("gb_channel.device_id = ?", deviceID)
	}
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
	if scope.ChannelIDs != nil {
		if len(scope.ChannelIDs) == 0 {
			return q.Where("1=0"), nil
		}
		return q.Where("gb_channel.id IN ?", scope.ChannelIDs), nil
	}
	if len(scope.Codes) == 0 {
		return q.Where("1=0"), nil
	}
	return q.Where("gb_channel.device_id IN ?", scope.Codes), nil
}

type markerVO struct {
	ID        uint   `json:"id"`
	ChannelID string `json:"channelId"`
	// DeviceID 是所属设备国标编码。地图卡片上的「播放视频」要拿它去点播，
	// 而点播入参是 (设备编码, 通道编码) 两元组 —— 只回通道 id 前端还得再查一次详情。
	DeviceID  string  `json:"deviceId"`
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
	// DeviceID 与 markerVO 同义：卡片上的播放入口需要设备编码，两条来源必须是同一份形状。
	DeviceID string `json:"deviceId"`
	Name     string `json:"name"`
	Status   int8   `json:"status"`
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
			DeviceID:       ch.DeviceID,
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
				ID: ch.ID, ChannelID: ch.ChannelID, DeviceID: ch.DeviceID, Name: ch.Name, Status: ch.Status,
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

// ── 轨迹 ───────────────────────────────────────────────────────────────

// 轨迹窗口的三条硬边界。
//
// ⛔ 上限 24h 不是为了省事：`gb_mobile_position_history` 保留期默认 7 天，
// 而一次 7 天的查询在 5 秒级上报频率下是百万行量级 —— 会直接把接口拖到超时。
// 前端的时间选择器也按这个上限做禁用，两边共用同一份口径。
const (
	// 默认给 24 小时而不是 1 小时：执法仪的值班周期就是"一整天"，
	// 默认 1 小时会让早上打开的人看到一条几乎空的轨迹，误以为功能坏了
	// （2026-09-21 设计评审已定：Q2 轨迹默认 24 小时）。
	trajectoryDefaultWindow = 24 * time.Hour
	trajectoryMaxWindow     = 24 * time.Hour
	trajectoryDefaultPoints = 2000
	trajectoryMaxPoints     = 5000
	// trajectoryMaxScanRows 是单次查询从库里取出的硬上限。
	//
	// ⛔ 不能像早期实现那样 `Limit(maxPoints+1)` 再抽稀：那样取到的是
	// **时间窗内最早的那 N+1 条**，抽稀只能在这 N+1 条里做 ⇒ 路线永远只画到开头一段，
	// 终点假落在中途（比"点少一些"更误导）。必须先全量取回、再等间隔抽稀。
	// 5 万行 ≈ 5MB 内存，24h 窗口在 5 秒级上报频率下是 1.7 万行，留了充足余量。
	trajectoryMaxScanRows = 50000
)

type trajectoryPoint struct {
	EventTime time.Time `json:"eventTime"`
	Longitude float64   `json:"longitude"`
	Latitude  float64   `json:"latitude"`
	Speed     *float64  `json:"speed,omitempty"`
	Direction *float64  `json:"direction,omitempty"`
	Altitude  *float64  `json:"altitude,omitempty"`
}

// Trajectory 返回一个通道在时间窗内的移动位置轨迹点(按时间升序)。
// 只有会发 MobilePosition 的移动设备(执法仪/车载/单兵)才有数据;固定枪机不上报,
// 所以"空轨迹"是**正常业务结果**,不是错误 —— 接口照常 200,由前端结合
// historyEnabled 区分「没开记录」与「这台设备本来就不上报」。
func (mc *MapController) Trajectory(c *gin.Context) {
	db := mc.db()
	if db == nil {
		mc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	channelID, err := strconv.ParseUint(strings.TrimSpace(c.Query("channelId")), 10, 64)
	if err != nil || channelID == 0 {
		mc.Fail(c, "channelId 非法", nil, 400)
		return
	}

	// ⛔ 先做可见性校验再查历史：历史表没有 owner_dept_id，若跳过这一步，
	//    任何登录用户拿别人的 channel id 就能拉到别人的行驶轨迹(位置即隐私)。
	var ch gbmodels.GbChannel
	res := db.WithContext(c.Request.Context()).
		Scopes(datascope.VisibilityScope(c, "gb_channel.owner_dept_id", "gb_channel.device_id")).
		Where("gb_channel.id = ?", channelID).Limit(1).Find(&ch)
	if res.Error != nil {
		mc.FailAndAbort(c, "查询通道失败", res.Error)
		return
	}
	// ⛔ 用 RowsAffected 判存在，不用 First + ErrRecordNotFound(回调会把 not-found 掩成零值)。
	if res.RowsAffected == 0 {
		mc.Fail(c, "通道不存在或无权访问", nil, 404)
		return
	}

	end := time.Now()
	start := end.Add(-trajectoryDefaultWindow)
	if raw := strings.TrimSpace(c.Query("startTime")); raw != "" {
		parsed, err := parseMapTime(raw)
		if err != nil {
			mc.Fail(c, "startTime 格式非法", err, 400)
			return
		}
		start = parsed
	}
	if raw := strings.TrimSpace(c.Query("endTime")); raw != "" {
		parsed, err := parseMapTime(raw)
		if err != nil {
			mc.Fail(c, "endTime 格式非法", err, 400)
			return
		}
		end = parsed
	}
	if !end.After(start) {
		mc.Fail(c, "结束时间需晚于开始时间", nil, 400)
		return
	}
	if end.Sub(start) > trajectoryMaxWindow {
		mc.Fail(c, "轨迹查询时间跨度不能超过 24 小时", nil, 400)
		return
	}

	maxPoints := trajectoryDefaultPoints
	if raw := strings.TrimSpace(c.Query("maxPoints")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			maxPoints = n
		}
	}
	if maxPoints < 100 {
		maxPoints = 100
	}
	if maxPoints > trajectoryMaxPoints {
		maxPoints = trajectoryMaxPoints
	}

	// 全量取回再抽稀（理由见 trajectoryMaxScanRows）。
	// Limit 仍保留一个硬上限，防止极端情况下把库和内存同时拖死。
	var rows []gbmodels.GbMobilePositionHistory
	err = db.WithContext(c.Request.Context()).
		Where("channel_id = ?", ch.ID).
		Where("event_time BETWEEN ? AND ?", start, end).
		Order("event_time ASC").
		Limit(trajectoryMaxScanRows).
		Find(&rows).Error
	if err != nil {
		mc.FailAndAbort(c, "查询轨迹失败", err)
		return
	}
	truncated := len(rows) > maxPoints || len(rows) >= trajectoryMaxScanRows
	if len(rows) > maxPoints {
		rows = thinTrajectoryRows(rows, maxPoints)
	}

	list := make([]trajectoryPoint, 0, len(rows))
	for _, row := range rows {
		list = append(list, trajectoryPoint{
			EventTime: row.EventTime,
			Longitude: row.Longitude,
			Latitude:  row.Latitude,
			Speed:     row.Speed,
			Direction: row.Direction,
			Altitude:  row.Altitude,
		})
	}
	mc.Success(c, gin.H{
		"channelId": ch.ChannelID,
		"deviceId":  ch.DeviceID,
		"name":      ch.Name,
		"list":      list,
		"total":     len(list),
		"truncated": truncated,
		// historyEnabled 是服务端"位置历史记录"总开关(服务配置里可关，交付包默认关)。
		// 前端必须用它区分两种空结果：开关关着 = 去哪开；开着还空 = 这台设备没上报。
		"historyEnabled": subscribe.PositionHistoryEnabled(),
		"startTime":      start,
		"endTime":        end,
	})
}

// thinTrajectoryRows 按时间顺序等间隔抽稀，首尾必留。
//
// 抽稀而不是"只取前 N 条"：轨迹是空间曲线，截断前半段会让用户看到
// 一条只走了一半的路线，而且终点会假落在中途 —— 比"点少一些"更误导。
func thinTrajectoryRows(rows []gbmodels.GbMobilePositionHistory, limit int) []gbmodels.GbMobilePositionHistory {
	if limit <= 0 || len(rows) <= limit || limit == 1 {
		return rows
	}
	out := make([]gbmodels.GbMobilePositionHistory, 0, limit)
	step := float64(len(rows)-1) / float64(limit-1)
	last := -1
	for i := 0; i < limit; i++ {
		idx := int(math.Round(float64(i) * step))
		if idx >= len(rows) {
			idx = len(rows) - 1
		}
		if idx == last {
			continue
		}
		out = append(out, rows[idx])
		last = idx
	}
	// 浮点累计误差可能让最后一个下标没落到末尾，这里强制对齐，
	// 否则路线会停在"倒数第二个采样点"，看起来像设备在终点前消失了。
	out[0] = rows[0]
	out[len(out)-1] = rows[len(rows)-1]
	return out
}

// parseMapTime 接受 RFC3339 与"空格分隔的本地时间"两种写法。
//
// 与 subscribe.parsePositionTime 保持同一组合法格式：前端时间选择器给的是本地
// `YYYY-MM-DD HH:mm:ss`，而接口文档举例用 RFC3339，两种都收才不会出现
// "文档里的能查、界面里的报 400"。
func parseMapTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("时间格式非法: %q", value)
}

// ── 默认中心点 ─────────────────────────────────────────────────────────

// mapCenterParamCode 是"地图默认视野"在 sys_param 里的键。
//
// 不自建表：中心点是**一份 JSON 配置**，不是业务实体（没有生命周期、没有列表、
// 不需要按部门多份）。sys_param 就是为这类"单键值"准备的表。
//
// 语义是**全平台一份**：平台常见用法是值班室大屏固定在辖区中心，换个人登录
// 看到的还是同一个视野；按用户各存一份反而会让"进来先看哪"变成随机。
const mapCenterParamCode = "gb28181.map.center"

const (
	// 与前端 mapMinZoom / mapMaxZoom 同一对边界：两边不一致会出现
	// "存进去 3 级、打开却是 5 级"的静默改写。
	mapCenterMinZoom = 5.0
	mapCenterMaxZoom = 22.0
)

// mapCenterPayload 是默认视野三元组。
//
// ⛔ zoom 用 float64 而不是 int：MapLibre 的 zoom 本身就是小数，
// 存整数会让用户设的 11.5 级在保存后悄悄变成 12 —— 地图会"自己动了一下"。
type mapCenterPayload struct {
	Longitude float64 `json:"longitude"`
	Latitude  float64 `json:"latitude"`
	Zoom      float64 `json:"zoom"`
}

func validateMapCenter(p mapCenterPayload) error {
	if p.Longitude < -180 || p.Longitude > 180 {
		return fmt.Errorf("经度应在 -180 ~ 180 之间")
	}
	if p.Latitude < -90 || p.Latitude > 90 {
		return fmt.Errorf("纬度应在 -90 ~ 90 之间")
	}
	// 0/0 在本仓全链路表示"无坐标"（与通道坐标同一口径），不能当成一个真实中心点存下来。
	if p.Longitude == 0 && p.Latitude == 0 {
		return fmt.Errorf("经纬度不能同时为 0")
	}
	if p.Zoom < mapCenterMinZoom || p.Zoom > mapCenterMaxZoom {
		return fmt.Errorf("缩放级别应在 %.0f ~ %.0f 之间", mapCenterMinZoom, mapCenterMaxZoom)
	}
	return nil
}

// GetCenter 读取默认中心点。未设置返回 center: null(前端回退内置默认视野)。
func (mc *MapController) GetCenter(c *gin.Context) {
	db := mc.db()
	if db == nil {
		mc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	// ⛔ Find + RowsAffected 判存在，不用 First + ErrRecordNotFound。
	var param models.SysParam
	res := db.WithContext(c.Request.Context()).Where("code = ?", mapCenterParamCode).Limit(1).Find(&param)
	if res.Error != nil {
		mc.FailAndAbort(c, "读取地图中心点失败", res.Error)
		return
	}
	if res.RowsAffected == 0 || param.Value == nil {
		mc.Success(c, gin.H{"center": nil})
		return
	}
	var center mapCenterPayload
	// 脏值(手改库/半截写入)一律当"未设置"：地图必须能打开，
	// 不能因为一条配置解析失败就让整个视图报错。
	if err := json.Unmarshal([]byte(*param.Value), &center); err != nil || validateMapCenter(center) != nil {
		mc.Success(c, gin.H{"center": nil})
		return
	}
	mc.Success(c, gin.H{"center": center})
}

// SetCenter 保存默认中心点(有则更新、无则插入)。
func (mc *MapController) SetCenter(c *gin.Context) {
	db := mc.db()
	if db == nil {
		mc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	var body mapCenterPayload
	if err := c.ShouldBindJSON(&body); err != nil {
		mc.Fail(c, "请求参数格式错误", err, 400)
		return
	}
	if err := validateMapCenter(body); err != nil {
		mc.Fail(c, err.Error(), nil, 400)
		return
	}
	raw, err := json.Marshal(body)
	if err != nil {
		mc.FailAndAbort(c, "序列化中心点失败", err)
		return
	}

	var param models.SysParam
	res := db.WithContext(c.Request.Context()).Where("code = ?", mapCenterParamCode).Limit(1).Find(&param)
	if res.Error != nil {
		mc.FailAndAbort(c, "读取地图中心点失败", res.Error)
		return
	}
	if res.RowsAffected == 0 {
		name := "地图默认中心点"
		code := mapCenterParamCode
		value := string(raw)
		status := int8(1)
		description := "设备地图进入时默认定位的中心点与缩放级别(JSON)"
		row := models.SysParam{Name: &name, Code: &code, Value: &value, Status: &status, Description: &description}
		if err := db.WithContext(c.Request.Context()).Create(&row).Error; err != nil {
			mc.FailAndAbort(c, "保存地图中心点失败", err)
			return
		}
	} else {
		if err := db.WithContext(c.Request.Context()).Model(&param).Update("value", string(raw)).Error; err != nil {
			mc.FailAndAbort(c, "保存地图中心点失败", err)
			return
		}
	}
	mc.Success(c, gin.H{"center": body})
}
