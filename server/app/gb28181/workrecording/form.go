package workrecording

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

var (
	ErrFormInvalid     = errors.New("作业表单参数不合法")
	ErrFormRequired    = errors.New("作业表单缺少必填项")
	ErrFormNotFound    = errors.New("作业不存在")
	ErrFormForbidden   = errors.New("无权编辑作业表单")
	ErrFormSubmitted   = errors.New("作业表单已提交，不能修改")
	ErrFormNotEditable = errors.New("作业表单当前不可编辑")
)

// Form 是作业单表单。字段保持字符串，因为业务尚未固定单位与取值规则。
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

// Validate 同时校验业务必填项与各字段的长度上限。
// 「先填作业单再录制」是唯一创建路径，因此必填校验就在这里，不再区分草稿态。
func (f Form) Validate() error {
	if missing := f.missingRequiredFields(); len(missing) > 0 {
		return fmt.Errorf("%w: %s", ErrFormRequired, strings.Join(missing, "、"))
	}
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

// missingRequiredFields 是开始录制所需的业务最小集合。其余字段选填，现场按进度补。
// 必须与前端 `web/src/views/gb28181/work-orders/orderState.ts` 的 workOrderRequiredFields 保持同步。
func (f Form) missingRequiredFields() []string {
	missing := make([]string, 0, 5)
	if strings.TrimSpace(f.ProjectName) == "" {
		missing = append(missing, "项目名称")
	}
	if strings.TrimSpace(f.StationArea) == "" {
		missing = append(missing, "站区")
	}
	if strings.TrimSpace(f.AnchorSectionNo) == "" {
		missing = append(missing, "锚段号")
	}
	if strings.TrimSpace(f.WorkLeader) == "" {
		missing = append(missing, "作业负责人")
	}
	named := false
	for _, person := range f.WorkPersonnel {
		if strings.TrimSpace(person) != "" {
			named = true
			break
		}
	}
	if !named {
		missing = append(missing, "作业人员")
	}
	return missing
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
