package controllers

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"uvplatform.cn/uvp-gb28181/app/utils/response"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm/clause"

	"uvplatform.cn/uvp-gb28181/app/gb28181/devicecapture"
	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.cn/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

type snapshotCreateBody struct {
	SnapNum  int `json:"snapNum"`
	Interval int `json:"interval"`
}

func (dc *DeviceMgmtController) CreateSnapshotSession(c *gin.Context) {
	registry, service := dc.captureRuntime(), dc.ptzServiceSnapshot()
	claims := dc.GetClaims(c)
	if registry == nil || service == nil {
		response.SetBusinessResult(c, 503, false)
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "message": "设备抓拍服务未就绪"})
		return
	}
	if claims == nil || claims.UserID == 0 {
		response.SetBusinessResult(c, 404, false)
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "通道不存在或无权限"})
		return
	}
	var body snapshotCreateBody
	if err := decodePlaybackJSON(c, &body); err != nil {
		response.SetBusinessResult(c, 422, false)
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": 422, "message": "抓拍配置参数不合法"})
		return
	}
	if body.SnapNum == 0 {
		body.SnapNum = 1
	}
	if body.Interval == 0 {
		body.Interval = 1
	}
	if body.SnapNum < 1 || body.SnapNum > 10 || body.Interval < 1 || body.Interval > 3600 {
		response.SetBusinessResult(c, 422, false)
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": 422, "message": "抓拍数量需为 1-10，间隔需为 1-3600 秒"})
		return
	}
	channel, ok := dc.ptzChannel(c)
	if !ok {
		return
	}
	target, ok := dc.loadPTZTarget(c, channel)
	if !ok {
		return
	}
	if target.Profile.Version != protocol.Version2022 {
		response.SetBusinessResult(c, 409, false)
		c.JSON(http.StatusConflict, gin.H{"code": 409, "message": "图像抓拍配置仅支持 GB/T 28181-2022 设备"})
		return
	}
	session := registry.Create(devicecapture.CreateRequest{
		OwnerID: strconv.FormatUint(uint64(claims.UserID), 10), ChannelID: strconv.FormatUint(uint64(channel.ID), 10),
		ChannelCode: target.ChannelCode, DeviceCode: target.DeviceCode, SnapNum: body.SnapNum, Interval: body.Interval,
	})
	uploadURL, err := snapshotUploadURL(c, session.UploadToken)
	if err != nil {
		registry.MarkFailed(session.ID, err)
		response.SetBusinessResult(c, 503, false)
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "message": err.Error()})
		return
	}
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if key == "" {
		key = "snapshot-" + session.ID
	}
	// ⛔ 走**配置族**通道下发，CmdType 必须是 `DeviceConfig`（A.2.3.2.1 的
	// `fixed="DeviceConfig"`）——不是 `DeviceControl`。2026-09-20 海康真机 A/B 取证：
	// 同一份 `<SnapShotConfig>`，用 `DeviceControl` 发设备**连业务应答都不回**、回读
	// `SnapNum` 仍是 0（配置压根没进去）；换成 `DeviceConfig` 才回 `<Result>OK</Result>`、
	// 按张数抓拍并上报，回读逐字段一致。改造前这条链路一直是"平台记 sent、设备没收到"。
	//
	// ⛔ 顺带拿到的两样东西，正是"抓拍配置配了能验"的闭环：
	//   ① `ResponseRequired` ⇒ 设备回 A.2.6.8 业务应答，operation 才可能进终态；
	//   ② ack 同事务自动派生一条 `ConfigDownload` 回读对账（`createDeviceConfigReconcile`），
	//      结论落在 `reconcile` 里（`SnapNum` / `Interval` / `UploadURL` / `SessionID` 设备会原样回显）。
	snapNum, interval := body.SnapNum, body.Interval
	blocks := manscdp.DeviceConfigBlocks{
		SnapShot: &manscdp.SnapShotBlock{
			SnapNum: &snapNum, Interval: &interval,
			UploadURL: &uploadURL, SessionID: &session.ID,
		},
	}
	operation, err := service.Execute(c.Request.Context(), target, ptz.Command{
		CmdType: manscdp.CmdDeviceConfig, Action: ptz.ActionSnapshotConfig, IdempotencyKey: key,
		Profile: target.Profile, TargetScope: gbmodels.ControlTargetScopeChannel, TargetCode: target.ChannelCode,
		Payload: map[string]interface{}{
			"configTypes": []string{manscdp.ConfigTypeSnapShotConfig},
			"blocks":      blocks,
			"sessionId":   session.ID, "snapNum": snapNum, "interval": interval,
		},
		ResponseRequired: true,
		// ⛔ 重试预算**刻意固定为 1**，别照 ApplyDeviceConfig 改成 3：
		// 这条命令**不幂等** —— 每收到一次就再抓 `SnapNum` 张。设备不 ack 时重发
		// 只会让操作员多拿到几批照片，而不会让配置更容易进去（报文已经发过了）。
		MaxAttempts: 1,
		Build: func(sn int) ([]byte, error) {
			return manscdp.BuildDeviceConfigBlocksWithProfile(target.Profile, target.ChannelCode, sn, blocks)
		},
	})
	if err != nil {
		registry.MarkFailed(session.ID, err)
		response.SetBusinessResult(c, 502, false)
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "message": "下发图像抓拍配置失败: " + err.Error()})
		return
	}
	registry.MarkWaiting(session.ID)
	session, _ = registry.GetForOwner(session.ID, strconv.FormatUint(uint64(claims.UserID), 10))
	dc.Success(c, snapshotSessionView(c, session, operation.OperationID))
}

