package controllers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
	"uvplatform.com/uvp-gb28181/app/utils/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	basecontrollers "uvplatform.com/uvp-gb28181/app/controllers"
	"uvplatform.com/uvp-gb28181/app/gb28181/alarmpopup"
	gbconfig "uvplatform.com/uvp-gb28181/app/gb28181/config"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/middleware"
	"uvplatform.com/uvp-gb28181/app/utils/datascope"
)

const maxAlarmDeleteBatch = 100

var (
	errAlarmNotFound       = errors.New("alarm not found")
	errAlarmDeleteConflict = errors.New("alarm delete conflict")
)

type alarmListItem struct {
	ID          string               `json:"id"`
	ReceivedAt  time.Time            `json:"receivedAt"`
	AlarmTime   *time.Time           `json:"alarmTime"`
	Device      alarmDeviceSummary   `json:"device"`
	Channel     *alarmChannelSummary `json:"channel"`
	SourceCode  string               `json:"sourceCode"`
	Priority    alarmEnumValue       `json:"priority"`
	Method      alarmEnumValue       `json:"method"`
	AlarmType   alarmEnumValue       `json:"alarmType"`
	Description string               `json:"description"`
}

type alarmDeviceSummary struct {
	ID    uint   `json:"id"`
	Code  string `json:"code"`
	Name  string `json:"name"`
	Alias string `json:"alias"`
}

type alarmChannelSummary struct {
	ID    uint   `json:"id"`
	Code  string `json:"code"`
	Name  string `json:"name"`
	Alias string `json:"alias"`
}

