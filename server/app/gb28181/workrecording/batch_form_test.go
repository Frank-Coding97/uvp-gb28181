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

func TestBatchFormPersistsOnLedgerWithCAS(t *testing.T) {
	db := batchFormDB(t)
	batch := models.GbWorkRecordingBatch{
		ID: uuid.NewString(), CreatedBy: 100, RequestID: "request-1", State: StateRecording,
		Version: 7, FormState: FormDraft, SchemaVersion: 1, FormJSON: "{}",
	}
	require.NoError(t, db.Create(&batch).Error)

	saved, err := SaveBatchDraft(context.Background(), db, batch.ID, 100, 0, Form{
		ProjectName: "四路放线作业", WorkPersonnel: []string{"张三", "李四"},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, saved.FormVersion)
	require.Equal(t, batch.ID, saved.BatchID)
	require.Equal(t, "四路放线作业", saved.Form.ProjectName)
	require.Equal(t, []string{"张三", "李四"}, saved.Form.WorkPersonnel)

	var persisted models.GbWorkRecordingBatch
	require.NoError(t, db.First(&persisted, "id = ?", batch.ID).Error)
	require.EqualValues(t, 7, persisted.Version, "saving the ledger form must not mutate recording state version")
	require.Equal(t, StateRecording, persisted.State)

	_, err = SaveBatchDraft(context.Background(), db, batch.ID, 100, 0, Form{})
	require.ErrorIs(t, err, ErrVersionConflict)
	_, err = SaveBatchDraft(context.Background(), db, batch.ID, 200, 1, Form{})
	require.ErrorIs(t, err, ErrFormForbidden)
}

func TestBatchFormRejectsSubmittedAndCorruptLedgers(t *testing.T) {
	db := batchFormDB(t)
	batch := models.GbWorkRecordingBatch{
		ID: uuid.NewString(), CreatedBy: 100, RequestID: "request-2", State: StateStopped,
		Version: 3, FormState: FormSubmitted, FormVersion: 2, SchemaVersion: 1, FormJSON: "{}",
	}
	require.NoError(t, db.Create(&batch).Error)
	_, err := SaveBatchDraft(context.Background(), db, batch.ID, 100, 2, Form{})
	require.ErrorIs(t, err, ErrFormSubmitted)

	require.NoError(t, db.Model(&batch).Updates(map[string]any{"form_state": FormDraft, "form_json": "{"}).Error)
	_, err = GetBatchForm(context.Background(), db, batch.ID, 100)
	require.Error(t, err)
}
