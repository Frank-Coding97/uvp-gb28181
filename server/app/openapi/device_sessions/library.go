package devicesessions

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

// 图像库（gb_channel_snapshot）的对外读取与删除。
//
// ⭐ 与 `device_sessions` 的抓图面是**同一份数据的两端**：抓图会话按 `sessionId` 只看
// "这一次抓到了哪几张"，图像库按通道 + 时间段看"历史上都有哪些图"。两者共用
// `registry.FilePath`（含路径穿越校验）与同一张表，不另建一套归属判断。
//
// ⛔ 与抓图会话的**关键语义差异**：抓图必须设备在线（要下发命令），图像库**不需要**
// —— 图已经在盘上了，设备离线照样要能翻历史、照样要能删。所以这里走
// `resolveChannelScope`（不带在线门禁），绝不能复用 `resolveTarget`。
const (
	maxLibraryPageSize   = 100
	maxLibraryIDs        = 100
	maxLibraryImageBytes = 8 << 20
)

// LibraryListOptions 是列表筛选条件。时间窗打在 captured_at（拍摄时刻）而不是 created_at
// （平台接收时刻）：设备补传时两者能差几小时，而调用方问的是"什么时候拍的"。
type LibraryListOptions struct {
	Page     int
	PageSize int
	From     string
	To       string
	Source   string
}

// LibraryImage 是图像库里的一张图。只出元数据 + 取图地址，不内联图片字节：
// 一个通道一年的图能有上万张，内联会让分页响应膨胀到不可用。
type LibraryImage struct {
	SnapshotID  uint64    `json:"snapshotId"`
	SessionID   string    `json:"sessionId,omitempty"`
	FileName    string    `json:"fileName"`
	Size        int64     `json:"size"`
	MD5         string    `json:"md5"`
	Source      string    `json:"source"`
	CapturedAt  time.Time `json:"capturedAt"`
	ReceivedAt  time.Time `json:"receivedAt"`
	ContentPath string    `json:"contentUrl"`
}

type LibraryPage struct {
	Items    []LibraryImage `json:"items"`
	Page     int            `json:"page"`
	PageSize int            `json:"pageSize"`
	Total    int64          `json:"total"`
}

// LibraryDeleteResult 是批量删除的结果。
// `missing` 汇总"不在本通道可见范围内或已不存在"的数量，**不区分两者**：
// 要求区分就等于提供了一个探测其他通道图片主键是否存在的接口。
type LibraryDeleteResult struct {
	Deleted     int `json:"deleted"`
	FilesFailed int `json:"filesFailed"`
	Missing     int `json:"missing"`
}

// ListLibrary 按通道翻历史抓拍图。
func (s *Service) ListLibrary(ctx context.Context, scope resource.DepartmentScope, deviceID, channelID string, options LibraryListOptions) (LibraryPage, error) {
	if s == nil || s.db == nil || ctx == nil {
		return LibraryPage{}, ErrUnavailable
	}
	_, channel, err := s.resolveChannelScope(ctx, scope, deviceID, channelID)
	if err != nil {
		return LibraryPage{}, err
	}
	page := options.Page
	if page == 0 {
		page = 1
	}
	pageSize := options.PageSize
	if pageSize == 0 {
		pageSize = 20
	}
	if page < 1 || pageSize < 1 || pageSize > maxLibraryPageSize || page-1 > int(^uint(0)>>1)/pageSize {
		return LibraryPage{}, ErrInvalidRequest
	}
	from, err := parseLibraryTime(options.From)
	if err != nil {
		return LibraryPage{}, ErrInvalidRequest
	}
	to, err := parseLibraryTime(options.To)
	if err != nil {
		return LibraryPage{}, ErrInvalidRequest
	}
	if from != nil && to != nil && from.After(*to) {
		return LibraryPage{}, ErrInvalidRequest
	}
	source := strings.TrimSpace(options.Source)
	if source != "" && !validLibrarySource(source) {
		return LibraryPage{}, ErrInvalidRequest
	}

	query := s.db.WithContext(ctx).Model(&gbmodels.GbChannelSnapshot{}).
		Where("channel_code = ? AND deleted_at IS NULL", channel.ChannelID)
	if from != nil {
		query = query.Where("captured_at >= ?", *from)
	}
	if to != nil {
		query = query.Where("captured_at <= ?", *to)
	}
	if source != "" {
		query = query.Where("source = ?", source)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return LibraryPage{}, ErrUnavailable
	}
	var rows []gbmodels.GbChannelSnapshot
	if err := query.Order("captured_at DESC, id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		return LibraryPage{}, ErrUnavailable
	}
	items := make([]LibraryImage, 0, len(rows))
	for _, row := range rows {
		items = append(items, libraryImage(row, deviceID, channelID))
	}
	return LibraryPage{Items: items, Page: page, PageSize: pageSize, Total: total}, nil
}