type alarmDetail struct {
	alarmListItem
	AlarmTypeParam string    `json:"alarmTypeParam"`
	Longitude      *float64  `json:"longitude"`
	Latitude       *float64  `json:"latitude"`
	RawDigest      string    `json:"rawDigest"`
	RawSummary     string    `json:"rawSummary"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type alarmQuery struct {
	Page       int
	PageSize   int
	DeviceID   *uint64
	SourceCode string
	AlarmFrom  *time.Time
	AlarmTo    *time.Time
	Priority   *int
	Method     *int
	AlarmType  *int
	Keyword    string
}

type alarmRow struct {
	ID             uint64
	DeviceID       uint
	DeviceCode     string
	DeviceName     string
	DeviceAlias    string
	ChannelID      *uint
	ChannelCode    *string
	ChannelName    *string
	ChannelAlias   *string
	SourceCode     string
	AlarmTime      *time.Time
	Priority       *int
	Method         *int
	AlarmType      *int
	AlarmTypeParam string
	Description    string
	Longitude      *float64
	Latitude       *float64
	RawDigest      string
	RawSummary     string
	ReceivedAt     time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type AlarmController struct {
	basecontrollers.Common
	db func() *gorm.DB
}

func NewAlarmController() *AlarmController {
	// ⛔ 用app.DB()（按 usedbtype 分发到 MySQL/PG/SQLServer/SQLite），
	//   不能写死 app.GormDbMysql：那样在绿色包的 SQLite 模式下拿到 nil，
	//   设备/告警/地图等页面一律报「DB 未就绪」，而日志里看不出原因。
	return &AlarmController{db: func() *gorm.DB { return app.DB() }}
}

func (controller *AlarmController) SetDB(provider func() *gorm.DB) {
	controller.db = provider
}

// List 查询告警列表。
//
// ⛔⛔ 实时弹窗的 SSE **复用本路径**（`?stream=1`），不是另开 `/alarms/stream`：
// casbin 用 `c.Request.URL.Path` 精确匹配策略，新增一条路由就要同步补
// sys_api + sys_menu_api + casbin 三处，漏一处线上立刻 403
// （`topics/api-permission-governance.md` 的头号坑）。走查询参数分流时
// URL.Path 不变 ⇒ 权限语义直接继承「查询告警列表」，不需要任何库改动。
func (controller *AlarmController) List(c *gin.Context) {
	if c.Query("stream") == "1" {
		controller.Stream(c)
		return
	}
	db := controller.database()
	if db == nil {
		alarmHTTPError(c, http.StatusServiceUnavailable, "ALARM_DB_UNAVAILABLE", "告警服务未就绪")
		return
	}
	queryParams, err := parseAlarmQuery(c)
	if err != nil {
		alarmHTTPError(c, http.StatusBadRequest, "INVALID_ALARM_QUERY", err.Error())
		return
	}
	var total int64
	if err := controller.scopedQuery(c, db, queryParams).Count(&total).Error; err != nil {
		alarmHTTPError(c, http.StatusInternalServerError, "ALARM_QUERY_FAILED", "查询告警失败")
		return
	}
	rows := make([]alarmRow, 0)
	selectColumns := strings.Join([]string{
		"alarm.id", "alarm.received_at", "alarm.alarm_time", "alarm.source_code",
		"alarm.priority", "alarm.method", "alarm.alarm_type", "alarm.description",
		"device.id AS device_id", "device.device_id AS device_code", "device.name AS device_name", "device.alias AS device_alias",
		"channel.id AS channel_id", "channel.channel_id AS channel_code", "channel.name AS channel_name", "channel.alias AS channel_alias",
	}, ", ")
	if err := controller.scopedQuery(c, db, queryParams).
		Select(selectColumns).
		Order("alarm.received_at DESC, alarm.id DESC").
		Offset((queryParams.Page - 1) * queryParams.PageSize).
		Limit(queryParams.PageSize).
		Scan(&rows).Error; err != nil {
		alarmHTTPError(c, http.StatusInternalServerError, "ALARM_QUERY_FAILED", "查询告警失败")
		return
	}
	list := make([]alarmListItem, 0, len(rows))
	for _, row := range rows {
		list = append(list, row.listItem())
	}
	alarmHTTPSuccess(c, gin.H{"list": list, "total": total, "page": queryParams.Page, "pageSize": queryParams.PageSize})
}

// alarmStreamPingInterval 是 SSE 保活间隔。反代（nginx）默认 60s 无数据就断，
// 25s 一次 ping 留足余量；与 dashboard/trace 两条既有 SSE 同一口径。
const alarmStreamPingInterval = 25 * time.Second

// Stream 把收到的设备报警实时推给当前用户（`event: alarm`）。
//
// 与「是否存储报警消息」无关：处理器在报文解析处广播，所以关闭落库时照样能弹。
func (controller *AlarmController) Stream(c *gin.Context) {
	db := controller.database()
	if db == nil {
		alarmHTTPError(c, http.StatusServiceUnavailable, "ALARM_DB_UNAVAILABLE", "告警服务未就绪")
		return
	}
	events, unsubscribe := alarmpopup.Subscribe()
	// ⛔ 必须 defer：否则连接断了通道还挂在 hub 里，每条报警都往死通道写。
	defer unsubscribe()

	header := c.Writer.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no") // nginx 不缓冲，否则要攒满才吐

	// 首帧先把开关状态送出去：关着时前端可以明确提示"已在服务配置里关闭"，
	// 而不是让用户对着一个永远没消息的连接猜。
	fmt.Fprintf(c.Writer, "event: ready\ndata: {\"enabled\":%t}\n\n", gbconfig.AlarmPopupEnabled())
	c.Writer.Flush()

	// 可见部门集合在连接建立时算一次：告警是低频事件，但每条都重算要打
	// 3~4 次角色/部门表。设备共享授权（gb_device_grant）仍按条走完整判据。
	deptIDs, filterByDept := datascope.GetOwnerDeptIDsWithDB(c, db)
	allowedDept := make(map[uint]bool, len(deptIDs))
	for _, id := range deptIDs {
		allowedDept[id] = true
	}

	ping := time.NewTicker(alarmStreamPingInterval)
	defer ping.Stop()
	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			if _, err := fmt.Fprint(c.Writer, "event: ping\ndata: {}\n\n"); err != nil {
				return
			}
			c.Writer.Flush()
		case event, ok := <-events:
			if !ok {
				return
			}
			if !controller.alarmEventVisible(c, db, event, filterByDept, allowedDept) {
				continue
			}
			payload, err := json.Marshal(event)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(c.Writer, "event: alarm\ndata: %s\n\n", payload); err != nil {
				return
			}
			c.Writer.Flush()
		}
	}
}

// alarmEventVisible 判断这条报警能不能推给当前连接的用户。
//
// 先走零查询的快路径（归属部门命中），没命中再按 `visibleScope` 查一次共享授权，
// 与告警列表页完全同一套口径。⛔ 取不到设备主键时一律不推（fail-closed）。
func (controller *AlarmController) alarmEventVisible(c *gin.Context, db *gorm.DB, event alarmpopup.Event, filterByDept bool, allowedDept map[uint]bool) bool {
	// 无 claims / 全量权限（notcheckuser）时 datascope 明确表示"不必过滤"。
	if !filterByDept {
		return true
	}
	if event.DeviceDBID == 0 {
		return false
	}
	if event.OwnerDeptID != 0 && allowedDept[event.OwnerDeptID] {
		return true
	}
	var count int64
	err := db.WithContext(c.Request.Context()).Model(&gbmodels.GbDevice{}).
		Where("id = ?", event.DeviceDBID).
		Scopes(visibleScope(c)).
		Count(&count).Error
	return err == nil && count > 0
}

func (controller *AlarmController) Detail(c *gin.Context) {
	db := controller.database()
	if db == nil {
		alarmHTTPError(c, http.StatusServiceUnavailable, "ALARM_DB_UNAVAILABLE", "告警服务未就绪")
		return
	}
	id, err := parseAlarmID(c.Param("id"))
	if err != nil {
		alarmHTTPError(c, http.StatusBadRequest, "INVALID_ALARM_ID", err.Error())
		return
	}
	var row alarmRow
	result := controller.scopedQuery(c, db, alarmQuery{Page: 1, PageSize: 20}).
		Where("alarm.id = ?", id).
		Select(strings.Join([]string{
			"alarm.id", "alarm.received_at", "alarm.alarm_time", "alarm.source_code",
			"alarm.priority", "alarm.method", "alarm.alarm_type", "alarm.alarm_type_param", "alarm.description",
			"alarm.longitude", "alarm.latitude", "alarm.raw_digest", "alarm.raw_summary", "alarm.created_at", "alarm.updated_at",
			"device.id AS device_id", "device.device_id AS device_code", "device.name AS device_name", "device.alias AS device_alias",
			"channel.id AS channel_id", "channel.channel_id AS channel_code", "channel.name AS channel_name", "channel.alias AS channel_alias",
		}, ", ")).
		Limit(1).Scan(&row)
	if result.Error != nil {
		alarmHTTPError(c, http.StatusInternalServerError, "ALARM_QUERY_FAILED", "查询告警详情失败")
		return
	}
	if result.RowsAffected == 0 {
		alarmHTTPError(c, http.StatusNotFound, "ALARM_NOT_FOUND", "告警不存在或无权访问")
		return
	}
	item := row.listItem()
	alarmHTTPSuccess(c, alarmDetail{
		alarmListItem: item, AlarmTypeParam: row.AlarmTypeParam,
		Longitude: row.Longitude, Latitude: row.Latitude, RawDigest: row.RawDigest,
		RawSummary: row.RawSummary, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	})
}

func (controller *AlarmController) Delete(c *gin.Context) {
	id, err := parseAlarmID(c.Param("id"))
	if err != nil {
		alarmHTTPError(c, http.StatusBadRequest, "INVALID_ALARM_ID", err.Error())
		return
	}
	controller.deleteAndRespond(c, []uint64{id}, []string{formatAlarmID(id)})
}

func (controller *AlarmController) BatchDelete(c *gin.Context) {
	middleware.MarkDeleteOperation(c)
	var request struct {
		IDs []string `json:"ids"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		alarmHTTPError(c, http.StatusBadRequest, "INVALID_ALARM_IDS", "ids 必须是字符串数组")
		return
	}
	ids, normalized, err := normalizeAlarmIDs(request.IDs)
	if err != nil {
		alarmHTTPError(c, http.StatusBadRequest, "INVALID_ALARM_IDS", err.Error())
		return
	}
	controller.deleteAndRespond(c, ids, normalized)
}

