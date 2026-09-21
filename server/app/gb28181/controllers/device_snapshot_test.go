package controllers_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	"uvplatform.cn/uvp-gb28181/app/gb28181/devicecapture"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	globalapp "uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
)

// 真机上传的 JPEG 头尾。设备实际发的是 137 KB 真图，这里只要过 magic 校验即可。
var uploadJPEGBytes = []byte{0xff, 0xd8, 0xff, 0xdb, 0x00, 0x01, 0x02, 0x03, 0xff, 0xd9}

const uploadBoundary = "---------------------------uvptestboundary"

// buildDeviceUploadMultipart 按**海康真机的报文形状**拼 multipart：
// 部件头里带 `name` + `filename` + `Content-Type`，文件名只在这里出现（真机路径里没有文件名段）。
func buildDeviceUploadMultipart(fieldName, filename string, payload []byte) (contentType string, body *bytes.Buffer) {
	body = &bytes.Buffer{}
	fmt.Fprintf(body, "--%s\r\n", uploadBoundary)
	fmt.Fprintf(body, "Content-Disposition: form-data; name=%q; filename=%q\r\n", fieldName, filename)
	body.WriteString("Content-Type: image/jpeg\r\n\r\n")
	body.Write(payload)
	fmt.Fprintf(body, "\r\n--%s--\r\n", uploadBoundary)
	return "multipart/form-data; boundary=" + uploadBoundary, body
}

// snapshotUploadRouter 复刻 RegisterContentRoutes 里 POST 那一条的注册形态
// （catch-all 是重点：真机路径带尾斜杠、无文件名段）。
func snapshotUploadRouter(controller *gbcontrollers.DeviceMgmtController) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())
	router.POST("/uploads/*token", controller.UploadDeviceSnapshot)
	return router
}

// TestUploadDeviceSnapshotAcceptsHikvisionMultipart 是**真机形态**的回归锚点。
//
// 2026-09-20 实测：设备发 `POST …/uploads/<token>/?SessionID=…`，body 是 multipart，
// 文件名在部件头。改造前三处全部对不上（只注册 PUT / 路径必须带 :filename /
// body 必须是裸 JPEG）⇒ 设备收到纯文本 `404 page not found`。
func TestUploadDeviceSnapshotAcceptsHikvisionMultipart(t *testing.T) {
	controller, db, _, _ := newPTZResourceController(t)
	// 图像库那张表由增量迁移建；单元测试的 sqlite 库直接 AutoMigrate 出同构结构。
	require.NoError(t, db.AutoMigrate(&gbmodels.GbChannelSnapshot{}))
	registry := devicecapture.NewRegistry(t.TempDir())
	controller.SetCaptureRuntime(registry)
	session := registry.Create(devicecapture.CreateRequest{
		OwnerID: "12", ChannelID: "31", ChannelCode: "37010301021320000002",
		DeviceCode: "37010301021120000001", SnapNum: 3, Interval: 3,
	})
	// 41 位文件名 = 设备编码 20 + 图像编码 2 + 时间 17（毫秒）+ 序列码 2（backlog E-4）
	const deviceFilename = "37010301021320000002022026092014090311401.jpg"
	contentType, body := buildDeviceUploadMultipart("file", deviceFilename, uploadJPEGBytes)

	router := snapshotUploadRouter(controller)
	// ⛔ 三个细节缺一不可：**POST** + 尾斜杠 + 带 `?SessionID=` 查询串。
	request := httptest.NewRequest(http.MethodPost, "/uploads/"+session.UploadToken+"/?SessionID="+session.ID, body)
	request.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	// 文件名必须**来自部件头**，不是路径参数（路径里根本没有文件名段）。
	require.Contains(t, response.Body.String(), deviceFilename)

	file, err := registry.Content(session.UploadToken, deviceFilename)
	require.NoError(t, err, "图片必须真的落盘，且以部件头里的文件名为准")
	require.Equal(t, int64(len(uploadJPEGBytes)), file.Size)

	got, _ := registry.GetForOwner(session.ID, "12")
	require.Len(t, got.Files, 1)
	require.Equal(t, devicecapture.StateReceiving, got.State)

	// ⭐ 落盘之外还必须**入库**：图像库（按通道/时间段/会话找图、按保留期清理）读的是表。
	var stored gbmodels.GbChannelSnapshot
	require.NoError(t, db.Where("file_name = ?", deviceFilename).First(&stored).Error)
	require.Equal(t, "37010301021320000002", stored.ChannelCode)
	require.Equal(t, uint(31), stored.ChannelID)
	require.Equal(t, gbmodels.SnapshotSourceDevice, stored.Source)
	require.Equal(t, int64(len(uploadJPEGBytes)), stored.Size)
	require.NotEmpty(t, stored.MD5, "必须留下图片摘要，供完整性对账")
	// ⛔ rel_path 必须是**相对**路径且正斜杠分隔：存绝对路径的库一搬机器就全部失效。
	require.Equal(t, "gb-device-snapshots/"+session.ID+"/"+deviceFilename, stored.RelPath)
	// ⛔ captured_at 取**文件名反解**的拍摄时刻（14:09:03.114），不是接收时刻（测试运行的那一刻）。
	require.Equal(t, "2026-09-20 14:09:03.114", stored.CapturedAt.Format("2006-01-02 15:04:05.000"))
	require.NotNil(t, stored.SessionID)
	require.Equal(t, session.ID, *stored.SessionID)
	// 响应里必须回 id：前端要拿它拼稳定读图地址（会话 token 会过期，id 不会）。
	require.Contains(t, response.Body.String(), `"id":`+uintStr(stored.ID))
}

