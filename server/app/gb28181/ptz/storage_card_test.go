package ptz

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func newStorageCardTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbPTZOperation{}, &gbmodels.GbDeviceStorageCard{}))
	service, err := NewService(db, &fakeTrackedSender{}, func() time.Time {
		return time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	})
	require.NoError(t, err)
	return service, db
}

func createStorageCardOperation(t *testing.T, db *gorm.DB, operationID string, deviceID uint, targetCode string) gbmodels.GbPTZOperation {
	t.Helper()
	operation := gbmodels.GbPTZOperation{
		OperationID: operationID, IdempotencyKey: operationID,
		DeviceID: deviceID, DeviceCode: "D", ChannelID: 7, ChannelCode: targetCode,
		CmdType: manscdp.CmdSDCardStatus, Action: "refresh_storage_cards",
		SN: 5, Status: gbmodels.PTZOperationSent, ResponseRequired: true, CreatedAt: time.Now(),
	}
	require.NoError(t, db.Create(&operation).Error)
	return operation
}

func card(id int, name string, state manscdp.SDCardState, capacity, free int) manscdp.SDCardItem {
	return manscdp.SDCardItem{ID: id, HddName: name, Status: state, Capacity: capacity, FreeSpace: free}
}

func loadCards(t *testing.T, db *gorm.DB, deviceID uint, targetCode string) []gbmodels.GbDeviceStorageCard {
	t.Helper()
	var list []gbmodels.GbDeviceStorageCard
	require.NoError(t, db.Where("device_id = ? AND target_code = ?", deviceID, targetCode).Order("card_id").Find(&list).Error)
	return list
}

func TestPersistStorageCardsStoresOneRowPerCard(t *testing.T) {
	service, db := newStorageCardTestService(t)
	operation := createStorageCardOperation(t, db, "sc-1", 1, "C1")

	require.NoError(t, service.persistStorageCardsWithDB(context.Background(), db, operation, manscdp.SDCardStatus{
		SN: 5, DeviceID: "C1", SumNum: 2,
		Items: []manscdp.SDCardItem{
			card(1, "SD Card 1", manscdp.SDCardStateOK, 32768, 24576),
			card(2, "SD Card 2", manscdp.SDCardStateFormatting, 16384, 0),
		},
	}, []byte("body")))

	list := loadCards(t, db, 1, "C1")
	require.Len(t, list, 2)
	require.Equal(t, 1, list[0].CardID)
	require.Equal(t, "SD Card 1", list[0].HddName)
	require.Equal(t, gbmodels.StorageCardStateOK, list[0].Status)
	require.Equal(t, 32768, list[0].CapacityMB)
	require.Equal(t, 24576, list[0].FreeSpaceMB)
	require.Equal(t, 2, list[1].CardID)
	require.Equal(t, gbmodels.StorageCardStateFormatting, list[1].Status)
	require.Equal(t, operation.ID, list[1].SourceOperationSeq)
	require.Equal(t, 5, list[1].SourceSN)
}

func TestPersistStorageCardsKeepsNewerOperationWhenResponsesArriveOutOfOrder(t *testing.T) {
	service, db := newStorageCardTestService(t)
	older := createStorageCardOperation(t, db, "sc-older", 1, "C1")
	newer := createStorageCardOperation(t, db, "sc-newer", 1, "C1")
	require.Greater(t, newer.ID, older.ID)

	require.NoError(t, service.persistStorageCardsWithDB(context.Background(), db, newer, manscdp.SDCardStatus{
		SN: 5, DeviceID: "C1", SumNum: 1,
		Items: []manscdp.SDCardItem{card(1, "new", manscdp.SDCardStateOK, 100, 90)},
	}, []byte("newer")))
	// 迟到的旧应答：整批放弃，不能把 100/90 覆盖回去。
	require.NoError(t, service.persistStorageCardsWithDB(context.Background(), db, older, manscdp.SDCardStatus{
		SN: 5, DeviceID: "C1", SumNum: 1,
		Items: []manscdp.SDCardItem{card(1, "old", manscdp.SDCardStateError, 1, 0)},
	}, []byte("older")))

	list := loadCards(t, db, 1, "C1")
	require.Len(t, list, 1)
	require.Equal(t, "new", list[0].HddName)
	require.Equal(t, 100, list[0].CapacityMB)
	require.Equal(t, newer.ID, list[0].SourceOperationSeq)
}