func (controller *AlarmController) ClearAll(c *gin.Context) {
	middleware.MarkDeleteOperation(c)
	db := controller.database()
	if db == nil {
		alarmHTTPError(c, http.StatusServiceUnavailable, "ALARM_DB_UNAVAILABLE", "告警服务未就绪")
		return
	}
	var deletedCount int64
	err := db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		deviceScope := tx.Model(&gbmodels.GbDevice{}).
			Select("id").
			Scopes(datascope.OwnerDeptScopeWithDB(c, tx, "owner_dept_id"))
		result := tx.Where("device_id IN (?)", deviceScope).
			Delete(&gbmodels.GbAlarmEvent{})
		if result.Error != nil {
			return result.Error
		}
		deletedCount = result.RowsAffected
		return nil
	})
	if err != nil {
		alarmHTTPError(c, http.StatusInternalServerError, "ALARM_DELETE_FAILED", "清空告警失败")
		return
	}
	alarmHTTPSuccess(c, gin.H{"deletedCount": deletedCount})
}

func (controller *AlarmController) deleteAndRespond(c *gin.Context, ids []uint64, normalized []string) {
	db := controller.database()
	if db == nil {
		alarmHTTPError(c, http.StatusServiceUnavailable, "ALARM_DB_UNAVAILABLE", "告警服务未就绪")
		return
	}
	err := db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var visible int64
		if err := controller.scopedQuery(c, tx, alarmQuery{Page: 1, PageSize: 20}).Where("alarm.id IN ?", ids).Count(&visible).Error; err != nil {
			return err
		}
		if visible != int64(len(ids)) {
			return errAlarmNotFound
		}
		deviceScope := tx.Model(&gbmodels.GbDevice{}).
			Select("id").
			Scopes(datascope.OwnerDeptScopeWithDB(c, tx, "owner_dept_id"))
		result := tx.Where("id IN ?", ids).
			Where("device_id IN (?)", deviceScope).
			Delete(&gbmodels.GbAlarmEvent{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != int64(len(ids)) {
			return errAlarmDeleteConflict
		}
		return nil
	})
	if err != nil {
		switch {
		case errors.Is(err, errAlarmNotFound):
			alarmHTTPError(c, http.StatusNotFound, "ALARM_NOT_FOUND", "告警不存在或无权访问")
		case errors.Is(err, errAlarmDeleteConflict):
			alarmHTTPError(c, http.StatusConflict, "ALARM_DELETE_CONFLICT", "告警已发生变化，未删除任何记录")
		default:
			alarmHTTPError(c, http.StatusInternalServerError, "ALARM_DELETE_FAILED", "删除告警失败")
		}
		return
	}
	alarmHTTPSuccess(c, gin.H{"deletedIds": normalized, "deletedCount": len(normalized)})
}

