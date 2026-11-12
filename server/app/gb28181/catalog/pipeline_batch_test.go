package catalog_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/gb28181/catalog"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

func TestPipelineIngestSharesTransactionWithinShortBatch(t *testing.T) {
	db := newPipelineTestDB(t)
	var transactions []gorm.ConnPool
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:record_transaction", func(tx *gorm.DB) {
		if tx.Statement.Table == "gb_device" || tx.Statement.Table == "gb_channel" {
			transactions = append(transactions, tx.Statement.ConnPool)
		}
	}))

	source := "37011200001170000001"
	err := catalog.New(db).Ingest(context.Background(), catalog.Sender{OwnerDeptID: 10, SourceDeviceID: source}, []catalog.CatalogItem{
		{DeviceID: source, Name: "device", CivilCode: "370112"},
		{DeviceID: "37011200001320000001", Name: "channel", CivilCode: "370112", ParentID: source},
	})
	require.NoError(t, err)
	require.Len(t, transactions, 2)
	require.Same(t, transactions[0], transactions[1], "同一短批次应只提交一次事务")
}

func TestPipelineIngestRollsBackFailedItemAndContinuesBatch(t *testing.T) {
	db := newPipelineTestDB(t)
	source := "37011200001170000001"
	failed := "37011200001320000001"
	succeeded := "37011200001320000002"
	injected := errors.New("injected channel failure")
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:fail_channel", func(tx *gorm.DB) {
		if tx.Statement.Table != "gb_channel" {
			return
		}
		channel, ok := tx.Statement.Dest.(*gbmodels.GbChannel)
		if ok && channel.ChannelID == failed {
			tx.AddError(injected)
		}
	}))

	err := catalog.New(db).Ingest(context.Background(), catalog.Sender{OwnerDeptID: 10, SourceDeviceID: source}, []catalog.CatalogItem{
		{DeviceID: source, Name: "device", CivilCode: "370112"},
		{DeviceID: failed, Name: "failed", CivilCode: "370112", ParentID: source},
		{DeviceID: succeeded, Name: "succeeded", CivilCode: "370112", ParentID: source},
	})
	require.ErrorIs(t, err, injected)

	var deviceCount, failedCount, succeededCount, failedNodeCount int64
	require.NoError(t, db.Model(&gbmodels.GbDevice{}).Where("device_id = ?", source).Count(&deviceCount).Error)
	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Where("channel_id = ?", failed).Count(&failedCount).Error)
	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Where("channel_id = ?", succeeded).Count(&succeededCount).Error)
	require.NoError(t, db.Model(&gbmodels.GbCatalogNode{}).Where("code = ?", failed).Count(&failedNodeCount).Error)
	require.EqualValues(t, 1, deviceCount)
	require.Zero(t, failedCount)
	require.Zero(t, failedNodeCount)
	require.EqualValues(t, 1, succeededCount)
}