// TestUploadDeviceSnapshotIsIdempotentForRepeatedImage 设备重传同一张图（网络抖动很常见）
// 必须幂等：唯一键 `(channel_code, file_name)` 让第二次走冲突更新，不产生第二行。
//
// ⛔ 也不能因为"已经有这行了"就把新字节丢掉 —— registry.Upload 会覆盖磁盘文件，
// 库行必须跟着指向新的 size/md5，否则库与磁盘不一致（对账时看起来像图片被篡改）。
func TestUploadDeviceSnapshotIsIdempotentForRepeatedImage(t *testing.T) {
	controller, db, _, _ := newPTZResourceController(t)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbChannelSnapshot{}))
	registry := devicecapture.NewRegistry(t.TempDir())
	controller.SetCaptureRuntime(registry)
	session := registry.Create(devicecapture.CreateRequest{
		OwnerID: "12", ChannelID: "31", ChannelCode: "37010301021320000002", SnapNum: 1, Interval: 1,
	})
	const deviceFilename = "37010301021320000002022026092014090311401.jpg"
	router := snapshotUploadRouter(controller)

	upload := func(payload []byte) *httptest.ResponseRecorder {
		contentType, body := buildDeviceUploadMultipart("file", deviceFilename, payload)
		request := httptest.NewRequest(http.MethodPost, "/uploads/"+session.UploadToken+"/", body)
		request.Header.Set("Content-Type", contentType)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	require.Equal(t, http.StatusOK, upload(uploadJPEGBytes).Code)
	// 第二次上传的字节更多（模拟设备重拍/重传），大小与摘要都必须被更新。
	second := append([]byte{0xff, 0xd8, 0xff, 0xdb}, uploadJPEGBytes[4:]...)
	second = append(second[:len(second)-2], 0x11, 0x22, 0xff, 0xd9)
	require.Equal(t, http.StatusOK, upload(second).Code)

	var rows []gbmodels.GbChannelSnapshot
	require.NoError(t, db.Where("file_name = ?", deviceFilename).Find(&rows).Error)
	require.Len(t, rows, 1, "同一通道同一文件名只能有一行")
	require.Equal(t, int64(len(second)), rows[0].Size, "重传后库里的大小必须跟着更新")
}

// TestUploadDeviceSnapshotFailsLoudlyWhenLibraryRecordFails 图落盘成功但**登记图像库失败**时
// 必须回 5xx，不许静默 200。
//
// ⛔ 这条是"图像库能不能用"的守护：库写不进去（表缺失、字段超长、库不可用）却回 200 的结果是
// "图片在盘上、缩略图也能看（走会话 token 那条路），但图像库里永远找不到这张图" ——
// 操作员与运维都不会发现。回 5xx 让设备重传，而重传因唯一键幂等，是安全的补救手段。
//
// 这里刻意**不 AutoMigrate gb_channel_snapshot**，让登记必然失败。
func TestUploadDeviceSnapshotFailsLoudlyWhenLibraryRecordFails(t *testing.T) {
	controller, _, _, _ := newPTZResourceController(t)
	registry := devicecapture.NewRegistry(t.TempDir())
	controller.SetCaptureRuntime(registry)
	session := registry.Create(devicecapture.CreateRequest{
		OwnerID: "12", ChannelID: "31", ChannelCode: "37010301021320000002", SnapNum: 1, Interval: 1,
	})
	contentType, body := buildDeviceUploadMultipart("file", "37010301021320000002022026092014090311401.jpg", uploadJPEGBytes)
	request := httptest.NewRequest(http.MethodPost, "/uploads/"+session.UploadToken+"/", body)
	request.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()
	snapshotUploadRouter(controller).ServeHTTP(response, request)

	require.Equal(t, http.StatusInternalServerError, response.Code, response.Body.String())
	// 图仍然落盘了（收图本身是成功的），这一点也要保住：5xx 表达的是"索引没建成"，
	// 不是"图没收下"——设备重传时会覆盖同一个文件，不会留下半张图。
	_, err := registry.Content(session.UploadToken, "37010301021320000002022026092014090311401.jpg")
	require.NoError(t, err)
}