func (controller *AlarmController) database() *gorm.DB {
	if controller == nil || controller.db == nil {
		return nil
	}
	return controller.db()
}

func (controller *AlarmController) scopedQuery(c *gin.Context, db *gorm.DB, queryParams alarmQuery) *gorm.DB {
	query := db.WithContext(c.Request.Context()).Table("gb_alarm_event AS alarm").
		Joins("JOIN gb_device AS device ON device.id = alarm.device_id AND device.deleted_at IS NULL").
		Joins("LEFT JOIN gb_channel AS channel ON channel.id = alarm.channel_id").
		Scopes(datascope.VisibilityScopeWithDB(c, db, "device.owner_dept_id", "device.device_id"))
	if queryParams.DeviceID != nil {
		query = query.Where("alarm.device_id = ?", *queryParams.DeviceID)
	}
	if queryParams.SourceCode != "" {
		query = query.Where("alarm.source_code LIKE ? ESCAPE '\\'", containsLikePattern(queryParams.SourceCode))
	}
	if queryParams.AlarmFrom != nil {
		query = query.Where("alarm.alarm_time >= ? AND alarm.alarm_time <= ?", *queryParams.AlarmFrom, *queryParams.AlarmTo)
	}
	if queryParams.Priority != nil {
		query = query.Where("alarm.priority = ?", *queryParams.Priority)
	}
	if queryParams.Method != nil {
		query = query.Where("alarm.method = ?", *queryParams.Method)
	}
	if queryParams.AlarmType != nil {
		query = query.Where("alarm.alarm_type = ?", *queryParams.AlarmType)
	}
	if queryParams.Keyword != "" {
		pattern := containsLikePattern(queryParams.Keyword)
		query = query.Where(`(
			alarm.description LIKE ? ESCAPE '\' OR alarm.source_code LIKE ? ESCAPE '\' OR
			device.device_id LIKE ? ESCAPE '\' OR device.name LIKE ? ESCAPE '\' OR device.alias LIKE ? ESCAPE '\' OR
			channel.channel_id LIKE ? ESCAPE '\' OR channel.name LIKE ? ESCAPE '\' OR channel.alias LIKE ? ESCAPE '\'
		)`, pattern, pattern, pattern, pattern, pattern, pattern, pattern, pattern)
	}
	return query
}