// ReadLibraryImage 按图像库主键取图。
//
// ⛔ 与 [Service.ReadImage] 的区别：那条只认本客户端抓图会话产生的图（会话 + 通道 +
// 文件名三重校验），这条认整个通道的图像库。两条都保留：会话面回答"我这次抓的图"，
// 图像库面回答"这个通道历史上的图"。
func (s *Service) ReadLibraryImage(ctx context.Context, scope resource.DepartmentScope, deviceID, channelID string, snapshotID uint64) (ImageContent, error) {
	if s == nil || s.db == nil || s.captures == nil || ctx == nil || snapshotID == 0 {
		return ImageContent{}, ErrNotFound
	}
	_, channel, err := s.resolveChannelScope(ctx, scope, deviceID, channelID)
	if err != nil {
		return ImageContent{}, err
	}
	var row gbmodels.GbChannelSnapshot
	result := s.db.WithContext(ctx).
		Where("id = ? AND channel_code = ? AND deleted_at IS NULL", snapshotID, channel.ChannelID).
		Limit(1).Find(&row)
	if result.Error != nil {
		return ImageContent{}, ErrUnavailable
	}
	if result.RowsAffected != 1 {
		return ImageContent{}, ErrNotFound
	}
	// ⛔ 上限检查放在读盘之前：一张被替换成超大文件的图不该先被读进内存再被拒。
	// 归类为"取不到"而不是"服务故障"：超过 8MB 的单张抓拍图只可能是库行与磁盘被
	// 人为换过，重试不会变好，返回 503 反而会让调用方误以为平台在抖动。
	if row.Size > maxLibraryImageBytes {
		return ImageContent{}, ErrNotFound
	}
	path, err := s.captures.FilePath(row.RelPath)
	if err != nil {
		// 路径非法（库里数据被篡改）与文件不存在都按"这张图取不到"处理：
		// 对调用方而言它们都是可预期的运营状态，不是服务故障。
		return ImageContent{}, ErrNotFound
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return ImageContent{}, ErrNotFound
	}
	if int64(len(payload)) != row.Size || len(payload) > maxLibraryImageBytes {
		return ImageContent{}, ErrUnavailable
	}
	// ⛔ md5 为空的历史行跳过摘要校验（老数据没有摘要），非空必须一致：
	// 不一致说明磁盘上的文件已经不是这一行登记的那张图，宁可报错也不返回错图。
	if strings.TrimSpace(row.MD5) != "" {
		digest := md5.Sum(payload)
		if !strings.EqualFold(hex.EncodeToString(digest[:]), row.MD5) {
			return ImageContent{}, ErrUnavailable
		}
	}
	return ImageContent{FileName: row.FileName, ContentType: "image/jpeg", MD5: row.MD5, Bytes: payload}, nil
}