// TestSnapshotContentServesImageByLibraryID 是「上传 → 落盘 → 入库 → 按 id 取图」的端到端锚点。
//
// ⭐ 这条链路存在的理由：会话 token 那条读图路径（`…/uploads/<token>/<file>`）随会话过期、
// 后端一重启就没了；图像库/会话面板重载都必须能按**库行 id** 把历史图取出来。
func TestSnapshotContentServesImageByLibraryID(t *testing.T) {
	controller, db, _, _ := newPTZResourceController(t)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbChannelSnapshot{}))
	registry := devicecapture.NewRegistry(t.TempDir())
	controller.SetCaptureRuntime(registry)
	session := registry.Create(devicecapture.CreateRequest{
		OwnerID: "12", ChannelID: "31", ChannelCode: "37010301021320000002", SnapNum: 1, Interval: 1,
	})
	const deviceFilename = "37010301021320000002022026092014090311401.jpg"
	contentType, body := buildDeviceUploadMultipart("file", deviceFilename, uploadJPEGBytes)
	request := httptest.NewRequest(http.MethodPost, "/uploads/"+session.UploadToken+"/", body)
	request.Header.Set("Content-Type", contentType)
	recorder := httptest.NewRecorder()
	snapshotUploadRouter(controller).ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var stored gbmodels.GbChannelSnapshot
	require.NoError(t, db.Where("file_name = ?", deviceFilename).First(&stored).Error)

	libraryRouter := gin.New()
	libraryRouter.Use(gin.Recovery())
	libraryRouter.GET("/snapshots/:id/content", controller.SnapshotContent)

	t.Run("按 id 取到同一张图", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		libraryRouter.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/snapshots/"+uintStr(stored.ID)+"/content", nil))
		require.Equal(t, http.StatusOK, recorder.Code)
		require.Equal(t, uploadJPEGBytes, recorder.Body.Bytes(), "读出来的必须是那张原图")
	})

	t.Run("不存在的 id", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		libraryRouter.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/snapshots/999999/content", nil))
		require.Equal(t, http.StatusNotFound, recorder.Code)
	})

	t.Run("非数字 id", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		libraryRouter.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/snapshots/abc/content", nil))
		require.Equal(t, http.StatusNotFound, recorder.Code)
	})

	t.Run("库行还在但文件被删", func(t *testing.T) {
		path, err := registry.FilePath(stored.RelPath)
		require.NoError(t, err)
		require.NoError(t, os.Remove(path))
		recorder := httptest.NewRecorder()
		libraryRouter.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/snapshots/"+uintStr(stored.ID)+"/content", nil))
		// ⛔ 404 而不是 500：库行与磁盘不一致是可预期的运营状态（手工清过目录、回滚过迁移），
		// 不是服务故障。
		require.Equal(t, http.StatusNotFound, recorder.Code)
	})
}

// TestUploadDeviceSnapshotMultipartDoesNotRequireFieldName 证明字段名不是判据：
// `name="file"` 只是海康自己的选择（不属协议），真正不可省略的是**部件头里的 filename**。
//
// ⭐ 顺带钉住"文件名不合规也能入库"：`shot.jpg` 不是 41 位标识 ⇒ 反解不出拍摄时刻
// ⇒ captured_at 回落到接收时刻，**但行照样写**（图片已经在盘上了，不该因为命名不合规丢掉索引）。
func TestUploadDeviceSnapshotMultipartDoesNotRequireFieldName(t *testing.T) {
	controller, db, _, _ := newPTZResourceController(t)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbChannelSnapshot{}))
	registry := devicecapture.NewRegistry(t.TempDir())
	controller.SetCaptureRuntime(registry)
	session := registry.Create(devicecapture.CreateRequest{OwnerID: "12", ChannelID: "31", SnapNum: 1, Interval: 1})
	contentType, body := buildDeviceUploadMultipart("image", "shot.jpg", uploadJPEGBytes)

	request := httptest.NewRequest(http.MethodPost, "/uploads/"+session.UploadToken+"/", body)
	request.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()
	snapshotUploadRouter(controller).ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	_, err := registry.Content(session.UploadToken, "shot.jpg")
	require.NoError(t, err)

	var stored gbmodels.GbChannelSnapshot
	require.NoError(t, db.Where("file_name = ?", "shot.jpg").First(&stored).Error)
	// 反解不出拍摄时刻 ⇒ 回落接收时刻；两者差异很大时说明回落发生了（这里只要非零即可）。
	require.False(t, stored.CapturedAt.IsZero(), "回落后必须是一个真实时刻，不能是 1970")
	require.WithinDuration(t, time.Now(), stored.CapturedAt, time.Minute)
}

