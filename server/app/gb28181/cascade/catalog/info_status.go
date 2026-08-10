package catalog

import (
	"encoding/xml"
	"errors"
	"fmt"
	"strings"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

var (
	ErrInvalidInfoStatusQuery  = errors.New("cascade catalog: invalid info/status query")
	ErrUnknownInfoStatusTarget = errors.New("cascade catalog: unknown info/status target")
	ErrMissingDeviceStatusFact = errors.New("cascade catalog: missing device status fact")
	ErrInvalidDeviceStatusFact = errors.New("cascade catalog: invalid device status fact")
)

type InfoStatusResponse struct {
	SN       int
	DeviceID string
	Body     []byte
}

// PlatformDeviceInfo contains only metadata explicitly approved for the local
// platform identity. Published device metadata is always read from Snapshot.
type PlatformDeviceInfo struct {
	DeviceID     string
	DeviceName   string
	Manufacturer string
	Model        string
	Firmware     string
	Channel      *int
}

type DeviceStatusFacts map[string]DeviceStatusFact

// DeviceStatusFact is an operation-scoped fact supplied by the caller. In
// particular, Online is not inferred from cascade registration or Catalog.
type DeviceStatusFact struct {
	Online     bool
	Working    bool
	Reason     string
	Encode     *bool
	Record     *bool
	DeviceTime string
	Alarm      *AlarmStatusFact
}

// A nil Alarm means unknown and is omitted. A non-nil empty Alarm explicitly
// reports a known empty list with the profile-specific count attribute.
type AlarmStatusFact struct {
	Items []AlarmStatusItemFact
}

type AlarmStatusItemFact struct {
	DeviceID   string
	DutyStatus string
}

type InfoStatusTargetError struct {
	Kind     error
	DeviceID string
}

func (e *InfoStatusTargetError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%v: %q", e.Kind, e.DeviceID)
}

func (e *InfoStatusTargetError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Kind
}

type infoStatusQuery struct {
	XMLName  xml.Name `xml:"Query"`
	CmdType  string   `xml:"CmdType"`
	SN       int      `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
}

func ParseInfoStatusQuery(profile protocol.Profile, command string, body []byte) (InfoStatusQuery, error) {
	var wire infoStatusQuery
	if err := manscdp.DecodeProfiledXML(profile, body, &wire); err != nil {
		return InfoStatusQuery{}, fmt.Errorf("%w: decode: %v", ErrInvalidInfoStatusQuery, err)
	}
	query := InfoStatusQuery{
		CmdType:  strings.TrimSpace(wire.CmdType),
		SN:       wire.SN,
		DeviceID: strings.TrimSpace(wire.DeviceID),
	}
	if wire.XMLName.Local != "Query" || query.CmdType != command || query.SN < 1 || query.DeviceID == "" {
		return InfoStatusQuery{}, ErrInvalidInfoStatusQuery
	}
	return query, nil
}

type InfoStatusQuery struct {
	CmdType  string
	SN       int
	DeviceID string
}

func PlanDeviceInfoResponse(profile protocol.Profile, platform PlatformDeviceInfo, snapshot Snapshot, body []byte) (InfoStatusResponse, error) {
	query, err := ParseInfoStatusQuery(profile, manscdp.CmdDeviceInfo, body)
	if err != nil {
		return InfoStatusResponse{}, err
	}

	response := deviceInfoResponseXML{
		CmdType:  manscdp.CmdDeviceInfo,
		SN:       query.SN,
		DeviceID: query.DeviceID,
		Result:   "OK",
	}
	switch {
	case query.DeviceID == strings.TrimSpace(platform.DeviceID):
		response.DeviceName = optionalString(platform.DeviceName)
		response.Manufacturer = optionalString(platform.Manufacturer)
		response.Model = optionalString(platform.Model)
		response.Firmware = optionalString(platform.Firmware)
		response.Channel = validChannelCount(platform.Channel)
	default:
		item, ok := findPublishedDevice(snapshot, query.DeviceID)
		if !ok {
			return InfoStatusResponse{}, targetError(ErrUnknownInfoStatusTarget, query.DeviceID)
		}
		response.DeviceName = optionalString(item.Name)
		response.Manufacturer = optionalString(item.Manufacturer)
		response.Model = optionalString(item.Model)
	}

	encoded, err := manscdp.MarshalProfiledXML(profile, response)
	if err != nil {
		return InfoStatusResponse{}, err
	}
	return InfoStatusResponse{SN: query.SN, DeviceID: query.DeviceID, Body: encoded}, nil
}

func PlanDeviceStatusResponse(profile protocol.Profile, platformID string, snapshot Snapshot, facts DeviceStatusFacts, body []byte) (InfoStatusResponse, error) {
	query, err := ParseInfoStatusQuery(profile, manscdp.CmdDeviceStatus, body)
	if err != nil {
		return InfoStatusResponse{}, err
	}
	if query.DeviceID != strings.TrimSpace(platformID) {
		if _, ok := findPublishedDevice(snapshot, query.DeviceID); !ok {
			return InfoStatusResponse{}, targetError(ErrUnknownInfoStatusTarget, query.DeviceID)
		}
	}
	fact, ok := facts[query.DeviceID]
	if !ok {
		return InfoStatusResponse{}, targetError(ErrMissingDeviceStatusFact, query.DeviceID)
	}

	response, err := buildDeviceStatusResponse(profile, query, fact)
	if err != nil {
		return InfoStatusResponse{}, err
	}
	encoded, err := manscdp.MarshalProfiledXML(profile, response)
	if err != nil {
		return InfoStatusResponse{}, err
	}
	return InfoStatusResponse{SN: query.SN, DeviceID: query.DeviceID, Body: encoded}, nil
}

type deviceInfoResponseXML struct {
	XMLName      xml.Name `xml:"Response"`
	CmdType      string   `xml:"CmdType"`
	SN           int      `xml:"SN"`
	DeviceID     string   `xml:"DeviceID"`
	DeviceName   *string  `xml:"DeviceName,omitempty"`
	Result       string   `xml:"Result"`
	Manufacturer *string  `xml:"Manufacturer,omitempty"`
	Model        *string  `xml:"Model,omitempty"`
	Firmware     *string  `xml:"Firmware,omitempty"`
	Channel      *int     `xml:"Channel,omitempty"`
}

type deviceStatusResponseXML struct {
	XMLName     xml.Name      `xml:"Response"`
	CmdType     string        `xml:"CmdType"`
	SN          int           `xml:"SN"`
	DeviceID    string        `xml:"DeviceID"`
	Result      string        `xml:"Result"`
	Online      string        `xml:"Online"`
	Status      string        `xml:"Status"`
	Reason      string        `xml:"Reason,omitempty"`
	Encode      *string       `xml:"Encode,omitempty"`
	Record      *string       `xml:"Record,omitempty"`
	DeviceTime  string        `xml:"DeviceTime,omitempty"`
	Alarmstatus *alarmListXML `xml:"Alarmstatus,omitempty"`
}

type alarmListXML struct {
	CountAttribute string
	Items          []alarmItemXML
}

type alarmItemXML struct {
	DeviceID   string `xml:"DeviceID"`
	DutyStatus string `xml:"DutyStatus"`
}

func (a alarmListXML) MarshalXML(encoder *xml.Encoder, start xml.StartElement) error {
	start.Attr = append(start.Attr, xml.Attr{Name: xml.Name{Local: a.CountAttribute}, Value: fmt.Sprintf("%d", len(a.Items))})
	if err := encoder.EncodeToken(start); err != nil {
		return err
	}
	for _, item := range a.Items {
		if err := encoder.EncodeElement(item, xml.StartElement{Name: xml.Name{Local: "Item"}}); err != nil {
			return err
		}
	}
	return encoder.EncodeToken(start.End())
}

func buildDeviceStatusResponse(profile protocol.Profile, query InfoStatusQuery, fact DeviceStatusFact) (deviceStatusResponseXML, error) {
	response := deviceStatusResponseXML{
		CmdType:    manscdp.CmdDeviceStatus,
		SN:         query.SN,
		DeviceID:   query.DeviceID,
		Result:     "OK",
		Online:     boolStatus(fact.Online, "ONLINE", "OFFLINE"),
		Status:     boolStatus(fact.Working, "OK", "ERROR"),
		Reason:     strings.TrimSpace(fact.Reason),
		Encode:     optionalControlStatus(fact.Encode),
		Record:     optionalControlStatus(fact.Record),
		DeviceTime: strings.TrimSpace(fact.DeviceTime),
	}
	if fact.Alarm != nil {
		attribute := "num"
		if profile.Version == protocol.Version(protocol.Version2022) {
			attribute = "Num"
		}
		response.Alarmstatus = &alarmListXML{CountAttribute: attribute, Items: make([]alarmItemXML, 0, len(fact.Alarm.Items))}
		for _, item := range fact.Alarm.Items {
			deviceID := strings.TrimSpace(item.DeviceID)
			dutyStatus := strings.ToUpper(strings.TrimSpace(item.DutyStatus))
			if deviceID == "" || !validDutyStatus(dutyStatus) {
				return deviceStatusResponseXML{}, fmt.Errorf("%w: alarm item", ErrInvalidDeviceStatusFact)
			}
			response.Alarmstatus.Items = append(response.Alarmstatus.Items, alarmItemXML{DeviceID: deviceID, DutyStatus: dutyStatus})
		}
	}
	return response, nil
}

func findPublishedDevice(snapshot Snapshot, deviceID string) (CatalogItem, bool) {
	for _, item := range snapshot.Items {
		if item.Kind == CatalogItemDevice && item.ID == deviceID {
			return item, true
		}
	}
	return CatalogItem{}, false
}

func targetError(kind error, deviceID string) error {
	return &InfoStatusTargetError{Kind: kind, DeviceID: deviceID}
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func validChannelCount(value *int) *int {
	if value == nil || *value < 0 {
		return nil
	}
	result := *value
	return &result
}

func optionalControlStatus(value *bool) *string {
	if value == nil {
		return nil
	}
	status := boolStatus(*value, "ON", "OFF")
	return &status
}

func boolStatus(value bool, on, off string) string {
	if value {
		return on
	}
	return off
}

func validDutyStatus(value string) bool {
	switch value {
	case "ONDUTY", "OFFDUTY", "ALARM":
		return true
	default:
		return false
	}
}
