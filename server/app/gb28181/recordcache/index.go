package recordcache

import (
	"context"
	"strings"

	"gorm.io/gorm"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// GormSegmentFileIndex 读取平台**已入库**的录制文件（gb_recording_file）。
//
// ⛔ 为什么必须绕到这张表：ZLM 的 getMp4RecordFile 只解析 rootPath + paths，
// 不回时长与大小（既有测试把 DurationMS / FileSize 恒为 nil 钉成了契约）。
// 拿它算进度会让「已缓存时长」永远停在 0，游标随之失去唯一可靠依据，
// 于是退化成「按请求区间推进」—— 把「实际只录到 84 秒」当成「录满 20 分钟」
// 宣布成功，用户一个文件都拿不到。
//
// on_record_mp4 hook（ZLM 自己发的，载荷里带 time_len / file_size）已经把
// 真实数据写进这张表了，直接读即可 —— 不必再问设备，也不必问 ZLM。
//
// 关于归属：缓存流的记录在这张表里 channel_id=0 / device_id=”，
// 因为 pb- 流不属于任何通道。它们因此**不会**出现在按通道过滤的云端录像列表里，
// 不会污染用户的云端录像。
type GormSegmentFileIndex struct {
	db *gorm.DB
}

func NewGormSegmentFileIndex(db *gorm.DB) *GormSegmentFileIndex {
	return &GormSegmentFileIndex{db: db}
}

func (i *GormSegmentFileIndex) IndexedSegments(ctx context.Context, stream string) ([]IndexedSegment, error) {
	stream = strings.TrimSpace(stream)
	if i == nil || i.db == nil || stream == "" {
		return nil, nil
	}
	var rows []gbmodels.GbRecordingFile
	if err := i.db.WithContext(ctx).
		Where("stream = ? AND app = ?", stream, mediaApp).
		Order("id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]IndexedSegment, 0, len(rows))
	for _, row := range rows {
		segment := IndexedSegment{Name: row.FileName, Path: row.FilePath}
		if row.FileSize != nil {
			segment.Size = int64(*row.FileSize)
		}
		// time_len 是 decimal 秒，转毫秒时按四舍五入前先乘 1000 取整：
		// 83.841 秒 → 83841ms。截断成 83 秒会让长录像的游标越走越偏。
		if row.TimeLen != nil {
			segment.DurationMS = int64(*row.TimeLen*1000 + 0.5)
		}
		if row.StartTime != nil {
			segment.StartedAt = *row.StartTime
		}
		if row.RecordDate != nil {
			segment.Period = row.RecordDate.Format("2006-01-02")
		} else if row.StartTime != nil {
			segment.Period = row.StartTime.Format("2006-01-02")
		}
		out = append(out, segment)
	}
	return out, nil
}
