package models

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"

	"go.uber.org/zap"
)

const AREAPATH = "./resource/public/area/area.json"

var (
	instance AreaModelList
	once     sync.Once
)

func modelLogContext(contexts ...context.Context) context.Context {
	if len(contexts) > 0 && contexts[0] != nil {
		return contexts[0]
	}
	return context.Background()
}

func NewAreaModel() *AreaModel {
	return &AreaModel{}
}

func GetAreaListInstance(contexts ...context.Context) AreaModelList {
	ctx := modelLogContext(contexts...)

	once.Do(func() {
		file, err := os.Open(AREAPATH)
		if err != nil {
			app.Log(ctx).Error("area data load failed",
				zap.String("event", "models.area.load_failed"),
				zap.String("phase", "open"),
				zap.String("path", AREAPATH),
				logging.Error(err))
			return
		}
		defer file.Close()

		if err := json.NewDecoder(file).Decode(&instance); err != nil {
			app.Log(ctx).Error("area data load failed",
				zap.String("event", "models.area.load_failed"),
				zap.String("phase", "decode"),
				zap.String("path", AREAPATH),
				logging.Error(err))
		}
	})
	return instance
}

type AreaModel struct {
	Value    string        `json:"value"`
	Label    string        `json:"label"`
	Level    string        `json:"level"`
	Parent   string        `json:"parent"`
	Children AreaModelList `json:"children"`
}

type AreaModelList []AreaModel

func (list AreaModelList) IsEmpty() bool {
	return len(list) == 0
}

// AreaText 地区编码转换为地区文本
func (list AreaModelList) AreaText(area string, split string) string {
	if area == "" || list.IsEmpty() {
		return ""
	}
	areaList := strings.Split(area, ",")
	areaText := ""
	current := list
	for _, v := range areaList {
		for _, item := range current {
			if item.Value == v {
				areaText += item.Label + split
				current = item.Children
				break
			}
		}
	}
	return strings.TrimRight(areaText, split)
}
