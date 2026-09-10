package workrecording

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func batchFormDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "batch-form.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.GbWorkRecordingBatch{}))
	return db
}

// 作业单在创建时就已提交表单，因此详情读取到的表单是只读的。
func TestBatchFormReadsTheSubmittedOrderForm(t *testing.T) {
	db := batchFormDB(t)
	batch := models.GbWorkRecordingBatch{
		ID: uuid.NewString(), CreatedBy: 100, RequestID: "request-1", State: StateRecording,
		Version: 1, FormState: FormSubmitted, FormVersion: 1, SchemaVersion: 1,
		FormJSON: `{"projectName":"沪宁线放线作业","workPersonnel":["张伟","李强"]}`,
	}
	require.NoError(t, db.Create(&batch).Error)

	record, err := GetBatchForm(context.Background(), db, batch.ID, 100)
	require.NoError(t, err)
	require.Equal(t, batch.ID, record.BatchID)
	require.Equal(t, "沪宁线放线作业", record.Form.ProjectName)
	require.Equal(t, []string{"张伟", "李强"}, record.Form.WorkPersonnel)
	require.False(t, record.Editable)
}

func TestBatchFormRejectsForeignActorsMissingAndCorruptLedgers(t *testing.T) {
	db := batchFormDB(t)
	batch := models.GbWorkRecordingBatch{
		ID: uuid.NewString(), CreatedBy: 100, RequestID: "request-2", State: StateStopped,
		Version: 3, FormState: FormSubmitted, FormVersion: 2, SchemaVersion: 1, FormJSON: "{}",
	}
	require.NoError(t, db.Create(&batch).Error)

	_, err := GetBatchForm(context.Background(), db, batch.ID, 200)
	require.ErrorIs(t, err, ErrFormForbidden)

	_, err = GetBatchForm(context.Background(), db, uuid.NewString(), 100)
	require.ErrorIs(t, err, ErrFormNotFound)

	require.NoError(t, db.Model(&batch).Updates(map[string]any{"form_json": "{"}).Error)
	_, err = GetBatchForm(context.Background(), db, batch.ID, 100)
	require.Error(t, err)
}