// DeleteLibraryImages 批量删除本通道的图像相库行与磁盘文件。
//
// ⛔ 顺序刻意是「先删文件 → 再删库行」（与平台 [DeleteSnapshots] 一致）：
// 反过来的话一旦文件删失败就回滚不了库，而文件删不掉时保留库行至少还能在列表里看到
// 这张图、调用方还能重试。文件删除失败**不影响**库行删除，失败数单独回报。
//
// ⛔ 文件路径必须走 `captures.FilePath`（内含路径穿越校验），绝不能自己
// `filepath.Join(root, rel)` —— rel_path 来自数据库，被篡改成 `../../etc/passwd`
// 就是任意文件删除。
func (s *Service) DeleteLibraryImages(ctx context.Context, scope resource.DepartmentScope, deviceID, channelID string, ids []uint64) (LibraryDeleteResult, error) {
	if s == nil || s.db == nil || s.captures == nil || ctx == nil {
		return LibraryDeleteResult{}, ErrUnavailable
	}
	if len(ids) == 0 || len(ids) > maxLibraryIDs {
		return LibraryDeleteResult{}, ErrInvalidRequest
	}
	unique := make([]uint64, 0, len(ids))
	seen := make(map[uint64]struct{}, len(ids))
	for _, id := range ids {
		if id == 0 {
			return LibraryDeleteResult{}, ErrInvalidRequest
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	_, channel, err := s.resolveChannelScope(ctx, scope, deviceID, channelID)
	if err != nil {
		return LibraryDeleteResult{}, err
	}

	// ⛔ 只在本通道范围内取行：删除请求里的 id 属于别的通道时**查不出来**，
	// 于是既不会被删、也不会泄露它在别处存在（统一进 missing）。
	var rows []gbmodels.GbChannelSnapshot
	if err := s.db.WithContext(ctx).
		Select("id", "rel_path").
		Where("channel_code = ? AND deleted_at IS NULL AND id IN ?", channel.ChannelID, unique).
		Find(&rows).Error; err != nil {
		return LibraryDeleteResult{}, ErrUnavailable
	}
	result := LibraryDeleteResult{Missing: len(unique) - len(rows)}
	if len(rows) == 0 {
		return result, nil
	}

	fileFailed := 0
	for i := range rows {
		path, err := s.captures.FilePath(rows[i].RelPath)
		if err != nil {
			// ⛔ 路径非法 ⇒ 跳过这个文件但**库行照删**：否则一条坏数据能让整批删不掉。
			fileFailed++
			continue
		}
		// ⛔ 文件不存在不算失败：库行与磁盘不一致（手工清过目录、回滚过迁移）
		// 是可预期的运营状态，此时仍应删掉库行让列表归位。
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			fileFailed++
		}
	}
	deleteIDs := make([]uint64, 0, len(rows))
	for i := range rows {
		deleteIDs = append(deleteIDs, uint64(rows[i].ID))
	}
	// ⛔ 物理删除而不是软删：契约对外承诺的是"文件与记录一并删除"，
	// 软删会让这张图在库里永久占位却永远取不到（文件已经没了）。
	if err := s.db.WithContext(ctx).Unscoped().Where("id IN ?", deleteIDs).
		Delete(&gbmodels.GbChannelSnapshot{}).Error; err != nil {
		return LibraryDeleteResult{}, ErrUnavailable
	}
	result.Deleted = len(deleteIDs)
	result.FilesFailed = fileFailed
	return result, nil
}

func libraryImage(row gbmodels.GbChannelSnapshot, deviceID, channelID string) LibraryImage {
	image := LibraryImage{
		SnapshotID: uint64(row.ID), FileName: row.FileName, Size: row.Size, MD5: row.MD5,
		Source: row.Source, CapturedAt: row.CapturedAt, ReceivedAt: row.CreatedAt,
		ContentPath: fmt.Sprintf("/openapi/v1/devices/%s/channels/%s/snapshot-library/%d/content",
			deviceID, channelID, row.ID),
	}
	if row.SessionID != nil {
		image.SessionID = *row.SessionID
	}
	return image
}

func validLibrarySource(source string) bool {
	switch source {
	case gbmodels.SnapshotSourceDevice, gbmodels.SnapshotSourceZLM, gbmodels.SnapshotSourceBrowser:
		return true
	default:
		return false
	}
}

// parseLibraryTime 只接受带时区的 RFC3339。⛔ 不接受裸 "2006-01-02 15:04:05"：
// 那种写法没有时区，平台与调用方的本地时区不同就会静默错开几小时 ——
// 而"少查了几小时的图"看起来永远像"这段时间没有抓拍"。
func parseLibraryTime(raw string) (*time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return nil, err
	}
	utc := parsed.UTC()
	return &utc, nil
}