// TestUploadDeviceSnapshotRejectsUnknownTokenAndBadImage 反向锚点：两条各自的拒绝理由不能混。
func TestUploadDeviceSnapshotRejectsUnknownTokenAndBadImage(t *testing.T) {
	controller, _, _, _ := newPTZResourceController(t)
	registry := devicecapture.NewRegistry(t.TempDir())
	controller.SetCaptureRuntime(registry)
	session := registry.Create(devicecapture.CreateRequest{OwnerID: "12", ChannelID: "31", SnapNum: 1, Interval: 1})
	router := snapshotUploadRouter(controller)

	t.Run("未登记的 token", func(t *testing.T) {
		contentType, body := buildDeviceUploadMultipart("file", "shot.jpg", uploadJPEGBytes)
		request := httptest.NewRequest(http.MethodPost, "/uploads/00000000-0000-0000-0000-000000000000/", body)
		request.Header.Set("Content-Type", contentType)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		require.Equal(t, http.StatusNotFound, response.Code)
	})

	t.Run("部件体不是 JPEG", func(t *testing.T) {
		contentType, body := buildDeviceUploadMultipart("file", "shot.jpg", []byte("not-a-jpeg"))
		request := httptest.NewRequest(http.MethodPost, "/uploads/"+session.UploadToken+"/", body)
		request.Header.Set("Content-Type", contentType)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		require.Equal(t, http.StatusUnprocessableEntity, response.Code)
	})

	t.Run("没有带文件名的部件", func(t *testing.T) {
		body := &bytes.Buffer{}
		fmt.Fprintf(body, "--%s\r\n", uploadBoundary)
		body.WriteString("Content-Disposition: form-data; name=\"SessionID\"\r\n\r\nabc\r\n")
		fmt.Fprintf(body, "--%s--\r\n", uploadBoundary)
		request := httptest.NewRequest(http.MethodPost, "/uploads/"+session.UploadToken+"/", body)
		request.Header.Set("Content-Type", "multipart/form-data; boundary="+uploadBoundary)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		require.Equal(t, http.StatusUnprocessableEntity, response.Code)
	})
}

func TestCreateSnapshotSessionDispatchesCapabilityUploadURL(t *testing.T) {
	controller, db, channel, _ := newPTZResourceController(t)
	require.NoError(t, db.AutoMigrate(&basemodels.SysDepartment{}, &basemodels.SysRole{}, &basemodels.SysUserRole{}, &basemodels.User{}))
	seedDeptScopedUser(t, db, 12, 10)
	require.NoError(t, db.Model(&gbmodels.GbDevice{}).Where("device_id = ?", channel.DeviceID).Updates(map[string]interface{}{"effective_version": gbmodels.ProtocolVersion2022, "owner_dept_id": 10}).Error)
	require.NoError(t, db.Model(channel).Update("owner_dept_id", 10).Error)
	controller.SetCaptureRuntime(devicecapture.NewRegistry(t.TempDir()))
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(func(c *gin.Context) {
		c.Set(consts.BindContextKeyName, &globalapp.Claims{ClaimsUser: globalapp.ClaimsUser{UserID: 12}})
		c.Next()
	})
	router.POST("/channel/:id/snapshot-sessions", controller.CreateSnapshotSession)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/snapshot-sessions", strings.NewReader(`{"snapNum":2,"interval":3}`))
	// ⛔ Host 必须是**设备可达的局域网地址**：`UploadURL` 由它派生，取回环地址会被
	// `snapshotUploadHostUnreachableForDevice` 当场拒发（设备是另一台机器）。
	// 取值与真机形态一致：前端 vite 在 5177。
	request.Host = "192.168.10.120:5177"
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())

	// ⛔ 命令类型必须是 `DeviceConfig`，不是 `DeviceControl`。
	// 2026-09-20 海康真机 A/B 取证：`DeviceControl` 形态设备**连业务应答都不回**、
	// 回读 `SnapNum` 仍是 0（配置压根没进去）；`DeviceConfig` 才回 `<Result>OK</Result>`
	// 并按张数抓拍。A.2.3.2.1 的 `fixed="DeviceConfig"` 是明文要求。
	var operation gbmodels.GbPTZOperation
	require.NoError(t, db.Where("action = ?", "snapshot_config").First(&operation).Error)
	require.Equal(t, "DeviceConfig", operation.CmdType)
	// ⛔ 必须等业务应答：只有这样 ack 才会自动派生一条 `ConfigDownload` 回读对账，
	// 抓拍配置才可能"配了能验"（旧实现 response_required=false，永远停在 sent）。
	require.True(t, operation.ResponseRequired)
	// ⛔ 重试预算刻意是 1：这条命令不幂等，每收到一次就再抓 SnapNum 张。
	require.Equal(t, 1, operation.MaxAttempts)

	// payload 带的是**配置族**的载荷（configTypes + blocks），回读对账正是从这里反推要读什么。
	require.Contains(t, operation.PayloadJSON, `"configTypes":["SnapShotConfig"]`)
	require.Contains(t, operation.PayloadJSON, `"snapShot"`)
	require.Contains(t, operation.PayloadJSON, "/api/gb28181/device-snapshots/uploads/",
		"上传地址必须由平台生成并写进配置")
	require.Contains(t, operation.PayloadJSON,
		`"uploadUrl":"http://192.168.10.120:5177/api/gb28181/device-snapshots/uploads/`,
		"上传地址由请求 Host 派生（设备从局域网回连的那条路）")
	// ⛔ 这里**不断言 SIP 报文正文**：`response_required=true` 的命令由 scheduler 下发
	// （`Execute` 只落一条 queued），而 scheduler 是 app 主循环驱动的，单元测试里不跑。
	// 报文形态由协议层锚点覆盖：`manscdp.TestBuildSnapShotConfigUsesDeviceConfigCmdType`
	// ——那条用例断言 `<CmdType>DeviceConfig</CmdType>` + `<SnapShotConfig>`，
	// 与本用例断言的 operation（cmd_type / payload.blocks）合起来才是完整闭环。
	//
	// ⛔⛔ 但**重建**那一环必须单独钉住（`ptz.TestBuildScheduledPTZBodyBlocksPayload
	// NeverRebuildsAsVideoParamAttribute`）：2026-09-20 的生产缺陷正是"落库的 operation
	// 完全正确、重建出来的报文却是 A-5" —— 只看落库内容会完全放过它。
}

