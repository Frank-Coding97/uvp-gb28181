package controllers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	basecontrollers "uvplatform.cn/uvp-gb28181/app/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/middleware"
	"uvplatform.cn/uvp-gb28181/app/utils/datascope"
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
	return &AlarmController{db: func() *gorm.DB { return app.GormDbMysql }}
}

func (controller *AlarmController) SetDB(provider func() *gorm.DB) {
	controller.db = provider
}

func (controller *AlarmController) List(c *gin.Context) {
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
	err := db.WithContext(c).Transaction(func(tx *gorm.DB) error {
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
	err := db.WithContext(c).Transaction(func(tx *gorm.DB) error {
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
	query := db.WithContext(c).Table("gb_alarm_event AS alarm").
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
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "操作成功", "data": data})
}

func alarmHTTPError(c *gin.Context, status int, errorCode, message string) {
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

func alarmType(method, value *int) alarmEnumValue {
	if value == nil {
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
