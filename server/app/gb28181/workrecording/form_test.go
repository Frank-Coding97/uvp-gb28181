package workrecording

import (
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
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

// 业务最小必填集合，其余字段留空。
func requiredForm() Form {
	return Form{
		ProjectName:     "沪宁线放线作业",
		StationArea:     "南京南—江宁",
		AnchorSectionNo: "A-12",
		WorkLeader:      "张伟",
		WorkPersonnel:   []string{"张伟"},
	}
}

func TestFormValidateAcceptsChineseAndEnforcesRuneLimits(t *testing.T) {
	valid := requiredForm()
	valid.ProjectName = strings.Repeat("界", 256)
	valid.WireLayingProcess = strings.Repeat("过程", 2000)
	valid.Remark = strings.Repeat("备注", 2000)
	valid.WorkPersonnel = make([]string, 50)
	for i := range valid.WorkPersonnel {
		valid.WorkPersonnel[i] = "张三"
	}
	require.NoError(t, valid.Validate())

	for _, mutate := range []func(*Form){
		func(f *Form) { f.ProjectName = strings.Repeat("界", 257) },
		func(f *Form) { f.WireLayingProcess = strings.Repeat("过", 4001) },
		func(f *Form) { f.Remark = strings.Repeat("注", 4001) },
		func(f *Form) { f.WorkPersonnel = append(f.WorkPersonnel, "李四") },
		func(f *Form) { f.WorkPersonnel = []string{strings.Repeat("人", 257)} },
	} {
		broken := valid
		broken.WorkPersonnel = append([]string{}, valid.WorkPersonnel...)
		mutate(&broken)
		require.ErrorIs(t, broken.Validate(), ErrFormInvalid)
	}
}

// 「先填作业单再录制」是唯一创建路径，所以必填校验与长度校验都在 Validate 上。
func TestFormValidateRequiresTheBusinessMinimum(t *testing.T) {
	require.NoError(t, requiredForm().Validate())

	// 空白不算填了
	blank := requiredForm()
	blank.ProjectName = "   "
	require.ErrorIs(t, blank.Validate(), ErrFormRequired)

	for _, drop := range []func(*Form){
		func(f *Form) { f.ProjectName = "" },
		func(f *Form) { f.StationArea = "" },
		func(f *Form) { f.AnchorSectionNo = "" },
		func(f *Form) { f.WorkLeader = "" },
		func(f *Form) { f.WorkPersonnel = nil },
		func(f *Form) { f.WorkPersonnel = []string{} },
		func(f *Form) { f.WorkPersonnel = []string{"  "} },
	} {
		broken := requiredForm()
		drop(&broken)
		require.ErrorIs(t, broken.Validate(), ErrFormRequired)
	}
}

func TestFormValidateNamesEveryMissingField(t *testing.T) {
	err := Form{}.Validate()
	require.ErrorIs(t, err, ErrFormRequired)
	for _, field := range []string{"项目名称", "站区", "锚段号", "作业负责人", "作业人员"} {
		require.Contains(t, err.Error(), field)
	}
}

func TestDecodeStoredFormAcceptsEmptyAndRejectsTrailingContent(t *testing.T) {
	empty, err := decodeStoredForm("")
	require.NoError(t, err)
	require.Equal(t, []string{}, empty.WorkPersonnel)

	decoded, err := decodeStoredForm(`{"projectName":"沪宁线放线作业","workPersonnel":["张伟","李强"]}`)
	require.NoError(t, err)
	require.Equal(t, "沪宁线放线作业", decoded.ProjectName)
	require.Equal(t, []string{"张伟", "李强"}, decoded.WorkPersonnel)

	for _, raw := range []string{"not-json", `{"projectName":"甲"}{"projectName":"乙"}`} {
		_, err := decodeStoredForm(raw)
		require.Error(t, err, raw)
	}
}