// ---------- 图像库列表接口 ----------

// snapshotLibraryFixture 复用 PTZ 脚手架（它已建好 device/channel 与数据范围要用的授权表），
// 再补上图像库表与 sys 部门/角色表 —— 数据范围用例需要后者。
//
// ⛔ 必须先 useRealResponseHandler(t)：同包的 `zlm_node_test.go` 在 init 里把全局
// `app.Response` 换成了 `mockResponse`，而那个 mock 的 `Fail` 硬编码
// `c.JSON(http.StatusOK, ...)`（忽略 httpCode）。不显式换回真实 handler，本组用例的
// 状态码断言就只是在量那个 mock —— 单跑绿、全量跑红，而"红"的那次才是真相。
func snapshotLibraryFixture(t *testing.T, middleware ...gin.HandlerFunc) (*gin.Engine, *gorm.DB) {
	t.Helper()
	useRealResponseHandler(t)
	controller, db, _, _ := newPTZResourceController(t)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbChannelSnapshot{},
		&basemodels.SysDepartment{}, &basemodels.SysRole{}, &basemodels.SysUserRole{}, &basemodels.User{}))
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(middleware...)
	router.GET("/snapshots", controller.ListSnapshots)
	return router, db
}

// seedSnapshotChannel 建"设备 + 通道"这一对：图像库列表要靠 JOIN 才能拿到通道名/设备名，
// 以及**归属部门**（数据范围过滤打的就是 gb_channel.owner_dept_id）。
func seedSnapshotChannel(t *testing.T, db *gorm.DB, deviceCode, channelCode string, deptID uint) {
	t.Helper()
	require.NoError(t, db.Create(&gbmodels.GbDevice{
		DeviceID: deviceCode, Name: "设备-" + deviceCode, OwnerDeptID: deptID,
	}).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannel{
		DeviceID: deviceCode, ChannelID: channelCode, Name: "通道-" + channelCode,
		OwnerDeptID: deptID, Status: gbmodels.ChannelStatusOnline,
	}).Error)
}

// channelPrimaryKey 取通道的**平台主键**（gb_channel.id）。
// ⛔ 断言里不能拿国标编码代替它：那正是 `channelId` 参数最容易被写错的形态，
// 用编码去断言等于把两列混成一件东西，写错也看不出来。
func channelPrimaryKey(t *testing.T, db *gorm.DB, channelCode string) uint {
	t.Helper()
	var channel gbmodels.GbChannel
	require.NoError(t, db.Where("channel_id = ?", channelCode).First(&channel).Error)
	require.NotZero(t, channel.ID)
	return channel.ID
}

func ptrTo[T any](value T) *T { return &value }

// seedSnapshotImage 落一张库行；`mutate` 用于补那些"只有某个筛选维度才关心"的列
// （`channel_id` 平台主键 / `session_id` 会话）。⛔ 默认**刻意不填**这两列：
// 填了的话"筛选条件根本没生效"会被"反正只有一行"掩盖成通过。
func seedSnapshotImage(t *testing.T, db *gorm.DB, channelCode, fileName string, capturedAt time.Time, source string, mutate ...func(*gbmodels.GbChannelSnapshot)) uint {
	t.Helper()
	row := &gbmodels.GbChannelSnapshot{
		ChannelCode: channelCode, FileName: fileName,
		RelPath: "gb-device-snapshots/session-a/" + fileName,
		Size:    2048, MD5: "d41d8cd98f00b204e9800998ecf8427e",
		CapturedAt: capturedAt, Source: source,
	}
	for _, apply := range mutate {
		apply(row)
	}
	require.NoError(t, db.Create(row).Error)
	return row.ID
}

func getSnapshotLibraryList(t *testing.T, router *gin.Engine, query string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/snapshots"+query, nil))
	return w
}

// decodeSnapshotLibraryData 解出整个 `data` 对象（happy path 固定 HTTP 200）。
func decodeSnapshotLibraryData(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	data, ok := body["data"].(map[string]any)
	require.True(t, ok, "响应缺少 data 对象: %s", w.Body.String())
	return data
}