func parseAlarmQuery(c *gin.Context) (alarmQuery, error) {
	query := alarmQuery{Page: 1, PageSize: 20}
	var err error
	if raw := c.Query("page"); raw != "" {
		query.Page, err = strconv.Atoi(raw)
		if err != nil || query.Page < 1 {
			return query, fmt.Errorf("page 必须是正整数")
		}
	}
	if raw := c.Query("pageSize"); raw != "" {
		query.PageSize, err = strconv.Atoi(raw)
		if err != nil || !validAlarmPageSize(query.PageSize) {
			return query, fmt.Errorf("pageSize 仅支持 10、20、50、100")
		}
	}
	if raw := c.Query("deviceId"); raw != "" {
		value, parseErr := strconv.ParseUint(raw, 10, 64)
		if parseErr != nil || value == 0 {
			return query, fmt.Errorf("deviceId 必须是正整数")
		}
		query.DeviceID = &value
	}
	query.SourceCode = strings.TrimSpace(c.Query("sourceCode"))
	if utf8.RuneCountInString(query.SourceCode) > 20 {
		return query, fmt.Errorf("sourceCode 最多 20 个字符")
	}
	query.Keyword = strings.TrimSpace(c.Query("keyword"))
	if utf8.RuneCountInString(query.Keyword) > 100 {
		return query, fmt.Errorf("keyword 最多 100 个字符")
	}
	query.AlarmFrom, query.AlarmTo, err = parseAlarmTimeRange(c.Query("alarmFrom"), c.Query("alarmTo"))
	if err != nil {
		return query, err
	}
	if query.Priority, err = parseOptionalAlarmInt(c.Query("priority"), "priority"); err != nil {
		return query, err
	}
	if query.Method, err = parseOptionalAlarmInt(c.Query("method"), "method"); err != nil {
		return query, err
	}
	if query.AlarmType, err = parseOptionalAlarmInt(c.Query("alarmType"), "alarmType"); err != nil {
		return query, err
	}
	return query, nil
}

func parseOptionalAlarmInt(raw, name string) (*int, error) {
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return nil, fmt.Errorf("%s 必须是整数", name)
	}
	return &value, nil
}

func validAlarmPageSize(value int) bool {
	return value == 10 || value == 20 || value == 50 || value == 100
}

func (row alarmRow) listItem() alarmListItem {
	item := alarmListItem{
		ID: formatAlarmID(row.ID), ReceivedAt: row.ReceivedAt, AlarmTime: row.AlarmTime,
		Device:     alarmDeviceSummary{ID: row.DeviceID, Code: row.DeviceCode, Name: row.DeviceName, Alias: row.DeviceAlias},
		SourceCode: row.SourceCode, Priority: alarmPriority(row.Priority), Method: alarmMethod(row.Method),
		AlarmType: alarmType(row.Method, row.AlarmType), Description: row.Description,
	}
	if row.ChannelID != nil {
		item.Channel = &alarmChannelSummary{ID: *row.ChannelID, Code: derefString(row.ChannelCode), Name: derefString(row.ChannelName), Alias: derefString(row.ChannelAlias)}
	}
	return item
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func alarmHTTPSuccess(c *gin.Context, data any) {
	response.SetBusinessResult(c, 0, true)
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "操作成功", "data": data})
}

func alarmHTTPError(c *gin.Context, status int, errorCode, message string) {
	response.SetBusinessResult(c, status, status == 0)
	c.JSON(status, gin.H{"code": status, "message": message, "data": gin.H{"errorCode": errorCode}})
}

type alarmEnumValue struct {
	Value *int   `json:"value"`
	Label string `json:"label"`
}

func formatAlarmID(id uint64) string {
	return strconv.FormatUint(id, 10)
}

func parseAlarmID(raw string) (uint64, error) {
	if raw == "" || strings.TrimSpace(raw) != raw {
		return 0, fmt.Errorf("告警 ID 必须是十进制正整数")
	}
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("告警 ID 必须是十进制正整数")
	}
	return id, nil
}