func (dc *DeviceMgmtController) GetSnapshotSession(c *gin.Context) {
	registry, claims := dc.captureRuntime(), dc.GetClaims(c)
	if registry == nil || claims == nil || claims.UserID == 0 {
		response.SetBusinessResult(c, 404, false)
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "抓拍任务不存在"})
		return
	}
	session, ok := registry.GetForOwner(c.Param("sessionId"), strconv.FormatUint(uint64(claims.UserID), 10))
	if !ok || session.ChannelID != c.Param("id") {
		response.SetBusinessResult(c, 404, false)
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "抓拍任务不存在"})
		return
	}
	dc.Success(c, snapshotSessionView(c, session, ""))
}

// UploadDeviceSnapshot 接收设备回传的抓拍图片。
//
// ⛔ 必须同时支持两种形态，少一种真机的图就收不到（2026-09-20 海康实测，详见
// `docs/snapshot-flow-design.md` 与技能 `uvp-device-config-family` §19.7）：
//
//   - `multipart/form-data`（**真机形态**）：设备 POST 到 `…/uploads/<token>/?SessionID=…`，
//     路径到 token 就结束、**没有文件名段**，文件名**只出现在部件头**的 Content-Disposition 里。
//   - 裸 JPEG（旧形态：PUT + 路径文件名 + 整包 body）：保留兼容，不作主路径。
//
// 两种形态最后都汇到 `registry.Upload(token, filename, contentType, reader)` 一处做校验与落盘 ——
// "什么算合法图片"只能有一个答案。
func (dc *DeviceMgmtController) UploadDeviceSnapshot(c *gin.Context) {
	registry := dc.captureRuntime()
	if registry == nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	token, pathFilename := resolveDeviceSnapshotUploadTarget(c)
	var file devicecapture.File
	var err error
	if token == "" {
		err = devicecapture.ErrNotFound
	} else {
		file, err = receiveDeviceSnapshotUpload(c, registry, token, pathFilename)
	}
	if err != nil {
		status := http.StatusUnprocessableEntity
		if errors.Is(err, devicecapture.ErrNotFound) {
			status = http.StatusNotFound
		}
		response.SetBusinessResult(c, status, status == 0)
		c.JSON(status, gin.H{"code": status, "message": "抓拍图片接收失败"})
		return
	}
	// 落盘已成功，但**还必须能登记进图像库**：图像库（按通道/时间段/会话找图、按保留期清理）
	// 读的是表，不是目录。登记失败时返回 5xx 让设备重传 —— 重复上传因
	// `(channel_code, file_name)` 唯一键而幂等，不会产生第二行，所以重传是安全的补救手段。
	// ⛔ 反过来（登记失败仍回 200）会让"图在盘上但库里查不到"变成静默状态：
	// 操作员看得见缩略图（走 token 那条路），图像库里却永远找不到这张图。
	snapshotID, err := dc.recordDeviceSnapshot(c, registry, token, file)
	if err != nil {
		app.Log(c.Request.Context()).Error("抓拍图片已落盘但登记图像库失败",
			zap.String("event", "gb28181.snapshot.library_record_failed"),
			zap.String("file", file.Name), zap.String("rel_path", file.RelPath), zap.Error(err))
		response.SetBusinessResult(c, 500, false)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "抓拍图片登记失败"})
		return
	}
	response.SetBusinessResult(c, 0, true)
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"id": snapshotID, "name": file.Name, "size": file.Size}})
}