func TestPersistStorageCardsRemovesCardsThatDisappeared(t *testing.T) {
	service, db := newStorageCardTestService(t)
	first := createStorageCardOperation(t, db, "sc-first", 1, "C1")
	require.NoError(t, service.persistStorageCardsWithDB(context.Background(), db, first, manscdp.SDCardStatus{
		SN: 5, DeviceID: "C1", SumNum: 2,
		Items: []manscdp.SDCardItem{
			card(1, "A", manscdp.SDCardStateOK, 10, 5),
			card(2, "B", manscdp.SDCardStateOK, 10, 5),
		},
	}, []byte("first")))

	second := createStorageCardOperation(t, db, "sc-second", 1, "C1")
	require.NoError(t, service.persistStorageCardsWithDB(context.Background(), db, second, manscdp.SDCardStatus{
		SN: 5, DeviceID: "C1", SumNum: 1,
		Items: []manscdp.SDCardItem{card(1, "A", manscdp.SDCardStateOK, 10, 4)},
	}, []byte("second")))

	list := loadCards(t, db, 1, "C1")
	require.Len(t, list, 1, "卡 2 被拔掉了，必须从缓存里消失，否则前端会一直显示一张不存在的卡")
	require.Equal(t, 1, list[0].CardID)
	require.Equal(t, 4, list[0].FreeSpaceMB)
}

func TestPersistStorageCardsEmptiesCacheWhenDeviceReportsNoCards(t *testing.T) {
	service, db := newStorageCardTestService(t)
	first := createStorageCardOperation(t, db, "sc-first", 1, "C1")
	require.NoError(t, service.persistStorageCardsWithDB(context.Background(), db, first, manscdp.SDCardStatus{
		SN: 5, DeviceID: "C1", SumNum: 1,
		Items: []manscdp.SDCardItem{card(1, "A", manscdp.SDCardStateOK, 10, 5)},
	}, []byte("first")))

	second := createStorageCardOperation(t, db, "sc-second", 1, "C1")
	require.NoError(t, service.persistStorageCardsWithDB(context.Background(), db, second,
		manscdp.SDCardStatus{SN: 5, DeviceID: "C1", SumNum: 0}, []byte("none")))

	require.Empty(t, loadCards(t, db, 1, "C1"), "SumNum=0 是合法结果：无卡")
}

func TestPersistStorageCardsDoesNotTouchOtherTargets(t *testing.T) {
	service, db := newStorageCardTestService(t)
	// 同一台设备的两个查询目标：按设备编码查、按通道编码查。
	deviceTarget := createStorageCardOperation(t, db, "sc-device", 1, "DEVICE")
	channelTarget := createStorageCardOperation(t, db, "sc-channel", 1, "CHANNEL")
	require.NoError(t, service.persistStorageCardsWithDB(context.Background(), db, deviceTarget, manscdp.SDCardStatus{
		SN: 5, DeviceID: "DEVICE", SumNum: 1,
		Items: []manscdp.SDCardItem{card(1, "DEV", manscdp.SDCardStateOK, 10, 5)},
	}, []byte("device")))
	require.NoError(t, service.persistStorageCardsWithDB(context.Background(), db, channelTarget, manscdp.SDCardStatus{
		SN: 5, DeviceID: "CHANNEL", SumNum: 2,
		Items: []manscdp.SDCardItem{
			card(1, "CH1", manscdp.SDCardStateOK, 20, 15),
			card(2, "CH2", manscdp.SDCardStateOK, 20, 15),
		},
	}, []byte("channel")))

	// ⛔ 这里就是"清理消失的卡"最容易写错的地方：如果按"不在本次列表里"删，
	// 上面 DEVICE 那一行会被这一轮 CHANNEL 的写入误删。
	require.Len(t, loadCards(t, db, 1, "DEVICE"), 1)
	require.Len(t, loadCards(t, db, 1, "CHANNEL"), 2)
}

