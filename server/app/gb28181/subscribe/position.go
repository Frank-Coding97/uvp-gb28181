package subscribe

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

type PositionProcessor struct {
	db             *gorm.DB
	now            func() time.Time
	historyEnabled func() bool
}

func NewPositionProcessor(db *gorm.DB, now func() time.Time, historyEnabled ...func() bool) *PositionProcessor {
	if now == nil {
		now = time.Now
	}
	enabled := PositionHistoryEnabled
	if len(historyEnabled) > 0 && historyEnabled[0] != nil {
		enabled = historyEnabled[0]
	}
	return &PositionProcessor{db: db, now: now, historyEnabled: enabled}
}

// Process 落地一包 MobilePosition NOTIFY —— 2016 扁平形态与 2022 列表形态都走这里。
//
// 两形态的差异在解析层就被 manscdp 归一化掉了（见 MobilePositionNotify.Positions），
// 本函数只面对「一组位置」：扁平形态恒为 1 条，2022 列表形态可能是 N 条（一包多台设备）。
func (p *PositionProcessor) Process(ctx context.Context, device *gbmodels.GbDevice, notification Notification) error {
	if p == nil || p.db == nil || device == nil {
		return fmt.Errorf("位置处理器未就绪")
	}
	notify, err := manscdp.ParseMobilePositionNotify(notification.Body)
	if err != nil {
		return err
	}
	positions := notify.Positions()
	if len(positions) == 0 {
		// 2022 A.2.5.6 允许 SumNum=0 的空列表（本次没有位置上报）。这是合法 no-op，不是错误 ——
		// 返回错误会让 notify.go 连 last_notify_at 都不更新，把一次「无位置」记成订阅异常。
		return nil
	}
	// 逐条独立落库，不整包成功/失败：
	//   · 单条脏数据（坐标为 0 / 采集时间非法）只跳过该条；
	//   · 整包全废时才返回首个错误 —— 保持改动前「零坐标必须报错」的契约
	//     （单设备场景恰好只有 1 条，行为与改动前逐字节一致）。
	// 不因一条失败就整包回滚是刻意的：一台上报异常不该连坐掉同包里其它设备的合法位置。
	var (
		saved    int
		skipped  int
		firstErr error
	)
	for _, position := range positions {
		if err := p.saveOne(ctx, device, position); err != nil {
			skipped++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		saved++
	}
	switch {
	case saved == 0:
		return firstErr
	case firstErr != nil:
		// 部分成功必须留痕：静默丢弃正是本次要修的毛病。
		logging.FromContext(ctx, nil).Named("subscribe").Warn(
			"位置通知部分条目未落地",
			zap.String("event", "subscribe.position.partial_persist"),
			zap.String("device_id", device.DeviceID),
			zap.Int("total", len(positions)),
			zap.Int("saved", saved),
			zap.Int("skipped", skipped),
			zap.Error(firstErr),
		)
	}
	return nil
}

// saveOne 落地单条位置：坐标守卫 → 采集时间 → 通道归属 → latest（+ history）写入。
func (p *PositionProcessor) saveOne(ctx context.Context, device *gbmodels.GbDevice, position manscdp.MobilePositionItem) error {
	if position.Longitude == 0 || position.Latitude == 0 {
		return fmt.Errorf("位置坐标不能为 0")
	}
	// CaptureTime 由解析层归一化保证非空：2022 取 Item/CaptureTime，2016 取根 Time。
	eventTime, err := parsePositionTime(position.CaptureTime)
	if err != nil {
		return err
	}
	var channel gbmodels.GbChannel
	channelResult := p.db.WithContext(ctx).Where("device_id = ? AND channel_id = ?", device.DeviceID, position.DeviceID).Limit(1).Find(&channel)
	var channelID *uint
	if channelResult.Error == nil && channelResult.RowsAffected > 0 {
		channelID = &channel.ID
	}
	receivedAt := p.now()
	latest := gbmodels.GbMobilePositionLatest{
		DeviceID: device.ID, SourceCode: position.DeviceID, ChannelID: channelID,
		EventTime: eventTime, ReceivedAt: receivedAt, Longitude: position.Longitude, Latitude: position.Latitude,
		Speed: floatPtr(position.Speed), Direction: floatPtr(position.Direction), Altitude: floatPtr(position.Altitude),
	}
	upsertLatest := func(tx *gorm.DB) error {
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "device_id"}, {Name: "source_code"}},
			DoUpdates: clause.AssignmentColumns([]string{"channel_id", "event_time", "received_at", "longitude", "latitude", "speed", "direction", "altitude", "updated_at"}),
		}).Create(&latest).Error
	}
	if p.historyEnabled == nil || !p.historyEnabled() {
		return upsertLatest(p.db.WithContext(ctx))
	}
	history := gbmodels.GbMobilePositionHistory{
		DeviceID: device.ID, SourceCode: position.DeviceID, ChannelID: channelID,
		EventTime: eventTime, ReceivedAt: receivedAt, Longitude: position.Longitude, Latitude: position.Latitude,
		Speed: floatPtr(position.Speed), Direction: floatPtr(position.Direction), Altitude: floatPtr(position.Altitude),
	}
	return p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := upsertLatest(tx); err != nil {
			return err
		}
		return tx.Create(&history).Error
	})
}

func parsePositionTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("位置时间非法: %q", value)
}

func floatPtr(value float64) *float64 { return &value }