// recordDeviceSnapshot 把刚落盘的图片登记进 `gb_channel_snapshot`，返回该行主键。
//
// ⭐ 返回主键是**给前端用的稳定地址**：`/api/gb28181/device-mgmt/snapshots/:id/content`。
// 会话 token 那条路（`…/uploads/<token>/<file>`）会随会话过期/重启失效，
// 所以前端拿到 id 后应当立即改用它 —— 图一旦入库，怎么重启都还能取。
//
// ⛔ 归属信息（设备/通道）来自**会话**而不是上传请求：设备 POST 的路径只有 token，
// 报文里也不带通道编码，会话是"这张图属于谁"的唯一出处。
// 查不到平台主键时写 0，不阻塞收图（图片内容已经落盘了，把"关联不上"升级成"收图失败"
// 只会让设备侧看到一个它无法修复的错误）。
func (dc *DeviceMgmtController) recordDeviceSnapshot(c *gin.Context, registry *devicecapture.Registry, token string, file devicecapture.File) (uint, error) {
	session, ok := registry.SessionForToken(token)
	if !ok {
		return 0, devicecapture.ErrNotFound
	}
	row := gbmodels.GbChannelSnapshot{
		ChannelCode: session.ChannelCode,
		FileName:    file.Name,
		RelPath:     file.RelPath,
		Size:        file.Size,
		MD5:         file.MD5,
		Source:      gbmodels.SnapshotSourceDevice,
		// 先按接收时刻兜底，能反解出拍摄时刻就覆盖（见下）。
		CapturedAt: file.ReceivedAt,
	}
	// ⛔ captured_at 优先取**文件名反解**的拍摄时刻：设备补传时它与接收时刻可能差很远，
	// 而图像库的时间轴要回答"什么时候拍的"。反解失败（设备没按 §9.14.1 命名）不是错误路径
	// —— 图片已经在盘上了，不该因为命名不合规就丢掉索引（见 manscdp.ParseSnapshotFileID 注释）。
	if parsed, err := manscdp.ParseSnapshotFileID(file.Name); err == nil {
		row.CapturedAt = parsed.CapturedAt
	}
	if parsed, err := strconv.ParseUint(strings.TrimSpace(session.ChannelID), 10, 64); err == nil {
		row.ChannelID = uint(parsed)
	}
	if strings.TrimSpace(session.ID) != "" {
		sessionID := session.ID
		row.SessionID = &sessionID
	}
	var device gbmodels.GbDevice
	if err := dc.db().WithContext(c.Request.Context()).Select("id").
		Where("device_id = ?", session.DeviceCode).First(&device).Error; err == nil {
		row.DeviceID = device.ID
	}

	// ⛔ 用 OnConflict 而不是"先查后插"：设备重传同一张图（网络抖动很常见）会走到这里，
	// 而唯一键 `(channel_code, file_name)` 就是为它准备的。冲突时**更新**而不是忽略 ——
	// 重传的图会覆盖旧文件（registry.Upload 就是这么写的），库行必须跟着指向新的大小/摘要，
	// 否则库与磁盘不一致。`deleted_at: nil` 顺带把被软删的行复活：重新上传同一张图
	// 语义上就是"这张图又在了"。
	now := time.Now()
	result := dc.db().WithContext(c.Request.Context()).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "channel_code"}, {Name: "file_name"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"device_id": row.DeviceID, "channel_id": row.ChannelID,
			"rel_path": row.RelPath, "size": row.Size, "md5": row.MD5,
			"captured_at": row.CapturedAt, "source": row.Source,
			"session_id": row.SessionID, "deleted_at": nil, "updated_at": now,
		}),
	}).Create(&row)
	if result.Error != nil {
		return 0, result.Error
	}
	// ⛔ 再按唯一键回查一次拿 id，不用 Create 回填的 row.ID：冲突更新时（MySQL 的
	// ON DUPLICATE KEY UPDATE 尤其）回填值不保证是既有行的主键，
	// 而前端要拿这个 id 去拼读图地址 —— 拿错 id 会取到别人的图。
	var stored gbmodels.GbChannelSnapshot
	if err := dc.db().WithContext(c.Request.Context()).
		Select("id").Where("channel_code = ? AND file_name = ?", row.ChannelCode, row.FileName).
		First(&stored).Error; err != nil {
		return 0, err
	}
	return stored.ID, nil
}

// resolveDeviceSnapshotUploadTarget 兼容两种路由注册形态，返回 (token, 路径里的文件名)。
//
//   - catch-all（POST `…/uploads/*token`）：`Param("token")` 形如 `/<token>`、`/<token>/`
//     或 `/<token>/<file>` —— 带前导斜杠，尾斜杠与文件名都得在这里剥。
//   - 具名参数（PUT `…/uploads/:token/:filename`）：`Param("token")` 已是裸 token。
//
// ⛔ 别想着把两种形态统一掉：gin 不允许在同一层同时注册 `:token` 与 `*token`（会 panic），
// 而 PUT/GET 侧已有具名参数在用，所以 POST 只能走 catch-all + 这里手工解析。
func resolveDeviceSnapshotUploadTarget(c *gin.Context) (string, string) {
	filename := c.Param("filename")
	raw := strings.Trim(c.Param("token"), "/")
	if raw == "" {
		return "", ""
	}
	segments := strings.Split(raw, "/")
	if len(segments) > 1 && filename == "" {
		filename = strings.Join(segments[1:], "/")
	}
	return segments[0], filename
}

// receiveDeviceSnapshotUpload 取出图片字节并交给 registry 落盘。按 Content-Type 分流形态。
func receiveDeviceSnapshotUpload(c *gin.Context, registry *devicecapture.Registry, token, pathFilename string) (devicecapture.File, error) {
	contentType := strings.TrimSpace(c.GetHeader("Content-Type"))
	if strings.HasPrefix(strings.ToLower(contentType), "multipart/form-data") {
		return receiveDeviceSnapshotMultipart(c, registry, token)
	}
	// 旧形态：整包 body 就是 JPEG，文件名来自路径。路径里没有文件名就没法对账，直接拒。
	if pathFilename == "" {
		return devicecapture.File{}, devicecapture.ErrInvalidImage
	}
	return registry.Upload(token, pathFilename, contentType, c.Request.Body)
}

