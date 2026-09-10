package workrecording

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

var (
	ErrFormInvalid     = errors.New("作业表单参数不合法")
	ErrFormNotFound    = errors.New("作业不存在")
	ErrFormForbidden   = errors.New("无权编辑作业表单")
	ErrFormSubmitted   = errors.New("作业表单已提交，不能修改")
	ErrFormNotEditable = errors.New("作业表单当前不可编辑")
)

// Form is the durable, optional work-recording form. Values remain strings
// because the business has not yet fixed units or validation rules for them.
type Form struct {
	ProjectName           string   `json:"projectName"`
	Major                 string   `json:"major"`
	StationArea           string   `json:"stationArea"`
	Mileage               string   `json:"mileage"`
	AnchorSectionNo       string   `json:"anchorSectionNo"`
	StartAnchorPillarNo   string   `json:"startAnchorPillarNo"`
	EndAnchorPillarNo     string   `json:"endAnchorPillarNo"`
	WorkLeader            string   `json:"workLeader"`
	WorkPersonnel         []string `json:"workPersonnel"`
	TensionWireCarModel   string   `json:"tensionWireCarModel"`
	TensionWireCarNo      string   `json:"tensionWireCarNo"`
	SetTension            string   `json:"setTension"`
	StraightenerStatus    string   `json:"straightenerStatus"`
	StraightenerInspector string   `json:"straightenerInspector"`
	WireLayingProcess     string   `json:"wireLayingProcess"`
	Remark                string   `json:"remark"`
}

const (
	formShortFieldMaxRunes = 256
	formLongFieldMaxRunes  = 4000
	formPersonnelMax       = 50
)

func (f Form) normalized() Form {
	if f.WorkPersonnel == nil {
		f.WorkPersonnel = []string{}
	}
	return f
}

func (f Form) Validate() error {
	short := []string{
		f.ProjectName, f.Major, f.StationArea, f.Mileage, f.AnchorSectionNo,
		f.StartAnchorPillarNo, f.EndAnchorPillarNo, f.WorkLeader,
		f.TensionWireCarModel, f.TensionWireCarNo, f.SetTension,
		f.StraightenerStatus, f.StraightenerInspector,
	}
	for _, value := range short {
		if utf8.RuneCountInString(value) > formShortFieldMaxRunes {
			return ErrFormInvalid
		}
	}
	if utf8.RuneCountInString(f.WireLayingProcess) > formLongFieldMaxRunes || utf8.RuneCountInString(f.Remark) > formLongFieldMaxRunes {
		return ErrFormInvalid
	}
	if len(f.WorkPersonnel) > formPersonnelMax {
		return ErrFormInvalid
	}
	for _, person := range f.WorkPersonnel {
		if utf8.RuneCountInString(person) > formShortFieldMaxRunes {
			return ErrFormInvalid
		}
	}
	return nil
}

// FormRecord contains the form envelope returned by both GET and PUT.
type FormRecord struct {
	JobID         string `json:"jobId"`
	ChannelID     uint   `json:"channelId"`
	FormVersion   uint64 `json:"formVersion"`
	FormState     string `json:"formState"`
	SchemaVersion uint   `json:"schemaVersion"`
	DeviceID      string `json:"deviceId"`
	Editable      bool   `json:"editable"`
	Form          Form   `json:"form"`
	CreatedBy     uint   `json:"-"`
}

func GetForm(ctx context.Context, db *gorm.DB, jobID string) (FormRecord, error) {
	job, err := findFormJob(ctx, db, jobID)
	if err != nil {
		return FormRecord{}, err
	}
	return formRecord(*job)
}

