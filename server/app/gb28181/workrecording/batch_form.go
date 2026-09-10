package workrecording

import (
	"context"
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
