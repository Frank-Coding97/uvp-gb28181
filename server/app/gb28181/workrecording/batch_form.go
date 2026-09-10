package workrecording

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

type BatchFormRecord struct {
	BatchID       string `json:"batchId"`
	FormVersion   uint64 `json:"formVersion"`
	FormState     string `json:"formState"`
	SchemaVersion uint   `json:"schemaVersion"`
	DeviceID      string `json:"deviceId"`
	Editable      bool   `json:"editable"`
	Form          Form   `json:"form"`
}

func GetBatchForm(ctx context.Context, db *gorm.DB, batchID string, actor uint) (BatchFormRecord, error) {
	batch, err := findBatchForm(ctx, db, batchID)
	if err != nil {
		return BatchFormRecord{}, err
	}
	if actor == 0 || batch.CreatedBy != actor {
		return BatchFormRecord{}, ErrFormForbidden
	}
	return batchFormRecord(*batch)
}

func SaveBatchDraft(ctx context.Context, db *gorm.DB, batchID string, actor uint, expectedVersion uint64, form Form) (BatchFormRecord, error) {
	if db == nil || actor == 0 || strings.TrimSpace(batchID) == "" {
		return BatchFormRecord{}, ErrFormInvalid
	}
	form = form.normalized()
	if err := form.Validate(); err != nil {
		return BatchFormRecord{}, err
	}
	encoded, err := json.Marshal(form)
	if err != nil {
		return BatchFormRecord{}, err
	}
	batch, err := findBatchForm(ctx, db, batchID)
	if err != nil {
		return BatchFormRecord{}, err
	}
	if batch.CreatedBy != actor {
		return BatchFormRecord{}, ErrFormForbidden
	}
	if batch.FormState == FormSubmitted {
		return BatchFormRecord{}, ErrFormSubmitted
	}
	if batch.FormVersion != expectedVersion {
		return BatchFormRecord{}, ErrVersionConflict
	}
	result := db.WithContext(ctx).Model(&models.GbWorkRecordingBatch{}).Where("id = ? AND created_by = ? AND form_version = ? AND form_state = ?", batchID, actor, expectedVersion, FormDraft).UpdateColumns(map[string]any{"form_json": string(encoded), "form_version": expectedVersion + 1})
	if result.Error != nil {
		return BatchFormRecord{}, result.Error
	}
	if result.RowsAffected != 1 {
		return BatchFormRecord{}, ErrVersionConflict
	}
	batch.FormJSON, batch.FormVersion = string(encoded), expectedVersion+1
	return batchFormRecord(*batch)
}

func findBatchForm(ctx context.Context, db *gorm.DB, batchID string) (*models.GbWorkRecordingBatch, error) {
	if db == nil || strings.TrimSpace(batchID) == "" {
		return nil, ErrFormNotFound
	}
	var batch models.GbWorkRecordingBatch
	result := db.WithContext(ctx).Where("id = ?", batchID).First(&batch)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrFormNotFound
		}
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrFormNotFound
	}
	return &batch, nil
}

func batchFormRecord(batch models.GbWorkRecordingBatch) (BatchFormRecord, error) {
	form, err := decodeStoredForm(batch.FormJSON)
	if err != nil {
		return BatchFormRecord{}, err
	}
	return BatchFormRecord{BatchID: batch.ID, FormVersion: batch.FormVersion, FormState: batch.FormState, SchemaVersion: batch.SchemaVersion, DeviceID: batch.DeviceID, Editable: batch.FormState == FormDraft, Form: form}, nil
}