// SaveDraft stores one draft using formVersion as the compare-and-swap value.
// Only form columns are updated; recording state, version, timestamps and
// device identity are deliberately left untouched.
func SaveDraft(ctx context.Context, db *gorm.DB, jobID string, actor uint, expectedVersion uint64, form Form) (FormRecord, error) {
	if db == nil || actor == 0 || strings.TrimSpace(jobID) == "" {
		return FormRecord{}, ErrFormInvalid
	}
	form = form.normalized()
	if err := form.Validate(); err != nil {
		return FormRecord{}, err
	}
	encoded, err := json.Marshal(form)
	if err != nil {
		return FormRecord{}, fmt.Errorf("编码作业表单: %w", err)
	}

	job, err := findFormJob(ctx, db, jobID)
	if err != nil {
		return FormRecord{}, err
	}
	if job.CreatedBy != actor {
		return FormRecord{}, ErrFormForbidden
	}
	if job.FormState == FormSubmitted {
		return FormRecord{}, ErrFormSubmitted
	}
	if job.FormState != FormDraft {
		return FormRecord{}, ErrFormNotEditable
	}
	if job.FormVersion != expectedVersion {
		return FormRecord{}, ErrVersionConflict
	}

	result := db.WithContext(ctx).
		Model(&models.GbWorkRecording{}).
		Where("id = ? AND created_by = ? AND form_version = ? AND form_state = ?", jobID, actor, expectedVersion, FormDraft).
		UpdateColumns(map[string]any{
			"form_json":    string(encoded),
			"form_state":   FormDraft,
			"form_version": expectedVersion + 1,
		})
	if result.Error != nil {
		return FormRecord{}, result.Error
	}
	if result.RowsAffected != 1 {
		return classifySaveConflict(ctx, db, jobID, actor, expectedVersion)
	}
	job.FormJSON = string(encoded)
	job.FormState = FormDraft
	job.FormVersion = expectedVersion + 1
	return formRecord(*job)
}

func findFormJob(ctx context.Context, db *gorm.DB, jobID string) (*models.GbWorkRecording, error) {
	if db == nil || strings.TrimSpace(jobID) == "" {
		return nil, ErrFormNotFound
	}
	var job models.GbWorkRecording
	result := db.WithContext(ctx).Where("id = ?", jobID).First(&job)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrFormNotFound
		}
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrFormNotFound
	}
	return &job, nil
}

func classifySaveConflict(ctx context.Context, db *gorm.DB, jobID string, actor uint, expectedVersion uint64) (FormRecord, error) {
	job, err := findFormJob(ctx, db, jobID)
	if err != nil {
		return FormRecord{}, err
	}
	if job.CreatedBy != actor {
		return FormRecord{}, ErrFormForbidden
	}
	if job.FormState == FormSubmitted {
		return FormRecord{}, ErrFormSubmitted
	}
	if job.FormState != FormDraft {
		return FormRecord{}, ErrFormNotEditable
	}
	if job.FormVersion != expectedVersion {
		return FormRecord{}, ErrVersionConflict
	}
	return FormRecord{}, ErrVersionConflict
}

func formRecord(job models.GbWorkRecording) (FormRecord, error) {
	form, err := decodeStoredForm(job.FormJSON)
	if err != nil {
		return FormRecord{}, err
	}
	return FormRecord{
		JobID:         job.ID,
		ChannelID:     job.ChannelID,
		FormVersion:   job.FormVersion,
		FormState:     job.FormState,
		SchemaVersion: job.SchemaVersion,
		DeviceID:      job.DeviceID,
		Form:          form,
		CreatedBy:     job.CreatedBy,
	}, nil
}

func decodeStoredForm(raw string) (Form, error) {
	if strings.TrimSpace(raw) == "" {
		return Form{}.normalized(), nil
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	var form Form
	if err := decoder.Decode(&form); err != nil {
		return Form{}, fmt.Errorf("解析作业表单: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err == nil {
			return Form{}, errors.New("解析作业表单: 存在多余内容")
		}
		return Form{}, fmt.Errorf("解析作业表单: %w", err)
	}
	return form.normalized(), nil
}