// decodeSnapshotLibraryList 解出 `data.list` 与 `data.total`。
// ⛔ 只解 JSON 而不读结构体：本用例要验证的是**响应契约**（字段名、url 形状），
// 用结构体反解会把"字段名改了"这类回归吃掉（结构体 tag 跟着改，测试照样绿）。
func decodeSnapshotLibraryList(t *testing.T, w *httptest.ResponseRecorder) ([]map[string]any, float64) {
	t.Helper()
	data := decodeSnapshotLibraryData(t, w)
	total, ok := data["total"].(float64)
	require.True(t, ok, "响应缺少 total: %s", w.Body.String())
	rawList, ok := data["list"].([]any)
	require.True(t, ok, "响应缺少 list: %s", w.Body.String())
	list := make([]map[string]any, 0, len(rawList))
	for _, item := range rawList {
		entry, ok := item.(map[string]any)
		require.True(t, ok, "list 元素不是对象: %s", w.Body.String())
		list = append(list, entry)
	}
	return list, total
}

// TestListSnapshotsOrdersByCapturedAtAndPaginates 钉住三条契约：
// 倒序时间轴、total 是命中总数（不是本页条数）、url 由库行 id 派生。
func TestListSnapshotsOrdersByCapturedAtAndPaginates(t *testing.T) {
	router, db := snapshotLibraryFixture(t)
	const deviceCode = "37010301021320000111"
	const channelCode = "37010301021320000112"
	seedSnapshotChannel(t, db, deviceCode, channelCode, 10)
	base := time.Date(2026, 9, 20, 14, 33, 10, 0, time.UTC)
	oldest := seedSnapshotImage(t, db, channelCode, "a.jpg", base, gbmodels.SnapshotSourceDevice)
	middle := seedSnapshotImage(t, db, channelCode, "b.jpg", base.Add(time.Minute), gbmodels.SnapshotSourceDevice)
	newest := seedSnapshotImage(t, db, channelCode, "c.jpg", base.Add(2*time.Minute), gbmodels.SnapshotSourceDevice)

	list, total := decodeSnapshotLibraryList(t, getSnapshotLibraryList(t, router, "?pageSize=2"))
	require.Equal(t, float64(3), total, "total 必须是命中总数，不是本页条数")
	require.Len(t, list, 2)
	require.Equal(t, float64(newest), list[0]["id"], "默认按拍摄时刻倒序（最新的在前）")
	require.Equal(t, float64(middle), list[1]["id"])
	require.Equal(t,
		"/api/gb28181/device-mgmt/snapshots/"+strconv.FormatUint(uint64(newest), 10)+"/content",
		list[0]["url"], "取图地址必须由库行 id 派生（前端不自己拼路径）")
	require.Equal(t, "通道-"+channelCode, list[0]["channelName"], "通道名来自 JOIN，不是本表的列")
	require.Equal(t, "设备-"+deviceCode, list[0]["deviceName"])
	require.Equal(t, deviceCode, list[0]["deviceCode"], "deviceCode 是 20 位国标编码")

	list2, total2 := decodeSnapshotLibraryList(t, getSnapshotLibraryList(t, router, "?pageSize=2&page=2"))
	require.Equal(t, float64(3), total2)
	require.Len(t, list2, 1, "第二页只剩一条")
	require.Equal(t, float64(oldest), list2[0]["id"])
}

// TestListSnapshotsTimeWindowUsesCapturedAtNotCreatedAt 把"时间条件打在哪一列"钉死。
//
// ⭐ 手法：把**拍摄时刻**放到 2020 年，而 created_at（落库时刻）必然是"现在"。
// 于是"打在 captured_at"与"打在 created_at"对同一个窗口给出**相反**的结果，
// 用窗口断言就能把两者区分开 —— 若两边是同一个时刻，这条用例是区分不出来的。
func TestListSnapshotsTimeWindowUsesCapturedAtNotCreatedAt(t *testing.T) {
	router, db := snapshotLibraryFixture(t)
	const channelCode = "37010301021320000212"
	seedSnapshotChannel(t, db, "37010301021320000211", channelCode, 10)
	seedSnapshotImage(t, db, channelCode, "old.jpg",
		time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC), gbmodels.SnapshotSourceDevice)

	split := url.QueryEscape(time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339))
	_, total := decodeSnapshotLibraryList(t, getSnapshotLibraryList(t, router, "?to="+split))
	require.Equal(t, float64(1), total, "时间条件必须打在 captured_at（拍摄时刻）")
	_, total = decodeSnapshotLibraryList(t, getSnapshotLibraryList(t, router, "?from="+split))
	require.Equal(t, float64(0), total)
}