func normalizeAlarmIDs(rawIDs []string) ([]uint64, []string, error) {
	if len(rawIDs) == 0 || len(rawIDs) > maxAlarmDeleteBatch {
		return nil, nil, fmt.Errorf("告警 ID 数量必须在 1-%d 之间", maxAlarmDeleteBatch)
	}
	values := make([]uint64, 0, len(rawIDs))
	stringsOut := make([]string, 0, len(rawIDs))
	seen := make(map[uint64]struct{}, len(rawIDs))
	for _, raw := range rawIDs {
		id, err := parseAlarmID(raw)
		if err != nil {
			return nil, nil, err
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		values = append(values, id)
		stringsOut = append(stringsOut, formatAlarmID(id))
	}
	return values, stringsOut, nil
}

func alarmPriority(value *int) alarmEnumValue {
	return alarmEnum(value, map[int]string{
		1: "一级警情",
		2: "二级警情",
		3: "三级警情",
		4: "四级警情",
	})
}

func alarmMethod(value *int) alarmEnumValue {
	return alarmEnum(value, map[int]string{
		1: "电话报警",
		2: "设备报警",
		3: "短信报警",
		4: "GPS报警",
		5: "视频报警",
		6: "设备故障报警",
		7: "其他报警",
	})
}

// alarmMethodHasTypeTable reports whether GB/T 28181 定义了该报警方式的 AlarmType 取值表。
// 方式 1/3/4/7 没有类型表，报文里本来就不该出现 AlarmType。
func alarmMethodHasTypeTable(method int) bool {
	switch method {
	case 2, 5, 6:
		return true
	default:
		return false
	}
}

func alarmType(method, value *int) alarmEnumValue {
	if value == nil {
		// 方式本身就没有类型表时，空类型是「不适用」而不是「未知」。
		// 例如方式 7（人工报警）按标准不携带 AlarmType，标成「未知」会让人以为平台丢字段。
		if method != nil && !alarmMethodHasTypeTable(*method) {
			return alarmEnumValue{Label: "—"}
		}
		return alarmEnumValue{Label: "未知"}
	}
	labels := map[int]string(nil)
	if method != nil {
		switch *method {
		case 2:
			labels = map[int]string{
				1: "视频丢失报警",
				2: "设备防拆报警",
				3: "存储设备磁盘满报警",
				4: "设备高温报警",
				5: "设备低温报警",
			}
		case 5:
			labels = map[int]string{
				1:  "人工视频报警",
				2:  "运动目标检测报警",
				3:  "遗留物检测报警",
				4:  "物体移除检测报警",
				5:  "绊线检测报警",
				6:  "入侵检测报警",
				7:  "逆行检测报警",
				8:  "徘徊检测报警",
				9:  "流量统计报警",
				10: "密度检测报警",
				11: "视频异常检测报警",
				12: "快速移动报警",
				13: "图像遮挡报警（2022）",
			}
		case 6:
			labels = map[int]string{
				1: "存储设备磁盘故障报警",
				2: "存储设备风扇故障报警",
			}
		}
	}
	return alarmEnum(value, labels)
}

func alarmEnum(value *int, labels map[int]string) alarmEnumValue {
	if value == nil {
		return alarmEnumValue{Label: "未知"}
	}
	label, exists := labels[*value]
	if !exists {
		label = fmt.Sprintf("未知(%d)", *value)
	}
	return alarmEnumValue{Value: value, Label: label}
}

func parseAlarmTimeRange(rawFrom, rawTo string) (*time.Time, *time.Time, error) {
	if rawFrom == "" && rawTo == "" {
		return nil, nil, nil
	}
	if rawFrom == "" || rawTo == "" {
		return nil, nil, fmt.Errorf("告警时间范围必须同时提供起止时间")
	}
	from, err := time.Parse(time.RFC3339, rawFrom)
	if err != nil {
		return nil, nil, fmt.Errorf("告警开始时间必须是 RFC3339")
	}
	to, err := time.Parse(time.RFC3339, rawTo)
	if err != nil {
		return nil, nil, fmt.Errorf("告警结束时间必须是 RFC3339")
	}
	if from.After(to) {
		return nil, nil, fmt.Errorf("告警开始时间不能晚于结束时间")
	}
	return &from, &to, nil
}

func containsLikePattern(value string) string {
	escaped := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(value)
	return "%" + escaped + "%"
}
