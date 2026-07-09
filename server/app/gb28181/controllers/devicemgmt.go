package controllers

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// DeviceMgmtController 设备管理页主查询(plan §4.2 B2)
//
//   GET /devices               设备列表(多过滤 + 分页 + 排序 + mountCount)
//   GET /device/:id            设备详情
//   GET /channels              通道列表
//   GET /channel/:id           通道详情
//   GET /channel/:id/mounts    通道多挂载位置
//   GET /channel/:id/timeline  通道 24h 在线时序(Phase 1 简化:基于 last_status_at)
type DeviceMgmtController struct {
	controllers.Common
	db func() *gorm.DB
}

func NewDeviceMgmtController() *DeviceMgmtController {
	return &DeviceMgmtController{db: func() *gorm.DB { return app.GormDbMysql }}
}

func (dc *DeviceMgmtController) SetDB(p func() *gorm.DB) { dc.db = p }

// channelStats 单个 channel 的派生聚合
type deviceVOExtra struct {
	*gbmodels.GbDevice
	Online            bool `json:"online"`
	ChannelCount      int64 `json:"channelCount"`
	ChannelOnlineCount int64 `json:"channelOnlineCount"`
	OnlineRate        float64 `json:"onlineRate"`
}

// ListDevices 设备列表
// GET /devices?nodeId=&status=online&vendor=&q=&page=1&pageSize=20&sort=name:asc
func (dc *DeviceMgmtController) ListDevices(c *gin.Context) {
	db := dc.db()
	if db == nil {
		dc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	tid := tenantOf(c)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 20
	}

	q := db.WithContext(c).Model(&gbmodels.GbDevice{}).Where("tenant_id = ?", tid)
	if s := c.Query("status"); s != "" {
		switch s {
		case "online":
			q = q.Where("status = ?", gbmodels.DeviceStatusOnline)
		case "offline":
			q = q.Where("status = ?", gbmodels.DeviceStatusOffline)
		}
	}
	if v := c.Query("vendor"); v != "" {
		q = q.Where("manufacturer = ?", v)
	}
	if kw := strings.TrimSpace(c.Query("q")); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("name LIKE ? OR device_id LIKE ?", like, like)
	}

	// 排序:sort=name:asc 或 keepalive:desc
	if so := c.Query("sort"); so != "" {
		parts := strings.SplitN(so, ":", 2)
		if len(parts) == 2 && (parts[1] == "asc" || parts[1] == "desc") {
			col := safeSortColumn(parts[0])
			if col != "" {
				q = q.Order(col + " " + parts[1])
			}
		}
	} else {
		q = q.Order("id DESC")
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		dc.FailAndAbort(c, "统计失败", err)
		return
	}
	var list []gbmodels.GbDevice
	if err := q.Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		dc.FailAndAbort(c, "查询失败", err)
		return
	}

	// 派生通道聚合
	vos := make([]deviceVOExtra, 0, len(list))
	for i := range list {
		d := list[i]
		stats := dc.channelAggregate(c, db, d.DeviceID)
		vos = append(vos, deviceVOExtra{
			GbDevice:           &d,
			Online:             d.Status == gbmodels.DeviceStatusOnline,
			ChannelCount:       stats.total,
			ChannelOnlineCount: stats.online,
			OnlineRate:         stats.rate(),
		})
	}
	dc.Success(c, gin.H{"list": vos, "total": total, "page": page, "pageSize": pageSize})
}

type chStats struct{ total, online int64 }

func (s chStats) rate() float64 {
	if s.total == 0 {
		return 0
	}
	return float64(s.online) / float64(s.total)
}

func (dc *DeviceMgmtController) channelAggregate(c *gin.Context, db *gorm.DB, deviceID string) chStats {
	var total, online int64
	_ = db.WithContext(c).Model(&gbmodels.GbChannel{}).Where("device_id = ?", deviceID).Count(&total).Error
	_ = db.WithContext(c).Model(&gbmodels.GbChannel{}).Where("device_id = ? AND status = ?", deviceID, gbmodels.ChannelStatusOnline).Count(&online).Error
	return chStats{total: total, online: online}
}

