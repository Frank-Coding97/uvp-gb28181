package ptz

import (
	"crypto/sha256"
	"fmt"

	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
)

func accumulateCruiseDetailStage(stage queryResponseStage, body []byte) (*queryResponseStage, bool, error) {
	response, err := manscdp.ParseCruiseTrackResponse(body)
	if err != nil {
		return nil, false, err
	}
	if response.SumNum > maxQueryStagePresets {
		return nil, false, fmt.Errorf("cruise point limit exceeded")
	}
	if stage.detail == nil {
		detail := response.CruiseTrack
		detail.PointList.Points = nil
		stage.detail = &detail
		stage.expected = response.SumNum
		stage.detailPages = make(map[[32]byte]struct{})
		stage.detailSummary = []byte(summarizePTZBody(body))
	} else if stage.detail.ID != response.ID || stage.detail.Name != response.Name || stage.expected != response.SumNum {
		return nil, false, fmt.Errorf("cruise detail identity or total changed")
	}
	digest := sha256.Sum256(body)
	if _, duplicate := stage.detailPages[digest]; !duplicate {
		if len(stage.detailPages) >= 10000 {
			return nil, false, fmt.Errorf("cruise detail page limit exceeded")
		}
		stage.detailPages[digest] = struct{}{}
		stage.detail.PointList.Points = append(stage.detail.PointList.Points, response.PointList.Points...)
	}
	count := len(stage.detail.PointList.Points)
	if count > stage.expected {
		return nil, false, fmt.Errorf("cruise points exceed reported total")
	}
	stage.detail.PointList.Num = count
	stage.detail.PointList.NumUpper = count
	stage.detail.PointList.NumLower = 0
	return &stage, count == stage.expected, nil
}
