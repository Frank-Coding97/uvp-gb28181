package catalog

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"

	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
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
	return PlanCatalogResponsesWithinLimit(profile, targetID, snapshot, batchSize, body, nil)
}

// PlanCatalogResponsesWithinLimit preserves SN and total while respecting encoded transport size.
func PlanCatalogResponsesWithinLimit(profile protocol.Profile, targetID string, snapshot Snapshot, batchSize int, body []byte, fits func([]byte) bool) ([]CatalogResponse, error) {
	query, err := ParseCatalogQuery(profile, body)
	if err != nil {
		return nil, err
	}
	if batchSize <= 0 {
		return nil, ErrInvalidCatalogBatch
	}

	items, err := catalogItemsForQuery(snapshot, targetID, query.DeviceID)
	if err != nil {
		return nil, err
	}
	total := len(items)
	if total == 0 {
		response, err := marshalCatalogResponse(profile, query, nil, 0)
		if err != nil {
			return nil, err
		}
		return []CatalogResponse{response}, nil
	}

	responses := make([]CatalogResponse, 0, (total+batchSize-1)/batchSize)
	for start := 0; start < total; {
		end := start + batchSize
		if end > total {
			end = total
		}
		response, err := marshalCatalogResponse(profile, query, items[start:end], total)
		if err != nil {
			return nil, err
		}
		for fits != nil && !fits(response.Body) {
			if end-start == 1 {
				return nil, fmt.Errorf("catalog item exceeds UDP packet limit; configure TCP transport")
			}
			end = start + (end-start)/2
			response, err = marshalCatalogResponse(profile, query, items[start:end], total)
			if err != nil {
				return nil, err
			}
		}
		responses = append(responses, response)
		start = end
	}
	return responses, nil
}

func catalogItemsForQuery(snapshot Snapshot, localDeviceID, requestedID string) ([]CatalogItem, error) {
	if requestedID == strings.TrimSpace(localDeviceID) {
		return snapshot.Items, nil
	}
	var target *CatalogItem
	for i := range snapshot.Items {
		if snapshot.Items[i].ID == requestedID {
			target = &snapshot.Items[i]
			break
		}
	}
	if target == nil || (target.Kind != CatalogItemBusinessGroup && target.Kind != CatalogItemVirtualOrganization) {
		return nil, fmt.Errorf("%w: %q", ErrUnknownCatalogTarget, requestedID)
	}

	selectedOrganizations := map[string]struct{}{}
	if target.Kind == CatalogItemVirtualOrganization {
		selectedOrganizations[target.ID] = struct{}{}
	} else {
		for _, item := range snapshot.Items {
			if item.Kind == CatalogItemVirtualOrganization && item.BusinessGroupID == target.ID {
				selectedOrganizations[item.ID] = struct{}{}
			}
		}
	}
	if len(selectedOrganizations) > 0 {
		for changed := true; changed; {
			changed = false
			for _, item := range snapshot.Items {
				if item.Kind != CatalogItemVirtualOrganization {
					continue
				}
				if _, selected := selectedOrganizations[item.ID]; selected {
					continue
				}
				if containsCatalogParent(selectedOrganizations, item.ParentID) {
					selectedOrganizations[item.ID] = struct{}{}
					changed = true
				}
			}
		}
	}

	items := make([]CatalogItem, 0, len(snapshot.Items))
	for _, item := range snapshot.Items {
		include := false
		if target.Kind == CatalogItemBusinessGroup {
			_, isOrganization := selectedOrganizations[item.ID]
			include = item.ID == target.ID || isOrganization
			if !include && item.Kind == CatalogItemChannel {
				include = containsCatalogParent(selectedOrganizations, item.ParentID)
			}
		} else {
			_, include = selectedOrganizations[item.ID]
			if !include && item.Kind == CatalogItemChannel {
				include = containsCatalogParent(selectedOrganizations, item.ParentID)
			}
		}
		if include {
			items = append(items, item)
		}
	}
	return items, nil
}

func containsCatalogParent(ancestors map[string]struct{}, parentID string) bool {
	for _, part := range strings.Split(parentID, "/") {
		if _, ok := ancestors[strings.TrimSpace(part)]; ok {
			return true
		}
	}
	return false
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
	DeviceID        string              `xml:"DeviceID"`
	Name            string              `xml:"Name"`
	Manufacturer    string              `xml:"Manufacturer"`
	Model           string              `xml:"Model"`
	Owner           string              `xml:"Owner"`
	CivilCode       string              `xml:"CivilCode"`
	Address         string              `xml:"Address"`
	Parental        int                 `xml:"Parental"`
	ParentID        string              `xml:"ParentID,omitempty"`
	BusinessGroupID string              `xml:"BusinessGroupID,omitempty"`
	Info            *catalogItemInfoXML `xml:"Info,omitempty"`
	Secrecy         int                 `xml:"Secrecy"`
	Status          string              `xml:"Status"`
}

type catalogItemInfoXML struct {
	BusinessGroupID string `xml:"BusinessGroupID,omitempty"`
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
			wireItem := catalogItemXML{
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
			}
			if item.BusinessGroupID != "" {
				if profile.Version == protocol.Version(protocol.Version2016) {
					wireItem.Info = &catalogItemInfoXML{BusinessGroupID: item.BusinessGroupID}
				} else {
					wireItem.BusinessGroupID = item.BusinessGroupID
				}
			}
			response.DeviceList.Items = append(response.DeviceList.Items, wireItem)
		}
	}
	body, err := manscdp.MarshalProfiledXML(profile, response)
	if err != nil {
		return CatalogResponse{}, err
	}
	return CatalogResponse{SN: query.SN, SumNum: total, Body: body}, nil
}