func TestPersistStorageCardsIsolatesSameTargetCodeAcrossDevices(t *testing.T) {
	service, db := newStorageCardTestService(t)
	first := createStorageCardOperation(t, db, "sc-d1", 1, "C1")
	second := createStorageCardOperation(t, db, "sc-d2", 2, "C1")
	require.NoError(t, service.persistStorageCardsWithDB(context.Background(), db, first, manscdp.SDCardStatus{
		SN: 5, DeviceID: "C1", SumNum: 1,
		Items: []manscdp.SDCardItem{card(1, "A", manscdp.SDCardStateOK, 10, 5)},
	}, []byte("d1")))
	require.NoError(t, service.persistStorageCardsWithDB(context.Background(), db, second, manscdp.SDCardStatus{
		SN: 5, DeviceID: "C1", SumNum: 1,
		Items: []manscdp.SDCardItem{card(1, "B", manscdp.SDCardStateOK, 30, 20)},
	}, []byte("d2")))

	require.Equal(t, 10, loadCards(t, db, 1, "C1")[0].CapacityMB)
	require.Equal(t, 30, loadCards(t, db, 2, "C1")[0].CapacityMB)
}

func TestPersistStorageCardsKeepsOptionalFormatProgressAbsent(t *testing.T) {
	service, db := newStorageCardTestService(t)
	operation := createStorageCardOperation(t, db, "sc-1", 1, "C1")
	require.NoError(t, service.persistStorageCardsWithDB(context.Background(), db, operation, manscdp.SDCardStatus{
		SN: 5, DeviceID: "C1", SumNum: 1,
		Items: []manscdp.SDCardItem{card(1, "A", manscdp.SDCardStateUnformatted, 10, 0)},
	}, []byte("body")))
	require.Nil(t, loadCards(t, db, 1, "C1")[0].FormatProgress)

	progress := 42
	second := createStorageCardOperation(t, db, "sc-2", 1, "C1")
	require.NoError(t, service.persistStorageCardsWithDB(context.Background(), db, second, manscdp.SDCardStatus{
		SN: 5, DeviceID: "C1", SumNum: 1,
		Items: []manscdp.SDCardItem{{
			ID: 1, HddName: "A", Status: manscdp.SDCardStateFormatting,
			FormatProgress: &progress, Capacity: 10, FreeSpace: 0,
		}},
	}, []byte("body")))
	got := loadCards(t, db, 1, "C1")[0].FormatProgress
	require.NotNil(t, got)
	require.Equal(t, 42, *got)
}

func TestPersistStorageCardsMapsUnknownStatusWithoutFailing(t *testing.T) {
	service, db := newStorageCardTestService(t)
	operation := createStorageCardOperation(t, db, "sc-1", 1, "C1")
	require.NoError(t, service.persistStorageCardsWithDB(context.Background(), db, operation, manscdp.SDCardStatus{
		SN: 5, DeviceID: "C1", SumNum: 1,
		Items: []manscdp.SDCardItem{card(1, "A", manscdp.SDCardStateUnknown, 10, 5)},
	}, []byte("body")))
	require.Equal(t, gbmodels.StorageCardStateUnknown, loadCards(t, db, 1, "C1")[0].Status)
	require.True(t, gbmodels.StorageCardStateValid(loadCards(t, db, 1, "C1")[0].Status))
}

func TestPersistStorageCardsRejectsOperationWithoutIdentity(t *testing.T) {
	service, db := newStorageCardTestService(t)
	err := service.persistStorageCardsWithDB(context.Background(), db, gbmodels.GbPTZOperation{ID: 0, DeviceID: 1},
		manscdp.SDCardStatus{}, []byte("body"))
	require.Error(t, err, "没有可靠序列就不能写，否则 CAS 无从谈起")
}
