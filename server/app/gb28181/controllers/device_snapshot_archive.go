package controllers

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/gb28181/devicecapture"
)

// 批量下载的上限。⛔ 三个都是**必要的刹车**而不是保守估计：
//   - 张数：每条都要 stat 一次磁盘，200 张以外的时间全花在随机 IO 上；
//   - 字节数：zip 是流式的，理论上能吐几个 G，但这台机器同时还在转发视频流，
//     一次几十 G 的打包会把磁盘带宽吃光，表现是"所有人的预览都在卡"；
//   - 单次连接时长：由 Web 服务器的写超时兜底，这里不重复设。
const (
	maxSnapshotArchiveItems = 200
	maxSnapshotArchiveBytes = int64(2) << 30 // 2 GiB
)

// errSnapshotArchiveTooLarge 与普通查询错误分开：它要回给用户一句"分批下载"，
// 而不是笼统的"打包失败"（后者会让运维去翻日志找一个根本不存在的问题）。
var errSnapshotArchiveTooLarge = errors.New("snapshot archive exceeds size limit")

// snapshotArchiveEntry 是**预检通过**的待打包项。
//
// ⛔ 先预检再写响应体：zip 一旦开始往 `c.Writer` 写，HTTP 头就已经发出去了，
// 此时才发现"一张图都不存在"只能给客户端一个空 zip（浏览器照样提示下载成功），
// 而用户以为"图没了"。所以库行查询、路径校验、stat、体积累计全部前置。
type snapshotArchiveEntry struct {
	id         uint
	absPath    string
	name       string
	capturedAt time.Time
}

// snapshotArchiveRow 是打包预检要用的行：路径 + 命名素材（通道标签、拍摄时刻）。
type snapshotArchiveRow struct {
	ID           uint      `gorm:"column:id"`
	RelPath      string    `gorm:"column:rel_path"`
	CapturedAt   time.Time `gorm:"column:captured_at"`
	ChannelCode  string    `gorm:"column:channel_code"`
	ChannelName  string    `gorm:"column:channel_name"`
	ChannelAlias string    `gorm:"column:channel_alias"`
}