// receiveDeviceSnapshotMultipart 走**流式** multipart（`MultipartReader`，不落临时文件），
// 取第一个带文件名的部件。文件名与类型都从**部件头**读 ——
// 设备上传的文件名就是完成通知里 `SnapShotFileID` 去掉 `.jpg`（41 位），这是两者对账的唯一依据。
func receiveDeviceSnapshotMultipart(c *gin.Context, registry *devicecapture.Registry, token string) (devicecapture.File, error) {
	reader, err := c.Request.MultipartReader()
	if err != nil {
		return devicecapture.File{}, devicecapture.ErrInvalidImage
	}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return devicecapture.File{}, devicecapture.ErrInvalidImage
		}
		// ⛔ 判定"是不是文件部件"看 **`FileName()` 是否为空**，而不是死认字段名：
		//    `name="file"` 只是海康自己的选择，字段名不属协议；而**部件头里的 filename
		//    不可省略**（对账要用它），所以缺文件名的部件只能跳过。
		if part.FileName() == "" {
			_ = part.Close()
			continue
		}
		file, err := registry.Upload(token, part.FileName(), part.Header.Get("Content-Type"), part)
		_ = part.Close()
		return file, err
	}
	return devicecapture.File{}, devicecapture.ErrInvalidImage
}

func (dc *DeviceMgmtController) DeviceSnapshotContent(c *gin.Context) {
	registry := dc.captureRuntime()
	if registry == nil {
		c.Status(http.StatusNotFound)
		return
	}
	file, err := registry.Content(c.Param("token"), c.Param("filename"))
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "private, max-age=300")
	c.File(file.Path)
}

// SnapshotContent 是**图像库的稳定读接口**：按 `gb_channel_snapshot.id` 取图。
//
// ⭐ 与 [DeviceMgmtController.DeviceSnapshotContent] 的分工（两条路都要留着）：
//
//	| | 凭证 | 存活期 | 用途 |
//	|---|---|---|---|
//	| `…/device-snapshots/uploads/:token/:filename` | 会话上传令牌 | 随会话（内存态，重启即失效） | 刚下发那一批的**即时**预览 |
//	| `…/device-mgmt/snapshots/:id/content`（本函数） | JWT + `gb28181:device:snapshot` | 与库行同寿 | 图像库、会话面板重载 |
//
// ⛔ 本函数**刻意不返回 302 到静态目录**：那样图片就绕过了鉴权中间件（静态挂载免鉴权，
// 见 bootstrap.deviceCaptureBaseDir 的注释）。文件由 `c.File` 直接从私有目录读出。
//
// ⛔ 权限码走已有的 `gb28181:device:snapshot`（不是更宽的 `*:view`）：一张抓拍图就是一次
// 设备抓拍的产出，能看到"这次抓拍的图"的人才能看到"这张图"；绑 view 会让只读账号也能翻全部画面。
func (dc *DeviceMgmtController) SnapshotContent(c *gin.Context) {
	registry := dc.captureRuntime()
	if registry == nil {
		c.Status(http.StatusNotFound)
		return
	}
	id, err := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || id == 0 {
		c.Status(http.StatusNotFound)
		return
	}
	var row gbmodels.GbChannelSnapshot
	if err := dc.db().WithContext(c.Request.Context()).Select("id", "rel_path").
		Where("id = ?", id).First(&row).Error; err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	path, err := registry.FilePath(row.RelPath)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	// ⛔ 文件不存在时也返回 404 而不是 500：库行与磁盘不一致（手工删过目录、回滚过迁移）
	// 是可预期的运营状态，不是服务故障。
	if _, err := os.Stat(path); err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "private, max-age=300")
	c.File(path)
}

// EscapeLikePattern 转义 LIKE 模式里的通配符，配合 `ESCAPE '!'` 使用。
//
// ⛔ 不转义的话，用户搜 `100%`（或 `_`）会变成"匹配任意串"，界面表现是
// "搜一个具体编码却出来一堆不相干的图"——比不过滤更难排查。
// ⛔ 转义符是 `!` 而不是默认的 `\`：MySQL 里 `NO_BACKSLASH_ESCAPES` 之外
// 的模式下 `\` 会被当作字面量，行为随 sql_mode 漂移；`!` 不与二者冲突。
//
// 导出是因为本包的测试在 `controllers_test` 外部包里：转义字符级行为必须能
// 被单独钉住（换转义符时所有 ESCAPE 子句都要同步改，不同步的表现是"搜不到东西"）。
func EscapeLikePattern(raw string) string {
	replacer := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_")
	return replacer.Replace(raw)
}

// snapshotLibraryRow 是列表查询的扫描行：图像库自己的列 + 从 gb_channel / gb_device 补的名称。
type snapshotLibraryRow struct {
	ID          uint      `gorm:"column:id"`
	DeviceID    uint      `gorm:"column:device_id"`
	ChannelID   uint      `gorm:"column:channel_id"`
	ChannelCode string    `gorm:"column:channel_code"`
	SessionID   *string   `gorm:"column:session_id"`
	FileName    string    `gorm:"column:file_name"`
	Size        int64     `gorm:"column:size"`
	MD5         string    `gorm:"column:md5"`
	CapturedAt  time.Time `gorm:"column:captured_at"`
	Source      string    `gorm:"column:source"`
	CreatedAt   time.Time `gorm:"column:created_at"`
	ChannelName string    `gorm:"column:channel_name"`
	DeviceCode  string    `gorm:"column:device_code"`
	DeviceName  string    `gorm:"column:device_name"`
	// ⛔ 别名是**用户自己起的名字**（`gb_channel.alias` / `gb_device.alias`，
	// 设备上报覆盖不了）。现场人员嘴里的"东门那个枪机"往往只对应 alias，
	 // 而 `name` 是设备自报的厂家串（"IPC-HFW2431S"）。两个都要出：
	// 只给 name 会出现"搜不到东门"，只给 alias 会出现"这名字哪来的"。
	ChannelAlias string    `gorm:"column:channel_alias"`
	DeviceAlias  string    `gorm:"column:device_alias"`
}

