package gb28181

import (
	"encoding/xml"
	"fmt"
	"sort"

	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

type cascadePresetListXML struct {
	Num   int              `xml:"Num,attr"`
	Items []manscdp.Preset `xml:"Item"`
}
type cascadeCruiseItemXML struct {
	Number int    `xml:"Number"`
	Name   string `xml:"Name,omitempty"`
}
type cascadeCruiseListXML struct {
	Num   int                    `xml:"Num,attr"`
	Items []cascadeCruiseItemXML `xml:"CruiseTrack"`
}
type cascadeCruisePointsXML struct {
	Num   int                   `xml:"Num,attr"`
	Items []manscdp.CruisePoint `xml:"CruisePoint"`
}
type cascadeHomePositionXML struct {
	Enabled     int  `xml:"Enabled"`
	ResetTime   *int `xml:"ResetTime,omitempty"`
	PresetIndex *int `xml:"PresetIndex,omitempty"`
}
type cascadePTZResponseXML struct {
	XMLName      xml.Name                `xml:"Response"`
	CmdType      string                  `xml:"CmdType"`
	SN           int                     `xml:"SN"`
	DeviceID     string                  `xml:"DeviceID"`
	SumNum       *int                    `xml:"SumNum,omitempty"`
	PresetList   *cascadePresetListXML   `xml:"PresetList,omitempty"`
	HomePosition *cascadeHomePositionXML `xml:"HomePosition,omitempty"`
	CruiseList   *cascadeCruiseListXML   `xml:"CruiseTrackList,omitempty"`
	Number       *int                    `xml:"Number,omitempty"`
	Name         string                  `xml:"Name,omitempty"`
	CruisePoints *cascadeCruisePointsXML `xml:"CruisePointList,omitempty"`
	Pan          *float64                `xml:"Pan,omitempty"`
	Tilt         *float64                `xml:"Tilt,omitempty"`
	Zoom         *float64                `xml:"Zoom,omitempty"`
	Focus        *float64                `xml:"Focus,omitempty"`
	Iris         *float64                `xml:"Iris,omitempty"`
}

// Rebuild only standard response fields. Unknown device extensions and source
// identifiers are not copied into the upstream publication.
func planCascadePTZQueryResponses(profile protocol.Profile, query cascadePTZQuery, pages [][]byte, fits func([]byte) bool) ([][]byte, error) {
	if len(pages) == 0 {
		return nil, fmt.Errorf("missing PTZ query result")
	}
	base := cascadePTZResponseXML{CmdType: query.CmdType, SN: query.SN, DeviceID: query.DeviceID}
	kind, _ := cascadePTZQueryKind(query.CmdType)
	presets := map[int]manscdp.Preset{}
	tracks := map[int]cascadeCruiseItemXML{}
	total := -1
	var cruisePoints []manscdp.CruisePoint
	cruisePages := make(map[string]struct{})
	var cruiseTrack *manscdp.CruiseTrackResponse
	sourceID, sourceSN := "", ""
	for _, body := range pages {
		var envelope struct {
			XMLName xml.Name `xml:"Response"`
		}
		if err := manscdp.DecodeProfiledXML(protocol.ProfileFor(protocol.Version2016), body, &envelope); err != nil {
			return nil, err
		}
		head, err := manscdp.ParseHead(body)
		if err != nil {
			return nil, err
		}
		sourceKind, known := cascadePTZQueryKind(head.CmdType)
		if !known || sourceKind != kind {
			return nil, fmt.Errorf("PTZ query result command mismatch")
		}
		if sourceID == "" {
			sourceID, sourceSN = head.DeviceID, head.SN
		}
		if head.DeviceID != sourceID || head.SN != sourceSN {
			return nil, fmt.Errorf("PTZ query result identity changed")
		}
		switch query.CmdType {
		case manscdp.CmdPresetQuery:
			result, err := manscdp.ParsePresetResponse(body)
			if err != nil {
				return nil, err
			}
			if total >= 0 && total != result.SumNum {
				return nil, fmt.Errorf("preset query total changed")
			}
			total = result.SumNum
			for _, item := range result.Presets {
				if old, ok := presets[item.ID]; ok && old != item {
					return nil, fmt.Errorf("conflicting preset result")
				}
				presets[item.ID] = item
			}
		case manscdp.CmdCruiseTrackListQuery:
			result, err := manscdp.ParseCruiseTrackListResponse(body)
			if err != nil {
				return nil, err
			}
			if total >= 0 && total != result.SumNum {
				return nil, fmt.Errorf("cruise query total changed")
			}
			total = result.SumNum
			for _, item := range result.List.Tracks {
				value := cascadeCruiseItemXML{Number: item.ID, Name: item.Name}
				if old, ok := tracks[item.ID]; ok && old != value {
					return nil, fmt.Errorf("conflicting cruise result")
				}
				tracks[item.ID] = value
			}
		case manscdp.CmdHomePositionQuery:
			if len(pages) != 1 {
				return nil, fmt.Errorf("duplicate home position result")
			}
			result, err := manscdp.ParseHomePositionResponse(body, manscdp.HomePositionParseOptions{AllowBooleanEnabled: true})
			if err != nil {
				return nil, err
			}
			if result.HomePosition != nil {
				home := result.HomePosition
				base.HomePosition = &cascadeHomePositionXML{ResetTime: home.ResetTime, PresetIndex: home.PresetIndex}
				if home.Enabled {
					base.HomePosition.Enabled = 1
				}
			}
		case manscdp.CmdCruiseTrackQuery:
			result, err := manscdp.ParseCruiseTrackResponse(body)
			if err != nil {
				return nil, err
			}
			if query.Number == nil || result.ID != *query.Number {
				return nil, fmt.Errorf("mismatched cruise detail")
			}
			if cruiseTrack != nil && (result.Name != cruiseTrack.Name || result.SumNum != cruiseTrack.SumNum) {
				return nil, fmt.Errorf("cruise detail identity or total changed")
			}
			copy := *result
			cruiseTrack = &copy
			if _, duplicate := cruisePages[string(body)]; !duplicate {
				cruisePages[string(body)] = struct{}{}
				cruisePoints = append(cruisePoints, result.PointList.Points...)
			}
		case manscdp.CmdPTZPosition, manscdp.CmdPTZPreciseStatusQuery:
			if len(pages) != 1 {
				return nil, fmt.Errorf("duplicate precise status result")
			}
			result, err := manscdp.ParsePTZPreciseStatusResponse(body)
			if err != nil {
				return nil, err
			}
			base.Pan, base.Tilt, base.Zoom, base.Focus, base.Iris = result.Pan, result.Tilt, result.Zoom, result.Focus, result.Iris
		}
	}
	if query.CmdType == manscdp.CmdCruiseTrackQuery {
		if cruiseTrack == nil || cruiseTrack.SumNum != len(cruisePoints) || cruiseTrack.SumNum > 10000 {
			return nil, fmt.Errorf("incomplete cruise detail result")
		}
		base.Number, base.Name, base.SumNum = &cruiseTrack.ID, cruiseTrack.Name, &cruiseTrack.SumNum
		if cruiseTrack.SumNum > 0 {
			base.CruisePoints = &cascadeCruisePointsXML{Num: len(cruisePoints), Items: cruisePoints}
		}
	}
	var responses [][]byte
	marshalResponse := func(response cascadePTZResponseXML) ([]byte, error) {
		return manscdp.MarshalProfiledXML(profile, response)
	}
	if query.CmdType == manscdp.CmdPresetQuery || query.CmdType == manscdp.CmdCruiseTrackListQuery || query.CmdType == manscdp.CmdCruiseTrackQuery {
		count := len(presets)
		if query.CmdType == manscdp.CmdCruiseTrackListQuery {
			count = len(tracks)
		}
		if query.CmdType == manscdp.CmdCruiseTrackQuery {
			count = len(cruisePoints)
			total = cruiseTrack.SumNum
		}
		if total < 0 || total != count || total > 10000 {
			return nil, fmt.Errorf("incomplete PTZ query list")
		}
		base.SumNum = &total
		ids := make([]int, 0, count)
		if query.CmdType == manscdp.CmdPresetQuery {
			for id := range presets {
				ids = append(ids, id)
			}
		} else if query.CmdType == manscdp.CmdCruiseTrackListQuery {
			for id := range tracks {
				ids = append(ids, id)
			}
		}
		sort.Ints(ids)
		for start := 0; start < count || start == 0; {
			end := start + 64
			if end > count {
				end = count
			}
			for {
				response := base
				switch query.CmdType {
				case manscdp.CmdPresetQuery:
					response.PresetList = &cascadePresetListXML{Num: end - start}
					for _, id := range ids[start:end] {
						response.PresetList.Items = append(response.PresetList.Items, presets[id])
					}
				case manscdp.CmdCruiseTrackListQuery:
					if count > 0 {
						response.CruiseList = &cascadeCruiseListXML{Num: end - start}
						for _, id := range ids[start:end] {
							response.CruiseList.Items = append(response.CruiseList.Items, tracks[id])
						}
					}
				case manscdp.CmdCruiseTrackQuery:
					if count > 0 {
						response.CruisePoints = &cascadeCruisePointsXML{Num: end - start, Items: cruisePoints[start:end]}
					}
				}
				body, err := marshalResponse(response)
				if err != nil {
					return nil, err
				}
				if fits == nil || fits(body) {
					responses = append(responses, body)
					break
				}
				if end-start <= 1 {
					return nil, fmt.Errorf("PTZ query item exceeds transport packet limit")
				}
				end = start + (end-start)/2
			}
			if count == 0 {
				break
			}
			start = end
		}
	} else {
		body, err := marshalResponse(base)
		if err != nil {
			return nil, err
		}
		if fits != nil && !fits(body) {
			return nil, fmt.Errorf("PTZ query result exceeds transport packet limit")
		}
		responses = append(responses, body)
	}
	return responses, nil
}