// ArchiveSnapshots 把选中的抓拍图打成**一个 zip** 流式下载（老板 2026-11-04 要求）。
//
//	GET /api/gb28181/device-mgmt/snapshots/archive?ids=1,2,3[&ids=4][&preflight=1]
//
// ⭐ 为什么必须由后端打包（而不是前端 fetch 每张图再拼 zip）：
//   - 前端拼包要把**全部原图**先攒进标签页内存，200 张 3MB 的图就是 600MB，
//     手机和低配 PC 直接崩；
//   - 逐张触发 `<a download>`（改造前的做法）会被浏览器当成弹窗批量拦截，
//     表现是"提示已下载 20 张，下载目录里只有 8 个"。
//
// ⛔ 凭证走 `?token=`（与取图接口同一条路）：`<a download>` 带不了 Authorization 头，
// 而这条接口在鉴权组内。前端复用 `snapshotContentImageUrl` 的拼 token 逻辑。
//
// ⭐ `preflight=1` 只回 JSON（`{count, skipped, bytes}`）不打包：`<a download>` 这种方式
// **拿不到失败状态**——后端一旦回 400，浏览器会把那段 JSON 当文件存下来，
// 用户看到的是下载目录里多一个 `snapshots-20261104.zip`（其实是错误信息）或者干脆
// 一个空白标签页。所以前端先用它问一句"这 N 张能打包吗"，确认后才真触发下载。
//
// ⛔ 单张失败（文件被手工删了 / rel_path 被篡改）**跳过并继续**，不中断整包：
// 中断的后果是用户拿到一个半截 zip —— 解压时报"文件损坏"，却看不出是少哪张。
// 跳过的张数进日志，包照常下完。
func (dc *DeviceMgmtController) ArchiveSnapshots(c *gin.Context) {
	db := dc.db()
	registry := dc.captureRuntime()
	if db == nil {
		dc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	// ⛔ 抓拍存储没装配时**不允许**用 `filepath.Join(root, rel_path)` 兜底：
	// rel_path 来自数据库，一旦被篡改成 `../../etc/passwd` 就是把任意文件打进包里送出去。
	if registry == nil {
		dc.FailAndAbort(c, "抓拍存储未就绪", nil)
		return
	}

	ids := parseSnapshotArchiveIDs(c.QueryArray("ids"))
	if len(ids) == 0 {
		dc.FailAndAbort(c, "请先选择要下载的图片", nil)
		return
	}
	if len(ids) > maxSnapshotArchiveItems {
		dc.FailAndAbort(c, fmt.Sprintf("一次最多打包下载 %d 张，请分批操作", maxSnapshotArchiveItems), nil)
		return
	}

	entries, skipped, totalBytes, err := dc.collectSnapshotArchiveEntries(c, db, registry, ids)
	if errors.Is(err, errSnapshotArchiveTooLarge) {
		dc.FailAndAbort(c, fmt.Sprintf("选中的图片合计超过 %d GB，请分批下载", maxSnapshotArchiveBytes>>30), nil)
		return
	}
	if err != nil {
		dc.FailAndAbort(c, "打包失败", err)
		return
	}
	if len(entries) == 0 {
		// ⛔ 空包不是"成功"：库里还有行、磁盘上却没有文件是可预期的运营状态
		// （手工清过目录），给空 zip 等于骗用户"下载成功了"。
		dc.FailAndAbort(c, "选中的图片文件都不在服务器上", nil)
		return
	}

	if c.Query("preflight") == "1" {
		dc.Success(c, gin.H{
			"count": len(entries), "skipped": skipped, "bytes": totalBytes,
		})
		return
	}

	stamp := time.Now().Format("20060102-150405")
	// ⛔ 同时给 ASCII 与 RFC 5987 两份文件名：老浏览器（以及部分下载工具）不认 `filename*`，
	// 只给中文名时会退化成 URL 最后一段（一串乱码且丢扩展名）。
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", fmt.Sprintf(
		"attachment; filename=%q; filename*=UTF-8''%s",
		"snapshots-"+stamp+".zip", url.PathEscape("抓拍图-"+stamp+".zip")))
	// ⛔ 不设 Content-Length（流式，边读边吐）；也**不要**加 `Cache-Control: public`，
	// 图片是受控资源，缓存到中间代理等于越权暴露。
	c.Header("Cache-Control", "private, no-store")

	zw := zip.NewWriter(c.Writer)
	written, failed := 0, 0
	for i := range entries {
		if !appendSnapshotZipEntry(zw, entries[i]) {
			failed++
			continue
		}
		written++
	}
	if err := zw.Close(); err != nil {
		// 客户端中途关掉下载（点了取消）也会走到这里，不算服务故障。
		zap.S().Infow("抓拍图 zip 收尾失败（可能是客户端提前断开）",
			zap.Int("written", written), zap.Error(err))
		return
	}
	if failed > 0 || skipped > 0 {
		zap.S().Warnw("图像库打包下载跳过了部分文件",
			zap.Int("requested", len(ids)), zap.Int("written", written),
			zap.Int("preflight_skipped", skipped), zap.Int("write_failed", failed))
	}
}

