package catalog

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

var (
	ErrInvalidCatalogQuery  = errors.New("cascade catalog: invalid catalog query")
	ErrUnknownCatalogTarget = errors.New("cascade catalog: unknown catalog target")
	ErrInvalidCatalogBatch  = errors.New("cascade catalog: invalid catalog batch size")
)

// CatalogResponse is one already-encoded MANSCDP response. Content-Type and
// SIP transaction handling remain at the SIP boundary.
type CatalogResponse struct {
	SN     int
	SumNum int
	Body   []byte
}

type CatalogSendResult struct {
	SentBatches int
	Completed   bool
}

// CatalogResponseSender synchronously reports each response result. This makes
// the caller wait for a batch before moving to the next one.
type CatalogResponseSender interface {
	SendCatalogResponse(context.Context, []byte) error
}

// ParseCatalogQuery decodes and validates the supported Catalog Query shape.
func ParseCatalogQuery(profile protocol.Profile, body []byte) (manscdp.CatalogQuery, error) {
	var query manscdp.CatalogQuery
	if err := manscdp.DecodeProfiledXML(profile, body, &query); err != nil {
		return manscdp.CatalogQuery{}, fmt.Errorf("%w: decode: %v", ErrInvalidCatalogQuery, err)
	}
	query.CmdType = strings.TrimSpace(query.CmdType)
	query.DeviceID = strings.TrimSpace(query.DeviceID)
	if query.XMLName.Local != "Query" || query.CmdType != manscdp.CmdCatalog || query.SN < 1 || query.DeviceID == "" {
		return manscdp.CatalogQuery{}, ErrInvalidCatalogQuery
	}
	return query, nil
}

// PlanCatalogResponses converts exactly one immutable projection snapshot into
// encoded response batches. Every response uses the request SN and full item
// count, as required for a multi-response Catalog result.
func PlanCatalogResponses(profile protocol.Profile, targetID string, snapshot Snapshot, batchSize int, body []byte) ([]CatalogResponse, error) {
	query, err := ParseCatalogQuery(profile, body)
	if err != nil {
		return nil, err
	}
	if query.DeviceID != strings.TrimSpace(targetID) {
		return nil, fmt.Errorf("%w: %q", ErrUnknownCatalogTarget, query.DeviceID)
	}
	if batchSize <= 0 {
		return nil, ErrInvalidCatalogBatch
	}

	total := len(snapshot.Items)
	if total == 0 {
		response, err := marshalCatalogResponse(profile, query, nil, 0)
		if err != nil {
			return nil, err
		}
		return []CatalogResponse{response}, nil
	}

	responses := make([]CatalogResponse, 0, (total+batchSize-1)/batchSize)
	for start := 0; start < total; start += batchSize {
		end := start + batchSize
		if end > total {
			end = total
		}
		response, err := marshalCatalogResponse(profile, query, snapshot.Items[start:end], total)
		if err != nil {
			return nil, err
		}
		responses = append(responses, response)
	}
	return responses, nil
}

// SendCatalogResponses sends response batches in call order. On failure it
// reports only fully accepted preceding batches as sent and does not advance.
func SendCatalogResponses(ctx context.Context, sender CatalogResponseSender, responses []CatalogResponse) (CatalogSendResult, error) {
	if sender == nil {
		return CatalogSendResult{}, errors.New("cascade catalog: nil response sender")
	}
	result := CatalogSendResult{Completed: true}
	for _, response := range responses {
		if err := sender.SendCatalogResponse(ctx, response.Body); err != nil {
			result.Completed = false
			return result, err
		}
		result.SentBatches++
	}
	return result, nil
}

type catalogResponseXML struct {
	XMLName    xml.Name              `xml:"Response"`
	CmdType    string                `xml:"CmdType"`
	SN         int                   `xml:"SN"`
	DeviceID   string                `xml:"DeviceID"`
	SumNum     int                   `xml:"SumNum"`
	DeviceList *catalogDeviceListXML `xml:"DeviceList,omitempty"`
}

type catalogDeviceListXML struct {
	Num   int              `xml:"Num,attr"`
	Items []catalogItemXML `xml:"Item"`
}

// catalogItemXML is intentionally separate from the device-side parser DTO.
// It emits only facts present in the platform projection and never turns an
// unknown PTZ type or coordinate into a fabricated zero value.
type catalogItemXML struct {
	DeviceID     string `xml:"DeviceID"`
	Name         string `xml:"Name"`
	Manufacturer string `xml:"Manufacturer"`
	Model        string `xml:"Model"`
	Owner        string `xml:"Owner"`
	CivilCode    string `xml:"CivilCode"`
	Address      string `xml:"Address"`
	Parental     int    `xml:"Parental"`
	ParentID     string `xml:"ParentID,omitempty"`
	Secrecy      int    `xml:"Secrecy"`
	Status       string `xml:"Status"`
}

func marshalCatalogResponse(profile protocol.Profile, query manscdp.CatalogQuery, items []CatalogItem, total int) (CatalogResponse, error) {
	response := catalogResponseXML{
		CmdType:  manscdp.CmdCatalog,
		SN:       query.SN,
		DeviceID: query.DeviceID,
		SumNum:   total,
	}
	if len(items) > 0 {
		response.DeviceList = &catalogDeviceListXML{Num: len(items), Items: make([]catalogItemXML, 0, len(items))}
		for _, item := range items {
			response.DeviceList.Items = append(response.DeviceList.Items, catalogItemXML{
				DeviceID:     item.ID,
				Name:         item.Name,
				Manufacturer: item.Manufacturer,
				Model:        item.Model,
				Owner:        item.Owner,
				CivilCode:    item.CivilCode,
				Address:      item.Address,
				Parental:     item.Parental,
				ParentID:     item.ParentID,
				Secrecy:      item.Secrecy,
				Status:       string(item.Status),
			})
		}
	}
	body, err := manscdp.MarshalProfiledXML(profile, response)
	if err != nil {
		return CatalogResponse{}, err
	}
	return CatalogResponse{SN: query.SN, SumNum: total, Body: body}, nil
}