func safeSortColumn(s string) string {
	allowed := map[string]string{
		"name":      "name",
		"heartbeat": "keepalive_time",
		"status":    "status",
		"createdAt": "created_at",
		"id":        "id",
	}
	return allowed[s]
}

// GetDevice 设备详情
// GET /device/:id
func (dc *DeviceMgmtController) GetDevice(c *gin.Context) {
	db := dc.db()
	if db == nil {
		dc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	tid := tenantOf(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		dc.FailAndAbort(c, "ID 不合法", err)
		return
	}
	var d gbmodels.GbDevice
	res := db.WithContext(c).Where("tenant_id = ? AND id = ?", tid, id).Limit(1).Find(&d)
	if res.Error != nil {
		dc.FailAndAbort(c, "查询失败", res.Error)
		return
	}
	if res.RowsAffected == 0 {
		dc.FailAndAbort(c, "设备不存在", nil)
		return
	}
	stats := dc.channelAggregate(c, db, d.DeviceID)
	dc.Success(c, deviceVOExtra{
		GbDevice:           &d,
		Online:             d.Status == gbmodels.DeviceStatusOnline,
		ChannelCount:       stats.total,
		ChannelOnlineCount: stats.online,
		OnlineRate:         stats.rate(),
	})
}

// ListChannels 通道列表(列表/卡片视图通用)
// GET /channels?nodeId=&status=online&ptz=1&hasSnapshot=1&page=1&pageSize=40
func (dc *DeviceMgmtController) ListChannels(c *gin.Context) {
	db := dc.db()
	if db == nil {
		dc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	tid := tenantOf(c)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "40"))
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 40
	}

	q := db.WithContext(c).Model(&gbmodels.GbChannel{}).Where("tenant_id = ?", tid)

	// nodeId 过滤:走子树 path LIKE
	if nodeIDStr := c.Query("nodeId"); nodeIDStr != "" {
		if id, err := strconv.ParseUint(nodeIDStr, 10, 64); err == nil {
			var root gbmodels.GbCatalogNode
			if db.WithContext(c).Where("tenant_id = ? AND id = ?", tid, id).Limit(1).Find(&root).RowsAffected > 0 {
				// 子树下的所有 channel_id
				var chIDs []uint
				db.WithContext(c).Model(&gbmodels.GbCatalogNode{}).
					Where("tenant_id = ? AND node_type = ? AND path LIKE ?", tid, gbmodels.NodeTypeChannel, root.Path+"%").
					Pluck("channel_id", &chIDs)
				if len(chIDs) > 0 {
					q = q.Where("id IN ?", chIDs)
				} else {
					q = q.Where("1=0")
				}
			}
		}
	}

	if s := c.Query("status"); s == "online" {
		q = q.Where("status = ?", gbmodels.ChannelStatusOnline)
	} else if s == "offline" {
		q = q.Where("status = ?", gbmodels.ChannelStatusOffline)
	}
	if c.Query("ptz") == "1" {
		q = q.Where("ptz_type > 0")
	}
	if kw := strings.TrimSpace(c.Query("q")); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("name LIKE ? OR channel_id LIKE ?", like, like)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		dc.FailAndAbort(c, "统计失败", err)
		return
	}
	var list []gbmodels.GbChannel
	if err := q.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		dc.FailAndAbort(c, "查询失败", err)
		return
	}

	dc.Success(c, gin.H{"list": list, "total": total, "page": page, "pageSize": pageSize})
}