// collectSnapshotArchiveEntries 查询 + 预检，返回可打包项 / 跳过张数 / 合计字节。
//
// ⛔ 可见性 Scope 必须带：否则能把别的部门/设备管理范围之外的图打包带走
// （而列表页看不到它们，异常更难被发现）。
func (dc *DeviceMgmtController) collectSnapshotArchiveEntries(
	c *gin.Context, db *gorm.DB, registry *devicecapture.Registry, ids []uint64,
) ([]snapshotArchiveEntry, int, int64, error) {
	var rows []snapshotArchiveRow
	err := db.WithContext(c.Request.Context()).
		Table("gb_channel_snapshot AS s").
		Select("s.id, s.rel_path, s.captured_at, s.channel_code, " +
			"ch.name AS channel_name, ch.alias AS channel_alias").
		// 通道只用于**取名字**，不参与过滤；被删的通道也照样出历史图（同列表接口口径）。
		Joins(snapshotChannelJoin).
		Where("s.deleted_at IS NULL").
		Where("s.id IN ?", ids).
		Scopes(aliasedChannelVisibleScope(c)).
		Order("s.captured_at ASC, s.id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, 0, 0, err
	}

	entries := make([]snapshotArchiveEntry, 0, len(rows))
	var total int64
	// ids 里可能有"越权/已删"的 id，它们根本不会出现在 rows 里 —— 也算跳过，
	// 否则"选 10 张、包里 8 张"没有任何痕迹可查。
	skipped := len(ids) - len(rows)
	used := make(map[string]int, len(rows))

	for i := range rows {
		row := &rows[i]
		// ⛔ 路径必须走 registry.FilePath：它内含**路径穿越**校验，rel_path 来自数据库，
		// 不能直接 filepath.Join。
		path, err := registry.FilePath(row.RelPath)
		if err != nil {
			zap.S().Errorw("快照相对路径非法，打包时跳过",
				zap.Uint("id", row.ID), zap.String("rel_path", row.RelPath))
			skipped++
			continue
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			// 文件不在（手工清过目录）是运营常态，不升级成错误。
			skipped++
			continue
		}
		total += info.Size()
		if total > maxSnapshotArchiveBytes {
			return nil, skipped, total, errSnapshotArchiveTooLarge
		}
		entries = append(entries, snapshotArchiveEntry{
			id:         row.ID,
			absPath:    path,
			name:       nextSnapshotArchiveName(used, row),
			capturedAt: row.CapturedAt,
		})
	}
	return entries, skipped, total, nil
}

// nextSnapshotArchiveName 生成 zip 内文件名并**保证同包内不重名**。
//
// ⛔ 重名会被解压工具覆盖：同一通道连拍（间隔 1 秒）大概率落在同一秒里，
// 用户解压完发现"拍了 10 张只剩 8 个文件"，而且没有任何提示。
// 去重靠 `-2 / -3` 后缀，不改时间戳（时间戳是用户认图的主要线索）。
func nextSnapshotArchiveName(used map[string]int, row *snapshotArchiveRow) string {
	// 通道标签：别名 → 名称 → 国标编码 → 未知通道（与前端 snapshotChannelLabel 同序）。
	label := firstNonBlankText(row.ChannelAlias, row.ChannelName, row.ChannelCode, "未知通道")
	var base string
	if !row.CapturedAt.IsZero() && row.CapturedAt.Year() > 1 {
		base = label + "_" + row.CapturedAt.Format("20060102-150405")
	} else {
		// 拍摄时刻解析不出来（文件名不合规）时用行 id 兜底：至少不重名。
		base = fmt.Sprintf("%s_%d", label, row.ID)
	}
	base = sanitizeSnapshotArchiveName(base)

	name := base + ".jpg"
	if n := used[name]; n > 0 {
		used[name] = n + 1
		name = fmt.Sprintf("%s-%d.jpg", base, n+2)
	}
	// 每条都要登记：第三张起如果不登记，第四张还会撞同一个 `-2`。
	used[name] = 1
	return name
}