// TestListSnapshotsFiltersByChannelDeviceAndSource 覆盖三种筛选维度。
func TestListSnapshotsFiltersByChannelDeviceAndSource(t *testing.T) {
	router, db := snapshotLibraryFixture(t)
	const deviceA, channelA = "37010301021320000311", "37010301021320000312"
	const deviceB, channelB = "37010301021320000411", "37010301021320000412"
	seedSnapshotChannel(t, db, deviceA, channelA, 10)
	seedSnapshotChannel(t, db, deviceB, channelB, 10)
	base := time.Date(2026, 9, 20, 14, 0, 0, 0, time.UTC)
	deviceShot := seedSnapshotImage(t, db, channelA, "a.jpg", base, gbmodels.SnapshotSourceDevice)
	zlmShot := seedSnapshotImage(t, db, channelB, "b.jpg", base.Add(time.Hour), gbmodels.SnapshotSourceZLM)

	list, total := decodeSnapshotLibraryList(t, getSnapshotLibraryList(t, router, "?channelCode="+channelB))
	require.Equal(t, float64(1), total)
	require.Equal(t, float64(zlmShot), list[0]["id"])

	// ⛔ 设备维度用 `deviceCode`（20 位编码）而不是 `deviceId`：本表的 `device_id` 是
	// **平台主键**，与设备/通道列表接口里同名的 `deviceId`（编码）不是一回事。
	list, total = decodeSnapshotLibraryList(t, getSnapshotLibraryList(t, router, "?deviceCode="+deviceA))
	require.Equal(t, float64(1), total)
	require.Equal(t, float64(deviceShot), list[0]["id"])

	list, total = decodeSnapshotLibraryList(t, getSnapshotLibraryList(t, router, "?source=zlm"))
	require.Equal(t, float64(1), total)
	require.Equal(t, float64(zlmShot), list[0]["id"])
}

// TestListSnapshotsFiltersByPlatformChannelAndSession 补齐两个**静默失效**风险最高的参数。
//
//   - `channelId` 打的是 `s.channel_id`（**平台主键**），而同一个函数里 `channelCode` 打的是
//     国标编码。把 `channelId` 写成拿编码去比主键（或反过来），两个参数之一必然恒空 ——
//     而"恒空"的表现与"这段时间没抓拍过"完全一样，没人会去怀疑接口。
//   - `sessionId` 是会话面板"从库重载"的唯一入口（内存 Registry 一刷新就丢，图还在就得能重聚）。
//     漏掉它，表现是"刷新页面后这次抓拍的图就找不回来了"，而接口本身一切正常。
func TestListSnapshotsFiltersByPlatformChannelAndSession(t *testing.T) {
	router, db := snapshotLibraryFixture(t)
	const devA, codeA = "37010301021320000511", "37010301021320000512"
	const devB, codeB = "37010301021320000521", "37010301021320000522"
	seedSnapshotChannel(t, db, devA, codeA, 10)
	seedSnapshotChannel(t, db, devB, codeB, 10)
	pkA, pkB := channelPrimaryKey(t, db, codeA), channelPrimaryKey(t, db, codeB)

	base := time.Date(2026, 9, 20, 15, 0, 0, 0, time.UTC)
	const sessionA = "session-aaaa"
	shotA := seedSnapshotImage(t, db, codeA, "a.jpg", base, gbmodels.SnapshotSourceDevice,
		func(row *gbmodels.GbChannelSnapshot) {
			row.DeviceID, row.ChannelID, row.SessionID = pkA, pkA, ptrTo(sessionA)
		})
	shotB := seedSnapshotImage(t, db, codeB, "b.jpg", base.Add(time.Minute), gbmodels.SnapshotSourceDevice,
		func(row *gbmodels.GbChannelSnapshot) {
			row.DeviceID, row.ChannelID = pkB, pkB
		})
	require.NotEqual(t, pkA, pkB, "两个通道的平台主键必须不同，否则这条用例区分不出打到哪一列")

	list, total := decodeSnapshotLibraryList(t, getSnapshotLibraryList(t,
		router, "?channelId="+strconv.FormatUint(uint64(pkA), 10)))
	require.Equal(t, float64(1), total, "channelId 打的是平台主键（gb_channel.id）")
	require.Equal(t, float64(shotA), list[0]["id"])

	list, total = decodeSnapshotLibraryList(t, getSnapshotLibraryList(t, router, "?sessionId="+sessionA))
	require.Equal(t, float64(1), total, "sessionId 用于会话面板从库重载")
	require.Equal(t, float64(shotA), list[0]["id"])
	require.Equal(t, sessionA, list[0]["sessionId"], "会话标识必须回显，前端才能按会话重聚")

	list, total = decodeSnapshotLibraryList(t, getSnapshotLibraryList(t,
		router, "?channelId="+strconv.FormatUint(uint64(pkB), 10)))
	require.Equal(t, float64(1), total, "另一个通道也要能按平台主键查到（否则上面那条可能是恒真）")
	require.Equal(t, float64(shotB), list[0]["id"])

	// ⛔ 两个维度要能**叠加**（会话面板是在某通道下重载的），只按会话过滤会把别的通道的
	// 同会话图也端出来（会话 id 是平台生成的，跨通道不保证不重名）。
	_, total = decodeSnapshotLibraryList(t, getSnapshotLibraryList(t,
		router, "?sessionId="+sessionA+"&channelId="+strconv.FormatUint(uint64(pkB), 10)))
	require.Equal(t, float64(0), total, "会话 + 通道是 AND，不是两条独立查询的最后一条")
}