// GetChannel 通道详情
func (dc *DeviceMgmtController) GetChannel(c *gin.Context) {
	db := dc.db()
	if db == nil {
		dc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	tid := tenantOf(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		dc.FailAndAbort(c, "ID 不合法", err)
		return
	}
	var ch gbmodels.GbChannel
	res := db.WithContext(c).Where("tenant_id = ? AND id = ?", tid, id).Limit(1).Find(&ch)
	if res.Error != nil {
		dc.FailAndAbort(c, "查询失败", res.Error)
		return
	}
	if res.RowsAffected == 0 {
		dc.FailAndAbort(c, "通道不存在", nil)
		return
	}
	dc.Success(c, ch)
}

// ListChannelMounts 通道挂载位置列表(plan §3.5 多视图挂载)
func (dc *DeviceMgmtController) ListChannelMounts(c *gin.Context) {
	db := dc.db()
	if db == nil {
		dc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	tid := tenantOf(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		dc.FailAndAbort(c, "ID 不合法", err)
		return
	}

	type mountVO struct {
		ID           uint   `json:"id"`
		ParentNodeID uint   `json:"parentNodeId"`
		ParentName   string `json:"parentName"`
		ParentPath   string `json:"parentPath"`
		DisplayName  string `json:"displayName"`
		IsPrimary    bool   `json:"isPrimary"`
		MountSource  string `json:"mountSource"`
	}

	var mounts []gbmodels.GbChannelMount
	if err := db.WithContext(c).
		Where("tenant_id = ? AND channel_id = ?", tid, id).
		Order("is_primary DESC, sort_order, id").
		Find(&mounts).Error; err != nil {
		dc.FailAndAbort(c, "查询挂载失败", err)
		return
	}

	out := make([]mountVO, 0, len(mounts))
	for _, m := range mounts {
		var node gbmodels.GbCatalogNode
		db.WithContext(c).Select("id, name, path").Where("id = ?", m.ParentNodeID).Limit(1).Find(&node)
		out = append(out, mountVO{
			ID:           m.ID,
			ParentNodeID: m.ParentNodeID,
			ParentName:   node.Name,
			ParentPath:   node.Path,
			DisplayName:  m.DisplayName,
			IsPrimary:    m.IsPrimary,
			MountSource:  string(m.MountSource),
		})
	}
	dc.Success(c, gin.H{"list": out, "total": len(out)})
}

// ChannelTimeline 通道 24h 在线时序(Phase 1 简化:仅当前状态点 + last_status_at)
//
// Phase 2 引入真实历史时序表;本期返回 48 个 30min slot 的占位,
// 全部用当前状态填(若通道当前在线 → 48 个 online,否则 offline)。
// 这让前端组件可联调,联调通过后 P2 真实数据无侵入。
func (dc *DeviceMgmtController) ChannelTimeline(c *gin.Context) {
	db := dc.db()
	if db == nil {
		dc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	tid := tenantOf(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		dc.FailAndAbort(c, "ID 不合法", err)
		return
	}
	var ch gbmodels.GbChannel
	res := db.WithContext(c).Where("tenant_id = ? AND id = ?", tid, id).Limit(1).Find(&ch)
	if res.Error != nil {
		dc.FailAndAbort(c, "查询失败", res.Error)
		return
	}
	if res.RowsAffected == 0 {
		dc.FailAndAbort(c, "通道不存在", nil)
		return
	}

	type slot struct {
		Start  time.Time `json:"start"`
		End    time.Time `json:"end"`
		Status string    `json:"status"`
	}
	now := time.Now()
	slots := make([]slot, 0, 48)
	state := "offline"
	if ch.Status == gbmodels.ChannelStatusOnline {
		state = "online"
	}
	for i := 47; i >= 0; i-- {
		start := now.Add(-time.Duration(i+1) * 30 * time.Minute)
		end := start.Add(30 * time.Minute)
		slots = append(slots, slot{Start: start, End: end, Status: state})
	}
	dc.Success(c, gin.H{"slots": slots, "range": "24h", "channelId": id, "phase1Simplified": true})
}