// sanitizeSnapshotArchiveName 清掉文件系统不允许的字符。
//
// ⛔ Windows 除了 `<>:"/\|?*` 还禁止**结尾的点与空格**（`TrimRight` 那一行）：
// 不清的话解压时报"文件名无效"，整包可能被解压工具判成损坏。
// ⛔ 截断按 **rune** 不按 byte：中文名按 byte 截会把一个汉字劈成半个，
// zip 里就是非法 UTF-8（Windows 上显示成 `?`）。
func sanitizeSnapshotArchiveName(raw string) string {
	replacer := strings.NewReplacer(
		`\`, "_", "/", "_", ":", "_", "*", "_", "?", "_",
		`"`, "_", "<", "_", ">", "_", "|", "_")
	out := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return '_'
		}
		return r
	}, replacer.Replace(raw))
	out = strings.TrimRight(strings.TrimSpace(out), ". ")
	if out == "" {
		return "snapshot"
	}
	if runes := []rune(out); len(runes) > 80 {
		out = string(runes[:80])
	}
	return out
}

// parseSnapshotArchiveIDs 解析 `ids=1,2,3` 与 `ids=1&ids=2` 两种写法。
//
// ⛔ 逗号与重复参数都要认：前端用逗号拼（URL 短），而 curl / SDK 调用方
// 习惯重复同名参数。只认一种的表现是"调用方传了却被判成没选图"。
// ⛔ 非法值**跳过而不是整体报错**：一个 0/负数/字母不该让整批下载失败。
func parseSnapshotArchiveIDs(raw []string) []uint64 {
	values := make([]uint64, 0, len(raw))
	for _, chunk := range raw {
		for _, piece := range strings.Split(chunk, ",") {
			piece = strings.TrimSpace(piece)
			if piece == "" {
				continue
			}
			id, err := strconv.ParseUint(piece, 10, 64)
			if err != nil || id == 0 {
				continue
			}
			values = append(values, id)
		}
	}
	// 复用批量删除那套去重（同批次里重复 id 会让 IN 变长且无意义）。
	return dedupUint64(values)
}

// appendSnapshotZipEntry 把一张图写进 zip。返回 false 表示这张被跳过（不中断整包）。
//
// ⛔ 用 `zip.Store`（不压缩）而不是默认的 Deflate：抓拍图是 JPEG，本身已经压过了，
// 再 Deflate 一次平均只省 1~2% 的体积，却要多花几倍 CPU —— 200 张图的打包时间
// 从"磁盘读多久就多久"变成"CPU 算多久"，还会和视频转发抢 CPU。
func appendSnapshotZipEntry(zw *zip.Writer, entry snapshotArchiveEntry) bool {
	file, err := os.Open(entry.absPath)
	if err != nil {
		zap.S().Warnw("抓拍图打包时打开文件失败，已跳过",
			zap.Uint("id", entry.id), zap.Error(err))
		return false
	}
	defer func() { _ = file.Close() }()

	// ⛔ 文件名直接给 UTF-8 原文：archive/zip 会自己检测非 ASCII 并置上 UTF-8 标志位
	// （0x800）。手工置 `NonUTF8: true` 的话，Windows 资源管理器解压出来是乱码（GBK 误解）。
	header := &zip.FileHeader{Name: entry.name, Method: zip.Store}
	if !entry.capturedAt.IsZero() && entry.capturedAt.Year() > 1 {
		header.Modified = entry.capturedAt
	}
	writer, err := zw.CreateHeader(header)
	if err != nil {
		zap.S().Warnw("抓拍图打包时创建 zip 条目失败", zap.Uint("id", entry.id), zap.Error(err))
		return false
	}
	if _, err := io.Copy(writer, file); err != nil {
		// 读盘 IO 错（坏道 / 文件被并发删除）时这一条已经不完整了，但 zip 流无法回退：
		// 只能记日志，让日志里能回答"为什么少了这张"。
		zap.S().Warnw("抓拍图打包时读取文件失败（该张在包里不完整）",
			zap.Uint("id", entry.id), zap.Error(err))
		return false
	}
	return true
}

// firstNonBlankText 返回第一个非空（trim 后）的候选值。
// ⛔ 逐级 trim 而不是 `a != "" ? a : b`：别名可能是一串空格（用户清空时留下的），
// 空格是 truthy，串起来会让整列变成空白。
func firstNonBlankText(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