// TestListSnapshotsCapsPageSizeAndCoercesPage 把分页的两个**静默**风险钉死：
// `pageSize` 不封顶等于一次请求扫全表（`?pageSize=1000000` 就能把库拖住）；
// 页号非法时若不回落而是照原样算 offset，表现是"翻到第 0 页看到空列表"，
// 与"这段时间没抓拍过"分不出来。`page`/`pageSize` 必须原样回显，前端分页器靠它对齐。
func TestListSnapshotsCapsPageSizeAndCoercesPage(t *testing.T) {
	router, db := snapshotLibraryFixture(t)
	const channelCode = "37010301021320000612"
	seedSnapshotChannel(t, db, "37010301021320000611", channelCode, 10)
	base := time.Date(2026, 9, 20, 16, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		seedSnapshotImage(t, db, channelCode, fmt.Sprintf("s%d.jpg", i),
			base.Add(time.Duration(i)*time.Minute), gbmodels.SnapshotSourceDevice)
	}

	data := decodeSnapshotLibraryData(t, getSnapshotLibraryList(t, router, "?pageSize=1000000"))
	require.Equal(t, float64(200), data["pageSize"], "pageSize 必须有上限")
	require.Equal(t, float64(3), data["total"], "封顶的是本页条数，不是命中总数")

	for _, query := range []string{"?page=0&pageSize=2", "?page=-3", "?page=abc"} {
		data = decodeSnapshotLibraryData(t, getSnapshotLibraryList(t, router, query))
		require.Equal(t, float64(1), data["page"], "非法页号回落到第 1 页（而不是空列表）: %s", query)
		require.Equal(t, float64(3), data["total"], query)
	}
}

// TestListSnapshotsRejectsBadParams 非法参数必须**报错**而不是静默返回空列表 ——
// 静默空会把"参数写错了"伪装成"这段时间没抓拍过"，让人往设备侧排查。
//
// ⛔ 契约是 **HTTP 400 + `{"code":1}`**，不是 200。`ListSnapshots` 调 `FailAndAbort` 时
// 从不显式传状态码，`DefaultResponseHandler.Fail` 的默认值就是 `http.StatusBadRequest`
// （见 app/utils/response/response.go:82）。**两个都要断言**：只断业务码会被
// "改成 `Success` 里返回 code=1" 这类改法骗过，只断状态码则丢掉了业务码契约。
func TestListSnapshotsRejectsBadParams(t *testing.T) {
	router, _ := snapshotLibraryFixture(t)
	from := url.QueryEscape("2026-09-20T15:00:00+08:00")
	to := url.QueryEscape("2026-09-20T14:00:00+08:00")
	for name, query := range map[string]string{
		"来源不在白名单":  "?source=alien",
		"时间格式不合法":  "?from=not-a-time",
		"通道主键不是数字": "?channelId=abc",
		"起点晚于终点":   "?from=" + from + "&to=" + to,
	} {
		t.Run(name, func(t *testing.T) {
			w := getSnapshotLibraryList(t, router, query)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			var body map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			require.EqualValues(t, 1, body["code"],
				"非法参数必须给出失败业务码，不能静默返回空列表: %s", w.Body.String())
		})
	}
}

// TestListSnapshotsHidesOtherDepartmentsImages 是本接口唯一的越权面：
// 图像库里存的是**监控画面**，跨部门可见等于把别人的摄像机画面给出去。
//
// ⛔ 数据范围打的是 `gb_channel`（JOIN 进来的通道表）而不是本表的 `device_id`：
// 本表的 `device_id` 是平台主键，拿它当"20 位编码"比对会让过滤**静默失效**
// （恒真/恒假），而这条用例正是为拦住那种改法而写的。
func TestListSnapshotsHidesOtherDepartmentsImages(t *testing.T) {
	router, db := snapshotLibraryFixture(t, withClaims(7))
	seedDeptScopedUser(t, db, 7, 10)
	const ownDevice, ownChannel = "37010301021320000511", "37010301021320000512"
	const otherDevice, otherChannel = "37010301021320000611", "37010301021320000612"
	seedSnapshotChannel(t, db, ownDevice, ownChannel, 10)
	seedSnapshotChannel(t, db, otherDevice, otherChannel, 20)
	base := time.Date(2026, 9, 20, 14, 0, 0, 0, time.UTC)
	mine := seedSnapshotImage(t, db, ownChannel, "mine.jpg", base, gbmodels.SnapshotSourceDevice)
	seedSnapshotImage(t, db, otherChannel, "theirs.jpg", base, gbmodels.SnapshotSourceDevice)

	list, total := decodeSnapshotLibraryList(t, getSnapshotLibraryList(t, router, ""))
	require.Equal(t, float64(1), total, "别的部门的图必须不可见")
	require.Len(t, list, 1)
	require.Equal(t, float64(mine), list[0]["id"])
	require.Equal(t, ownChannel, list[0]["channelCode"])
}