// snapshotLibraryVO 是列表接口对外的一行。`url` 是**取图地址**（稳定读接口），
// 前端拿到后直接用，不要在列表里内联图片字节：一个通道一年的图能有上万张。
type snapshotLibraryVO struct {
	ID          uint      `json:"id"`
	DeviceID    uint      `json:"deviceId"`
	ChannelID   uint      `json:"channelId"`
	ChannelCode string    `json:"channelCode"`
	ChannelName string    `json:"channelName"`
	/** 用户自定义通道别名（不被设备上报覆盖）。为空时前端回退到 channelName。 */
	ChannelAlias string    `json:"channelAlias"`
	DeviceCode  string    `json:"deviceCode"`
	DeviceName  string    `json:"deviceName"`
	/** 用户自定义设备别名（不被设备上报覆盖）。为空时前端回退到 deviceName。 */
	DeviceAlias  string    `json:"deviceAlias"`
	SessionID   string    `json:"sessionId,omitempty"`
	FileName    string    `json:"fileName"`
	Size        int64     `json:"size"`
	MD5         string    `json:"md5"`
	CapturedAt  time.Time `json:"capturedAt"`
	Source      string    `json:"source"`
	URL         string    `json:"url"`
	CreatedAt   time.Time `json:"createdAt"`
}

// snapshotLibrarySelect 与 [snapshotLibraryRow] 逐列对应。
//
// ⛔ 显式列名，不用 `s.*`：`s.*` 展开出的 `s.device_id`（平台主键）与旁边的
// `ch.device_id AS device_code`（20 位国标编码）会同时出现，靠 GORM 的命名策略猜映射
// 很容易把两者接反 —— 而前端拿反了就是"用编码当主键去关联"。
const snapshotLibrarySelect = "s.id, s.device_id, s.channel_id, s.channel_code, s.session_id, s.file_name, " +
	"s.size, s.md5, s.captured_at, s.source, s.created_at, " +
	"ch.name AS channel_name, ch.device_id AS device_code, d.name AS device_name, " +
	// ⛔ 两个 alias 都要显式取，别指望 GORM 按名字猜：`ch.alias` 与 `d.alias`
	// 同名，不起 AS 就撞成同一列，而扫描行只有一个字段能接住两个值 ——
	// 表现是"设备别名显示成通道别名"或干脆空着，且不报错。
	"ch.alias AS channel_alias, d.alias AS device_alias"

