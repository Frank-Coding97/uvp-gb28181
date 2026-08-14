package controllers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
)

func TestAlarmIDContract(t *testing.T) {
	const largeID = uint64(9007199254740993)
	require.Equal(t, "9007199254740993", formatAlarmID(largeID))

	encoded, err := json.Marshal(alarmListItem{ID: formatAlarmID(largeID)})
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"id":"9007199254740993"`)

	parsed, err := parseAlarmID("2006")
	require.NoError(t, err)
	require.Equal(t, uint64(2006), parsed)

	for _, raw := range []string{"", "0", "-1", "1.5", "1e3", " 1 ", "18446744073709551616"} {
		_, err := parseAlarmID(raw)
		require.Error(t, err, raw)
	}
}

func TestAlarmIDBatchNormalization(t *testing.T) {
	values, stringsOut, err := normalizeAlarmIDs([]string{"3", "1", "3", "2"})
	require.NoError(t, err)
	require.Equal(t, []uint64{3, 1, 2}, values)
	require.Equal(t, []string{"3", "1", "2"}, stringsOut)

	_, _, err = normalizeAlarmIDs(nil)
	require.Error(t, err)

	tooMany := make([]string, maxAlarmDeleteBatch+1)
	for i := range tooMany {
		tooMany[i] = formatAlarmID(uint64(i + 1))
	}
	_, _, err = normalizeAlarmIDs(tooMany)
	require.Error(t, err)
}

func TestAlarmEnumLabels(t *testing.T) {
	require.Equal(t, alarmEnumValue{Value: nil, Label: "未知"}, alarmPriority(nil))
	for value, label := range map[int]string{1: "一级警情", 2: "二级警情", 3: "三级警情", 4: "四级警情"} {
		value := value
		require.Equal(t, alarmEnumValue{Value: &value, Label: label}, alarmPriority(&value))
	}
	unknown := 99
	require.Equal(t, "未知(99)", alarmPriority(&unknown).Label)

	for value, label := range map[int]string{
		1: "电话报警", 2: "设备报警", 3: "短信报警", 4: "GPS报警",
		5: "视频报警", 6: "设备故障报警", 7: "其他报警",
	} {
		value := value
		require.Equal(t, label, alarmMethod(&value).Label)
	}
	require.Equal(t, "未知(99)", alarmMethod(&unknown).Label)

	methodDevice, methodVideo, methodFault := 2, 5, 6
	typeOne, typeThirteen := 1, 13
	require.Equal(t, "视频丢失报警", alarmType(&methodDevice, &typeOne).Label)
	require.Equal(t, "人工视频报警", alarmType(&methodVideo, &typeOne).Label)
	require.Equal(t, "存储设备磁盘故障报警", alarmType(&methodFault, &typeOne).Label)
	require.Equal(t, "图像遮挡报警（2022）", alarmType(&methodVideo, &typeThirteen).Label)
	require.Equal(t, "未知(13)", alarmType(&methodDevice, &typeThirteen).Label)
	require.Equal(t, "未知", alarmType(nil, nil).Label)
}

func TestAlarmQueryValidation(t *testing.T) {
	from := "2026-08-04T10:00:00+08:00"
	to := "2026-08-04T11:00:00+08:00"
	fromTime, toTime, err := parseAlarmTimeRange(from, to)
	require.NoError(t, err)
	require.True(t, fromTime.Before(*toTime))

	for _, input := range [][2]string{{from, ""}, {"", to}, {"bad", to}, {to, from}} {
		_, _, err := parseAlarmTimeRange(input[0], input[1])
		require.Error(t, err, input)
	}
	fromTime, toTime, err = parseAlarmTimeRange("", "")
	require.NoError(t, err)
	require.Nil(t, fromTime)
	require.Nil(t, toTime)

	require.Equal(t, `%100\%\_\\中文%`, containsLikePattern(`100%_\中文`))
	require.Equal(t, `%normal%`, containsLikePattern("normal"))
}

type alarmHTTPFixture struct {
	router     *gin.Engine
	db         *gorm.DB
	deviceA    *gbmodels.GbDevice
	deviceB    *gbmodels.GbDevice
	channelA   *gbmodels.GbChannel
	alarmOld   *gbmodels.GbAlarmEvent
	alarmNew   *gbmodels.GbAlarmEvent
	alarmOther *gbmodels.GbAlarmEvent
}

func newAlarmHTTPFixture(t *testing.T, scoped bool) *alarmHTTPFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbAlarmEvent{}, &gbmodels.GbDeviceSubscription{},
		&basemodels.SysDepartment{}, &basemodels.SysRole{}, &basemodels.SysUserRole{}, &basemodels.User{},
	))

	deviceA := &gbmodels.GbDevice{DeviceID: "37010301021320000111", Name: "一号 NVR", Alias: "机房一号", OwnerDeptID: 10}
	deviceB := &gbmodels.GbDevice{DeviceID: "37010301021320000222", Name: "二号 NVR", OwnerDeptID: 20}
	require.NoError(t, db.Create(deviceA).Error)
	require.NoError(t, db.Create(deviceB).Error)
	channelA := &gbmodels.GbChannel{DeviceID: deviceA.DeviceID, ChannelID: "37010301021320000112", Name: "东门", Alias: "东门主通道"}
	require.NoError(t, db.Create(channelA).Error)

	baseTime := time.Date(2026, 8, 4, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	priority, method, alarmType := 2, 5, 2
	oldAlarmTime := baseTime.Add(-time.Hour)
	newAlarmTime := baseTime.Add(time.Minute)
	alarmOld := &gbmodels.GbAlarmEvent{ID: 9007199254740993, DeviceID: deviceA.ID, ChannelID: &channelA.ID, SourceCode: channelA.ChannelID, AlarmTime: &oldAlarmTime, Priority: &priority, Method: &method, AlarmType: &alarmType, Description: "旧移动目标", DedupeKey: "alarm-old", ReceivedAt: baseTime}
	alarmNew := &gbmodels.GbAlarmEvent{ID: 9007199254740994, DeviceID: deviceA.ID, ChannelID: &channelA.ID, SourceCode: channelA.ChannelID, AlarmTime: &newAlarmTime, Priority: &priority, Method: &method, AlarmType: &alarmType, AlarmTypeParam: "EventType=1", Description: "新移动目标", Longitude: float64Ptr(117.1), Latitude: float64Ptr(36.6), DedupeKey: "alarm-new", RawDigest: "digest", RawSummary: "<Notify>safe text</Notify>", ReceivedAt: baseTime.Add(time.Minute)}
	alarmOther := &gbmodels.GbAlarmEvent{ID: 9007199254740995, DeviceID: deviceB.ID, SourceCode: deviceB.DeviceID, Description: "外部门告警", DedupeKey: "alarm-other", ReceivedAt: baseTime.Add(2 * time.Minute)}
	require.NoError(t, db.Create(alarmOld).Error)
	require.NoError(t, db.Create(alarmNew).Error)
	require.NoError(t, db.Create(alarmOther).Error)

	controller := NewAlarmController()
	controller.SetDB(func() *gorm.DB { return db })
	router := gin.New()
	if scoped {
		seedAlarmDeptUser(t, db, 7, 10)
		router.Use(func(c *gin.Context) {
			c.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 7}})
			c.Next()
		})
	}
	router.GET("/api/gb28181/alarms", controller.List)
	router.GET("/api/gb28181/alarms/:id", controller.Detail)
	router.DELETE("/api/gb28181/alarms/:id", controller.Delete)
	router.POST("/api/gb28181/alarms/batch-delete", controller.BatchDelete)
	router.POST("/api/gb28181/alarms/clear-all", controller.ClearAll)
	return &alarmHTTPFixture{router: router, db: db, deviceA: deviceA, deviceB: deviceB, channelA: channelA, alarmOld: alarmOld, alarmNew: alarmNew, alarmOther: alarmOther}
}

func TestAlarmListDefaultOrderProjectionAndStringID(t *testing.T) {
	fixture := newAlarmHTTPFixture(t, false)
	response := alarmRequest(t, fixture.router, http.MethodGet, "/api/gb28181/alarms?page=1&pageSize=20")
	require.EqualValues(t, 3, response["data"].(map[string]any)["total"])
	list := response["data"].(map[string]any)["list"].([]any)
	require.Equal(t, "9007199254740995", list[0].(map[string]any)["id"])
	require.Equal(t, "9007199254740994", list[1].(map[string]any)["id"])
	require.NotContains(t, list[1].(map[string]any), "rawSummary")
	require.NotContains(t, list[1].(map[string]any), "longitude")
	require.Equal(t, "运动目标检测报警", list[1].(map[string]any)["alarmType"].(map[string]any)["label"])
}

func TestAlarmListFiltersAndCountShareConditions(t *testing.T) {
	fixture := newAlarmHTTPFixture(t, false)
	path := "/api/gb28181/alarms?deviceId=" + formatUint(uint64(fixture.deviceA.ID)) + "&sourceCode=112&priority=2&method=5&alarmType=2&keyword=%25_%5C%E6%96%B0"
	response := alarmRequest(t, fixture.router, http.MethodGet, path)
	data := response["data"].(map[string]any)
	require.EqualValues(t, 0, data["total"], "LIKE 特殊字符必须按字面匹配，不得扩大结果")

	response = alarmRequest(t, fixture.router, http.MethodGet, "/api/gb28181/alarms?deviceId="+formatUint(uint64(fixture.deviceA.ID))+"&keyword=%E6%96%B0")
	data = response["data"].(map[string]any)
	require.EqualValues(t, 1, data["total"])
	require.Len(t, data["list"].([]any), 1)
}

func TestAlarmListKeywordSearchesDeviceChannelAndDescription(t *testing.T) {
	fixture := newAlarmHTTPFixture(t, false)
	for _, test := range []struct {
		name    string
		keyword string
		total   int
	}{
		{name: "device name", keyword: "一号 NVR", total: 2},
		{name: "device code", keyword: fixture.deviceA.DeviceID, total: 2},
		{name: "channel name", keyword: "东门", total: 2},
		{name: "channel code", keyword: fixture.channelA.ChannelID, total: 2},
		{name: "description", keyword: "新移动目标", total: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := alarmRequest(t, fixture.router, http.MethodGet, "/api/gb28181/alarms?keyword="+url.QueryEscape(test.keyword))
			data := response["data"].(map[string]any)
			require.EqualValues(t, test.total, data["total"])
		})
	}
}

func TestAlarmListAndDetailAreDepartmentScoped(t *testing.T) {
	fixture := newAlarmHTTPFixture(t, true)
	response := alarmRequest(t, fixture.router, http.MethodGet, "/api/gb28181/alarms")
	data := response["data"].(map[string]any)
	require.EqualValues(t, 2, data["total"])

	response = alarmRequest(t, fixture.router, http.MethodGet, "/api/gb28181/alarms/"+formatAlarmID(fixture.alarmNew.ID))
	detail := response["data"].(map[string]any)
	require.Equal(t, "digest", detail["rawDigest"])
	require.Equal(t, "<Notify>safe text</Notify>", detail["rawSummary"])
	require.Equal(t, "机房一号", detail["device"].(map[string]any)["alias"])

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/gb28181/alarms/"+formatAlarmID(fixture.alarmOther.ID), nil)
	fixture.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "ALARM_NOT_FOUND", body["data"].(map[string]any)["errorCode"])
}

func TestAlarmDeletePhysicallyRemovesRowAndKeepsSubscription(t *testing.T) {
	fixture := newAlarmHTTPFixture(t, true)
	subscription := &gbmodels.GbDeviceSubscription{DeviceID: fixture.deviceA.ID, Kind: gbmodels.SubscriptionKindAlarm, Enabled: true, Status: gbmodels.SubscriptionStatusActive, Event: "Alarm", ExpiresSeconds: 3600}
	require.NoError(t, fixture.db.Create(subscription).Error)

	response := alarmRequest(t, fixture.router, http.MethodDelete, "/api/gb28181/alarms/"+formatAlarmID(fixture.alarmNew.ID))
	data := response["data"].(map[string]any)
	require.EqualValues(t, 1, data["deletedCount"])
	require.Equal(t, []any{formatAlarmID(fixture.alarmNew.ID)}, data["deletedIds"])

	var count int64
	require.NoError(t, fixture.db.Unscoped().Model(&gbmodels.GbAlarmEvent{}).Where("id = ?", fixture.alarmNew.ID).Count(&count).Error)
	require.Zero(t, count, "告警必须物理删除，不得留下软删除行")
	var current gbmodels.GbDeviceSubscription
	require.NoError(t, fixture.db.First(&current, subscription.ID).Error)
	require.True(t, current.Enabled)
	require.Equal(t, gbmodels.SubscriptionStatusActive, current.Status)

	replayed := *fixture.alarmNew
	replayed.ID = 0
	replayed.CreatedAt = time.Time{}
	replayed.UpdatedAt = time.Time{}
	require.NoError(t, fixture.db.Create(&replayed).Error, "物理删除 dedupe_key 后允许同通知迟到重传重新入库")
}

func TestAlarmBatchDeleteIsAtomicForUnauthorizedOrMissingIDs(t *testing.T) {
	fixture := newAlarmHTTPFixture(t, true)
	request := []string{formatAlarmID(fixture.alarmOld.ID), formatAlarmID(fixture.alarmOther.ID)}
	w := alarmJSONRequest(t, fixture.router, http.MethodPost, "/api/gb28181/alarms/batch-delete", gin.H{"ids": request})
	require.Equal(t, http.StatusNotFound, w.Code)
	require.EqualValues(t, 3, countAlarmRows(t, fixture.db), "混入外部门 ID 时一条也不能删除")

	request = []string{formatAlarmID(fixture.alarmOld.ID), "999999999999"}
	w = alarmJSONRequest(t, fixture.router, http.MethodPost, "/api/gb28181/alarms/batch-delete", gin.H{"ids": request})
	require.Equal(t, http.StatusNotFound, w.Code)
	require.EqualValues(t, 3, countAlarmRows(t, fixture.db), "混入不存在 ID 时一条也不能删除")
}

func TestAlarmBatchDeleteDeduplicatesAndDeletesInOneTransaction(t *testing.T) {
	fixture := newAlarmHTTPFixture(t, true)
	request := []string{formatAlarmID(fixture.alarmOld.ID), formatAlarmID(fixture.alarmOld.ID), formatAlarmID(fixture.alarmNew.ID)}
	w := alarmJSONRequest(t, fixture.router, http.MethodPost, "/api/gb28181/alarms/batch-delete", gin.H{"ids": request})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var response map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	data := response["data"].(map[string]any)
	require.EqualValues(t, 2, data["deletedCount"])
	require.Equal(t, []any{formatAlarmID(fixture.alarmOld.ID), formatAlarmID(fixture.alarmNew.ID)}, data["deletedIds"])
	require.EqualValues(t, 1, countAlarmRows(t, fixture.db))
}

func TestAlarmBatchDeleteRollsBackWhenRowsAffectedChanges(t *testing.T) {
	fixture := newAlarmHTTPFixture(t, true)
	require.NoError(t, fixture.db.Callback().Delete().Before("gorm:delete").Register("test:alarm-delete-race", func(tx *gorm.DB) {
		if tx.Statement.Table == "gb_alarm_event" {
			tx.Exec("DELETE FROM gb_alarm_event WHERE id = ?", fixture.alarmOld.ID)
		}
	}))
	request := []string{formatAlarmID(fixture.alarmOld.ID), formatAlarmID(fixture.alarmNew.ID)}
	w := alarmJSONRequest(t, fixture.router, http.MethodPost, "/api/gb28181/alarms/batch-delete", gin.H{"ids": request})
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	var response map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, "ALARM_DELETE_CONFLICT", response["data"].(map[string]any)["errorCode"])
	require.EqualValues(t, 3, countAlarmRows(t, fixture.db), "行数竞态必须回滚预删和本次删除")
}

func TestAlarmSingleDeleteUnauthorizedMatchesMissing(t *testing.T) {
	fixture := newAlarmHTTPFixture(t, true)
	for _, id := range []string{formatAlarmID(fixture.alarmOther.ID), "999999999999"} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/api/gb28181/alarms/"+id, nil)
		fixture.router.ServeHTTP(w, req)
		require.Equal(t, http.StatusNotFound, w.Code)
		var response map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		require.Equal(t, "ALARM_NOT_FOUND", response["data"].(map[string]any)["errorCode"])
	}
	require.EqualValues(t, 3, countAlarmRows(t, fixture.db))
}

func TestAlarmClearAllDeletesOnlyScopedRows(t *testing.T) {
	fixture := newAlarmHTTPFixture(t, true)
	w := alarmJSONRequest(t, fixture.router, http.MethodPost, "/api/gb28181/alarms/clear-all", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var response map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.EqualValues(t, 2, response["data"].(map[string]any)["deletedCount"])
	require.EqualValues(t, 1, countAlarmRows(t, fixture.db), "外部门告警必须保留")

	var remaining gbmodels.GbAlarmEvent
	require.NoError(t, fixture.db.First(&remaining, fixture.alarmOther.ID).Error)
	require.Equal(t, fixture.alarmOther.ID, remaining.ID)
}

func alarmRequest(t *testing.T, router *gin.Engine, method, path string) map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var response map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	return response
}

func alarmJSONRequest(t *testing.T, router *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	return w
}

func countAlarmRows(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbAlarmEvent{}).Count(&count).Error)
	return count
}

func seedAlarmDeptUser(t *testing.T, db *gorm.DB, userID, deptID uint) {
	t.Helper()
	require.NoError(t, db.Create(&basemodels.SysDepartment{BaseModel: basemodels.BaseModel{ID: deptID}, Name: "告警部门"}).Error)
	require.NoError(t, db.Create(&basemodels.User{BaseModel: basemodels.BaseModel{ID: userID}, Username: "alarm-user", Password: "x", DeptID: deptID}).Error)
	role := &basemodels.SysRole{Name: "alarm-dept-role", DataScope: 3}
	require.NoError(t, db.Create(role).Error)
	require.NoError(t, db.Create(&basemodels.SysUserRole{UserID: userID, RoleID: role.ID}).Error)
}

func float64Ptr(value float64) *float64 { return &value }

func formatUint(value uint64) string { return formatAlarmID(value) }
