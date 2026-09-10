package workrecording

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
)

func formDB(t *testing.T, path string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.AutoMigrate(&models.GbWorkRecording{}))
	return db
}

func formJob(id string, channelID, actor uint, started, stopped time.Time) models.GbWorkRecording {
	return models.GbWorkRecording{
		ID: id, ChannelID: channelID, CreatedBy: actor, RequestID: id,
		State: StateRecording, DesiredAction: DesiredActionStart, Version: 7,
		FormState: FormDraft, FormVersion: 0, SchemaVersion: 1,
		DeviceID: "34020000002000000001", FormJSON: "{}",
		StartedAt: &started, StoppedAt: &stopped,
	}
}

func TestFormValidateAcceptsChineseAndEnforcesRuneLimits(t *testing.T) {
	valid := Form{
		ProjectName:       strings.Repeat("界", 256),
		WireLayingProcess: strings.Repeat("过程", 2000),
		Remark:            strings.Repeat("备注", 2000),
		WorkPersonnel:     make([]string, 50),
	}
	for i := range valid.WorkPersonnel {
		valid.WorkPersonnel[i] = "张三"
	}
	require.NoError(t, valid.Validate())

	tooLong := valid
	tooLong.ProjectName = strings.Repeat("界", 257)
	require.ErrorIs(t, tooLong.Validate(), ErrFormInvalid)
	tooLong = valid
	tooLong.WireLayingProcess = strings.Repeat("过", 4001)
	require.ErrorIs(t, tooLong.Validate(), ErrFormInvalid)
	tooLong = valid
	tooLong.Remark = strings.Repeat("注", 4001)
	require.ErrorIs(t, tooLong.Validate(), ErrFormInvalid)
	tooLong = valid
	tooLong.WorkPersonnel = append(tooLong.WorkPersonnel, "李四")
	require.ErrorIs(t, tooLong.Validate(), ErrFormInvalid)
	tooLong = valid
	tooLong.WorkPersonnel[0] = strings.Repeat("人", 257)
	require.ErrorIs(t, tooLong.Validate(), ErrFormInvalid)
}

func TestFormSaveDraftSurvivesReopenAndPreservesRecordingFacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "work-form.db")
	started := time.Date(2026, 9, 9, 10, 11, 12, 0, time.UTC)
	stopped := started.Add(time.Hour)
	job := formJob(uuid.NewString(), 8, 100, started, stopped)
	db := formDB(t, path)
	require.NoError(t, db.Create(&job).Error)
	input := Form{ProjectName: "接触网中文项目", Major: "接触网", WorkPersonnel: []string{"张三", "李四"}, Remark: "草稿"}
	saved, err := SaveDraft(context.Background(), db, job.ID, job.CreatedBy, 0, input)
	require.NoError(t, err)
	require.EqualValues(t, 1, saved.FormVersion)
	require.Equal(t, FormDraft, saved.FormState)
	require.Equal(t, input.ProjectName, saved.Form.ProjectName)
	require.Equal(t, input.WorkPersonnel, saved.Form.WorkPersonnel)

	var persisted models.GbWorkRecording
	require.NoError(t, db.First(&persisted, "id = ?", job.ID).Error)
	require.Equal(t, job.State, persisted.State)
	require.Equal(t, job.Version, persisted.Version)
	require.Equal(t, job.DeviceID, persisted.DeviceID)
	require.Equal(t, job.StartedAt, persisted.StartedAt)
	require.Equal(t, job.StoppedAt, persisted.StoppedAt)

	raw, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	reopened := formDB(t, path)
	restored, err := GetForm(context.Background(), reopened, job.ID)
	require.NoError(t, err)
	require.Equal(t, input.ProjectName, restored.Form.ProjectName)
	require.Equal(t, input.WorkPersonnel, restored.Form.WorkPersonnel)
	require.EqualValues(t, 1, restored.FormVersion)
}

func TestFormSaveDraftUsesCASAndRejectsSubmittedRows(t *testing.T) {
	db := formDB(t, filepath.Join(t.TempDir(), "cas.db"))
	job := formJob(uuid.NewString(), 8, 100, time.Now().UTC(), time.Now().UTC())
	require.NoError(t, db.Create(&job).Error)
	_, err := SaveDraft(context.Background(), db, job.ID, job.CreatedBy, 0, Form{ProjectName: "第一次"})
	require.NoError(t, err)
	_, err = SaveDraft(context.Background(), db, job.ID, job.CreatedBy, 0, Form{ProjectName: "覆盖"})
	require.ErrorIs(t, err, ErrVersionConflict)
	require.NoError(t, db.Model(&models.GbWorkRecording{}).Where("id = ?", job.ID).Updates(map[string]any{"form_state": FormSubmitted}).Error)
	_, err = SaveDraft(context.Background(), db, job.ID, job.CreatedBy, 1, Form{ProjectName: "提交后"})
	require.ErrorIs(t, err, ErrFormSubmitted)
	var persisted models.GbWorkRecording
	require.NoError(t, db.First(&persisted, "id = ?", job.ID).Error)
	require.Equal(t, `{"projectName":"第一次","major":"","stationArea":"","mileage":"","anchorSectionNo":"","startAnchorPillarNo":"","endAnchorPillarNo":"","workLeader":"","workPersonnel":[],"tensionWireCarModel":"","tensionWireCarNo":"","setTension":"","straightenerStatus":"","straightenerInspector":"","wireLayingProcess":"","remark":""}`, persisted.FormJSON)
}

func TestFormQueriesTreatRowsAffectedZeroAsNotFound(t *testing.T) {
	db := formDB(t, filepath.Join(t.TempDir(), "not-found.db"))
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("workrecording:form_not_found", gormhelper.MaskNotDataError))
	missing := uuid.NewString()
	_, err := GetForm(context.Background(), db, missing)
	require.ErrorIs(t, err, ErrFormNotFound)
	_, err = SaveDraft(context.Background(), db, missing, 100, 0, Form{})
	require.ErrorIs(t, err, ErrFormNotFound)
}

func TestFormSaveDraftRejectsCorruptStoredJSON(t *testing.T) {
	db := formDB(t, filepath.Join(t.TempDir(), "corrupt.db"))
	job := formJob(uuid.NewString(), 8, 100, time.Now().UTC(), time.Now().UTC())
	job.FormJSON = "not-json"
	require.NoError(t, db.Create(&job).Error)
	_, err := GetForm(context.Background(), db, job.ID)
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrFormNotFound))
}