// ListSnapshots 图像库列表：按通道 / 时间段 / 来源翻历史抓拍图。
//
//	GET /api/gb28181/device-mgmt/snapshots
//	    ?channelId=&channelCode=&deviceCode=&sessionId=&source=&keyword=&from=&to=&page=&pageSize=
//
// 与 [DeviceMgmtController.SnapshotContent] 的分工：本条回答"有哪些图"（只出元数据 + 取图地址），
// 那条回答"这张图的字节"。
// DeleteSnapshots 批量删除抓拍图（老板 2026-10-07 要求）。
//
//	DELETE /api/gb28181/device-mgmt/snapshots   body: {"ids":[1,2]}
//
// ⛔ 语义是**彻底删**：库行物理删除 **且** 删掉磁盘上的 jpg（老板明确选择，不可恢复）。
//
// ⛔ 顺序刻意是「先删文件 → 再删库行」，反过来的话一旦文件删失败就回滚不了库，
// 而文件删不掉时保留库行至少还能在列表里看到这张图（用户可以重试）。
//
// ⛔ 可见性 Scope 必须带：否则能删到别的部门/设备管理范围之外的图。
// ⛔ 文件路径必须走 `registry.FilePath`（它内含**路径穿越**校验：
// `strings.HasPrefix(target, base+sep)`），绝不能直接 `filepath.Join(root, rel)`
// —— rel_path 来自数据库，一旦被篡改成 `../../etc/passwd` 就是任意文件删除。
func (dc *DeviceMgmtController) DeleteSnapshots(c *gin.Context) {
	db := dc.db()
	registry := dc.captureRuntime()
	if db == nil {
		dc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}

	var payload struct {
		IDs []uint64 `json:"ids"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		dc.FailAndAbort(c, "参数不合法", nil)
		return
	}
	// ⛔ 去重 + 限量：重复 id 会让 `IN` 变长且无意义；不限量则一个超大数组
	// 会把整个事务撑爆（每条都要 stat 文件）。
	ids := dedupUint64(payload.IDs)
	if len(ids) == 0 {
		dc.FailAndAbort(c, "请先选择要删除的图片", nil)
		return
	}
	const maxDelete = 200
	if len(ids) > maxDelete {
		dc.FailAndAbort(c, fmt.Sprintf("一次最多删除 %d 张，请分批操作", maxDelete), nil)
		return
	}

	ctx := c.Request.Context()
	// ⛔ 带可见性 Scope 取行：拿不到的行（不存在/越权）**不计入失败**，
	// 直接忽略即可 —— 前端列表本来也只显示看得见的图。
	var rows []gbmodels.GbChannelSnapshot
	// ⛔ 用带别名的可见性 Scope：本表自己也有 device_id/channel_id 列（平台主键），
	// 不带前缀在联表里是歧义列。
	q := db.WithContext(ctx).Select("id", "rel_path").Where("id IN ?", ids).
		Scopes(aliasedChannelVisibleScope(c))
	if err := q.Find(&rows).Error; err != nil {
		dc.FailAndAbort(c, "查询失败", err)
		return
	}
	if len(rows) == 0 {
		dc.Success(c, gin.H{"deleted": 0, "filesFailed": 0})
		return
	}

	// 先删文件。⚠️ 逐个删而不是并发：图片数受 maxDelete 限制，
	// 并发只会让错误处理变复杂，且这里的瓶颈是磁盘 IO。
	//
	// ⛔ registry 为 nil（抓拍存储未装配）时**只删库行**：删文件必须经
	// `registry.FilePath` 做**路径穿越**校验（`strings.HasPrefix(target, base+sep)`），
	// 没有它就只能直接 `filepath.Join(root, rel)` —— 而 rel_path 来自数据库，
	// 一旦被篡改成 `../../etc/passwd` 就是**任意文件删除**。
	// 此时宁可留孤儿文件（可人工清理）也不能开这个口子。
	fileFailed := 0
	if registry == nil {
		zap.S().Warnw("抓拍存储未装配，本次只删除库行，文件保留待人工清理",
			zap.Int("rows", len(rows)))
		fileFailed = len(rows)
	} else {
		for i := range rows {
			path, err := registry.FilePath(rows[i].RelPath)
			if err != nil {
				// ⛔ 路径非法（库里数据被篡改）⇒ **跳过这个文件**，
				// 但库行照删：否则一条坏数据就能让整个批量删不掉。
				zap.S().Errorw("快照相对路径非法，跳过文件删除",
					zap.Uint("id", rows[i].ID), zap.String("rel_path", rows[i].RelPath))
				fileFailed++
				continue
			}
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				// ⛔ 文件不存在（os.IsNotExist）不算失败：库行与磁盘不一致是可预期
				// 的运营状态（手工清过目录），此时仍应把库行删掉，让列表归位。
				zap.S().Errorw("抓拍图文件删除失败",
					zap.Uint("id", rows[i].ID), zap.Error(err))
				fileFailed++
			}
		}
	}

	delIDs := make([]uint64, 0, len(rows))
	for i := range rows {
		delIDs = append(delIDs, uint64(rows[i].ID))
	}
	// ⛔ 物理删除而不是 `deleted_at` 软删：老板明确要求"彻底删、文件也删"，
	// 软删会让这张图在库里永久占位却永远取不到（文件没了）。
	if err := db.WithContext(ctx).Unscoped().Where("id IN ?", delIDs).
		Delete(&gbmodels.GbChannelSnapshot{}).Error; err != nil {
		dc.FailAndAbort(c, "删除失败", err)
		return
	}
	dc.Success(c, gin.H{"deleted": len(delIDs), "filesFailed": fileFailed})
}

// dedupUint64 去重并保持原顺序（批量删除的入参可能重复）。
func dedupUint64(in []uint64) []uint64 {
	seen := make(map[uint64]struct{}, len(in))
	out := make([]uint64, 0, len(in))
	for _, v := range in {
		if v == 0 {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func (dc *DeviceMgmtController) ListSnapshots(c *gin.Context) {
	db := dc.db()
	if db == nil {
		dc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	page := parsePositiveInt(c.DefaultQuery("page", "1"), 1)
	pageSize := parsePositiveInt(c.DefaultQuery("pageSize", "40"), 40)
	if pageSize > 200 {
		pageSize = 200
	}

	q := db.WithContext(c.Request.Context()).
		Table("gb_channel_snapshot AS s").
		Select(snapshotLibrarySelect).
		// ⛔ LEFT JOIN 而不是 INNER JOIN：通道/设备被删之后，**历史图仍是原始凭证**，
		// 不该跟着消失。可见性由下面的 Scope 决定（它作用在 `ch.*` 上，匹配不到通道的行
		// 天然不可见），而不是由 JOIN 类型决定。
		//
		// ⛔⛔ 通道必须走**子查询取唯一行**，不能直接 JOIN（老板 2026-10-07 报
		// 「设备ID通道ID 的值不对」+「勾第一张第二张也跟着勾」+「看着像重复数据」）。
		// 实测根因：**同一个 20 位 `channel_id` 在 `gb_channel` 里有两行**
		// （库行 3539 与 3732 指向同一设备），直连 JOIN 变成一对多 ⇒
		//   1) 同一张图被**渲染两次**（页面出现两张一模一样的卡片）；
		//   2) 两行前端 `:key="item.id"` 相同 ⇒ Vue 复用同一 DOM ⇒ **勾一张选中两张**；
		//   3) 展示的通道主键变成 JOIN 命中的那一行（3732），与图里存的 3539 不一致。
		// ⛔ 归并判据用 `MIN(id)`：**只取一行**，宁可主键偏小也不要行数翻倍
		//（行数翻倍会同时毁掉展示与交互，而主键只用于报障定位）。
		// ⚠️ 同一个编码对应两个**不同**通道行，说明写入侧曾经重复建行，
		// 根治要在通道创建处加唯一约束，不是这里能兜住的。
		Joins(`LEFT JOIN gb_channel AS ch ON ch.id = (
			SELECT MIN(c2.id) FROM gb_channel AS c2
			WHERE c2.channel_id = s.channel_code AND c2.deleted_at IS NULL
		)`).
		Joins("LEFT JOIN gb_device AS d ON d.device_id = ch.device_id AND d.deleted_at IS NULL").
		Where("s.deleted_at IS NULL").
		// ⛔ 用**带别名**的可见性 Scope：本表自己也有 `device_id` 列（平台主键），
		// 不带前缀在联表里是歧义列（见 controllers/deptscope.go 的注释）。
		Scopes(aliasedChannelVisibleScope(c))

	if raw := strings.TrimSpace(c.Query("channelId")); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || id == 0 {
			dc.FailAndAbort(c, "channelId 不合法", err)
			return
		}
		q = q.Where("s.channel_id = ?", uint(id))
	}
	if code := strings.TrimSpace(c.Query("channelCode")); code != "" {
		q = q.Where("s.channel_code = ?", code)
	}
	// ⛔ 参数名是 `deviceCode`（20 位国标编码）而不是 `deviceId`：图像库表里的 `device_id`
	// 是**平台主键**，而设备/通道列表接口的 `deviceId` 参数一直是**20 位编码**（见 ListChannels）。
	// 同一个名字两种语义只会让调用方接错，所以这里用编码命名，并且查的是 `ch.device_id`（编码列）。
	if code := strings.TrimSpace(c.Query("deviceCode")); code != "" {
		q = q.Where("ch.device_id = ?", code)
	}
	// 会话面板"从库重载"就靠这个参数：内存里的 Registry 刷新即丢，图还在就得能按会话重聚。
	if sessionID := strings.TrimSpace(c.Query("sessionId")); sessionID != "" {
		q = q.Where("s.session_id = ?", sessionID)
	}
	if source := strings.TrimSpace(c.Query("source")); source != "" {
		if !validSnapshotSource(source) {
			dc.FailAndAbort(c, "source 不合法", nil)
			return
		}
		q = q.Where("s.source = ?", source)
	}
	// 名称/编码模糊搜。前端**不能**自己过滤：分页发生在这一层，只在页内过滤的话
	// 「搜到 0 条」可能只是目标在第 7 页，而界面看上去就是"没这张图"。
	if keyword := strings.TrimSpace(c.Query("keyword")); keyword != "" {
		// ⛔ `%`/`_` 是 LIKE 的通配符，必须转义后再拼，否则用户搜 `100%` 会匹配到一切。
		like := "%" + EscapeLikePattern(keyword) + "%"
		// ⛔ 名称列取自 LEFT JOIN 的 ch/d，与可见性 Scope 命中的是同一批行；
		// 编码列用 `s.channel_code`（抓拍当时记下的快照，不是 ch 表当前值），
		// 这样"设备改名之前拍的图"仍能按旧编码搜到。
		q = q.Where(
			"ch.name LIKE ? ESCAPE '!' OR d.name LIKE ? ESCAPE '!' OR s.channel_code LIKE ? ESCAPE '!' OR ch.device_id LIKE ? ESCAPE '!' "+
				"OR ch.alias LIKE ? ESCAPE '!' OR d.alias LIKE ? ESCAPE '!'",
			like, like, like, like, like, like)
	}
	from, err := parseOptionalTime(c.Query("from"))
	if err != nil {
		dc.FailAndAbort(c, "from 时间格式不合法", err)
		return
	}
	to, err := parseOptionalTime(c.Query("to"))
	if err != nil {
		dc.FailAndAbort(c, "to 时间格式不合法", err)
		return
	}
	if from != nil && to != nil && from.After(*to) {
		dc.FailAndAbort(c, "from 不能晚于 to", nil)
		return
	}
	// ⛔ 时间条件打在 **captured_at**（拍摄时刻，从文件名反解）而不是 created_at（接收时刻）：
	// 设备补传时两者可能差几小时，而操作员找图时说的是"几点拍的"。
	if from != nil {
		q = q.Where("s.captured_at >= ?", *from)
	}
	if to != nil {
		q = q.Where("s.captured_at <= ?", *to)
	}

	var total int64
	if err := q.Select("s.id").Count(&total).Error; err != nil {
		dc.FailAndAbort(c, "统计抓拍图片失败", err)
		return
	}
	var rows []snapshotLibraryRow
	if err := q.Select(snapshotLibrarySelect).
		Order("s.captured_at DESC, s.id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&rows).Error; err != nil {
		dc.FailAndAbort(c, "查询抓拍图片失败", err)
		return
	}

	list := make([]snapshotLibraryVO, 0, len(rows))
	for _, row := range rows {
		view := snapshotLibraryVO{
			ID: row.ID, DeviceID: row.DeviceID, ChannelID: row.ChannelID,
			ChannelCode: row.ChannelCode, ChannelName: row.ChannelName, ChannelAlias: row.ChannelAlias,
			DeviceCode: row.DeviceCode, DeviceName: row.DeviceName, DeviceAlias: row.DeviceAlias,
			FileName: row.FileName, Size: row.Size, MD5: row.MD5,
			CapturedAt: row.CapturedAt, Source: row.Source, CreatedAt: row.CreatedAt,
			URL: snapshotContentURL(row.ID),
		}
		if row.SessionID != nil {
			view.SessionID = *row.SessionID
		}
		list = append(list, view)
	}
	dc.Success(c, gin.H{"list": list, "total": total, "page": page, "pageSize": pageSize})
}

// snapshotContentURL 由库行 id 拼出取图地址。
// ⛔ 不做字符串拼接而是替换常量里的 `:id`：路由注册、迁移登记的 sys_api、前端拿到手的 url
// 都派生自同一个常量，改路径时不会漏掉某一处（漏掉的表现是"取图恒 404"）。
// `Replace` 只换第一处，恰好覆盖"路径里只有一个 `:id`"这个前提。
func snapshotContentURL(id uint) string {
	return strings.Replace(gbmodels.SnapshotLibraryAPIPath, ":id", strconv.FormatUint(uint64(id), 10), 1)
}

// validSnapshotSource 校验 source 取值。⛔ 白名单而不是"非空即可"：
// 传错来源返回空列表会让人以为"这段时间没抓拍过"，把参数错误伪装成业务事实。
func validSnapshotSource(source string) bool {
	switch source {
	case gbmodels.SnapshotSourceDevice, gbmodels.SnapshotSourceZLM, gbmodels.SnapshotSourceBrowser:
		return true
	default:
		return false
	}
}

func snapshotUploadURL(c *gin.Context, token string) (string, error) {
	scheme := strings.TrimSpace(strings.Split(c.GetHeader("X-Forwarded-Proto"), ",")[0])
	if scheme == "" {
		scheme = "http"
		if c.Request.TLS != nil {
			scheme = "https"
		}
	}
	host := strings.TrimSpace(strings.Split(c.GetHeader("X-Forwarded-Host"), ",")[0])
	if host == "" {
		host = c.Request.Host
	}
	if host == "" || scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("无法生成设备可访问的抓拍上传地址")
	}
	if snapshotUploadHostUnreachableForDevice(host) {
		return "", fmt.Errorf(
			"抓拍上传地址 %s 不是设备可达的地址（回环/通配地址设备访问不到）；请用平台所在机器的局域网 IP 或域名打开平台后再下发",
			host)
	}
	return (&url.URL{Scheme: scheme, Host: host, Path: "/api/gb28181/device-snapshots/uploads/" + token + "/"}).String(), nil
}

// snapshotUploadHostUnreachableForDevice 报告 host 是不是**设备在物理上不可能访问**的地址。
//
// ⛔ 这个门禁与"报文分派"同等重要，因为 `UploadURL` 是给**另一台机器**（设备）回连用的，
// 而它现在由**浏览器请求的 Host** 派生 —— 前端 dev server 的 `xfwd: true` 会把原始 Host
// 放进 `X-Forwarded-Host`（见 `web/vite.config.ts`）。于是"操作员用 localhost 打开平台"
// 就会下发出 `http://localhost:<port>/…`：设备把它解析成自己，**永远传不上来**。
//
// 更糟的是平台侧**没有任何错误**：会话停在 waiting，日志正常，唯一的现象就是
// "设备没上传" —— 与 2026-09-20 那次"抓拍根本没发出去"属同一类静默失效
// （见 `ptz.ActionSnapshotConfig` 的注释）。所以宁可当场拒发并说清原因，
// 也不要发一个注定失败的上传地址。
//
// ⭐ 域名一律放行：平台无法判定它解析到哪里，判错了会把正常部署挡在门外。
func snapshotUploadHostUnreachableForDevice(hostport string) bool {
	host := strings.TrimSpace(hostport)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	// IPv6 字面量在 Host 头里带方括号（`[::1]:8280`），解出来要去掉。
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" || strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	// IsUnspecified 覆盖 `0.0.0.0` / `::` —— 那也不是一个目标地址。
	return ip.IsLoopback() || ip.IsUnspecified()
}

func snapshotSessionView(c *gin.Context, session devicecapture.Session, operationID string) gin.H {
	files := make([]gin.H, 0, len(session.Files))
	for _, file := range session.Files {
		path := "/api/gb28181/device-snapshots/uploads/" + session.UploadToken + "/" + url.PathEscape(file.Name)
		files = append(files, gin.H{"name": file.Name, "size": file.Size, "receivedAt": file.ReceivedAt, "url": path})
	}
	view := gin.H{
		"sessionId": session.ID, "channelId": session.ChannelID, "channelCode": session.ChannelCode,
		"deviceCode": session.DeviceCode, "snapNum": session.SnapNum, "interval": session.Interval,
		"state": session.State, "receivedCount": len(session.Files), "notifiedCount": session.NotifiedCount,
		"files": files, "error": session.Error, "createdAt": session.CreatedAt, "updatedAt": session.UpdatedAt,
	}
	if operationID != "" {
		view["operationId"] = operationID
	}
	return view
}
