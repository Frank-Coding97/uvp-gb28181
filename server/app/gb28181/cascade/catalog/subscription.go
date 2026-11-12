package catalog

import (
	"encoding/xml"
	"fmt"
	"sort"

	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

// SubscriptionItems allows only actual published targets, including individual channels.
func SubscriptionItems(s Snapshot, local, target string) ([]CatalogItem, error) {
	for _, item := range s.Items {
		if item.ID == target && item.Kind == CatalogItemChannel {
			return []CatalogItem{item}, nil
		}
	}
	return catalogItemsForQuery(s, local, target)
}

type CatalogNotification struct {
	Body    []byte
	Changes []CatalogChange
}

type CatalogChange struct {
	Item  CatalogItem
	Event string
}

// DiffCatalog compares published facts; status and metadata changes need not
// increment the sharing projection revision. Deletions carry no old metadata.
func DiffCatalog(previous, current []CatalogItem) []CatalogChange {
	changes := make([]CatalogChange, 0)
	old := make(map[string]CatalogItem, len(previous))
	now := make(map[string]CatalogItem, len(current))
	for _, item := range previous {
		old[item.ID] = item
	}
	for _, item := range current {
		now[item.ID] = item
	}
	var removed []string
	for id := range old {
		if _, ok := now[id]; !ok {
			removed = append(removed, id)
		}
	}
	sort.Strings(removed)
	for _, id := range removed {
		changes = append(changes, CatalogChange{CatalogItem{ID: id}, "DEL"})
	}
	for _, item := range current {
		prior, ok := old[item.ID]
		if !ok {
			changes = append(changes, CatalogChange{item, "ADD"})
			continue
		}
		metadata := item
		metadata.Status = prior.Status
		metadata.StatusEvent = prior.StatusEvent
		if metadata != prior {
			changes = append(changes, CatalogChange{item, "UPDATE"})
		}
		if item.StatusEvent != prior.StatusEvent && (item.StatusEvent == ResourceStatusVideoLost || item.StatusEvent == ResourceStatusDefect) {
			changes = append(changes, CatalogChange{CatalogItem{ID: item.ID}, string(item.StatusEvent)})
		}
		clearedFault := (prior.StatusEvent == ResourceStatusVideoLost || prior.StatusEvent == ResourceStatusDefect) && item.StatusEvent == "" && item.Status == ResourceStatusOffline
		if (clearedFault || (item.StatusEvent != ResourceStatusVideoLost && item.StatusEvent != ResourceStatusDefect && item.Status != prior.Status)) && (item.Status == ResourceStatusOnline || item.Status == ResourceStatusOffline) {
			changes = append(changes, CatalogChange{CatalogItem{ID: item.ID}, string(item.Status)})
		}
	}
	return changes
}

type catalogNotifyXML struct {
	XMLName    xml.Name              `xml:"Notify"`
	CmdType    string                `xml:"CmdType"`
	SN         int                   `xml:"SN"`
	DeviceID   string                `xml:"DeviceID"`
	SumNum     int                   `xml:"SumNum"`
	DeviceList *catalogNotifyListXML `xml:"DeviceList,omitempty"`
}
type catalogNotifyListXML struct {
	Num   int                    `xml:"Num,attr"`
	Items []catalogNotifyItemXML `xml:"Item"`
}
type catalogNotifyItemXML struct {
	catalogItemXML
	Event string
}

func (item catalogNotifyItemXML) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	if item.Event != "ADD" && item.Event != "UPDATE" {
		return e.EncodeElement(struct {
			DeviceID string `xml:"DeviceID"`
			Event    string `xml:"Event"`
		}{item.DeviceID, item.Event}, start)
	}
	if err := e.EncodeToken(start); err != nil {
		return err
	}
	fields := []struct {
		name  string
		value any
	}{
		{"DeviceID", item.DeviceID}, {"Event", item.Event}, {"Name", item.Name},
		{"Manufacturer", item.Manufacturer}, {"Model", item.Model},
		{"CivilCode", item.CivilCode}, {"Address", item.Address}, {"Parental", item.Parental},
	}
	if item.kind == CatalogItemCivil {
		fields = fields[:3]
	} else if item.version == protocol.Version2016 {
		// Owner was removed from itemType in 2022; 2016 retains it.
		fields = append(fields[:5], append([]struct {
			name  string
			value any
		}{{"Owner", item.Owner}}, fields[5:]...)...)
	}
	for _, f := range fields {
		if err := e.EncodeElement(f.value, xml.StartElement{Name: xml.Name{Local: f.name}}); err != nil {
			return err
		}
	}
	if item.kind != CatalogItemCivil {
		if item.ParentID != "" {
			if err := e.EncodeElement(item.ParentID, xml.StartElement{Name: xml.Name{Local: "ParentID"}}); err != nil {
				return err
			}
		}
		// Appendix A.2.1.9 defines RegisterWay's default as 1.
		if err := e.EncodeElement(1, xml.StartElement{Name: xml.Name{Local: "RegisterWay"}}); err != nil {
			return err
		}
		if err := e.EncodeElement(item.Secrecy, xml.StartElement{Name: xml.Name{Local: "Secrecy"}}); err != nil {
			return err
		}
		if item.Status != "" {
			if err := e.EncodeElement(item.Status, xml.StartElement{Name: xml.Name{Local: "Status"}}); err != nil {
				return err
			}
		}
		if item.ptzType > 0 || (item.version == protocol.Version2016 && item.BusinessGroupID != "") {
			info := catalogItemInfoXML{PTZType: item.ptzType}
			if item.version == protocol.Version2016 {
				info.BusinessGroupID = item.BusinessGroupID
			}
			if err := e.EncodeElement(info, xml.StartElement{Name: xml.Name{Local: "Info"}}); err != nil {
				return err
			}
		} else if item.BusinessGroupID != "" {
			if err := e.EncodeElement(item.BusinessGroupID, xml.StartElement{Name: xml.Name{Local: "BusinessGroupID"}}); err != nil {
				return err
			}
		}
	}
	return e.EncodeToken(start.End())
}

// Each event batch uses its own Num/SumNum and the subscription's original SN.
func PlanCatalogNotifications(profile protocol.Profile, target string, sn int, changes []CatalogChange, batchSize int, fits func([]byte) bool) ([]CatalogNotification, error) {
	if target == "" || sn < 1 || batchSize < 1 {
		return nil, ErrInvalidCatalogQuery
	}
	marshal := func(part []CatalogChange) ([]byte, error) {
		n := catalogNotifyXML{CmdType: manscdp.CmdCatalog, SN: sn, DeviceID: target, SumNum: len(part)}
		if len(part) > 0 {
			n.DeviceList = &catalogNotifyListXML{Num: len(part)}
			for _, c := range part {
				i := c.Item
				switch c.Event {
				case "ADD", "UPDATE", "DEL", "ON", "OFF", "VLOST", "DEFECT":
				default:
					return nil, fmt.Errorf("unsupported catalog event")
				}
				n.DeviceList.Items = append(n.DeviceList.Items, catalogNotifyItemXML{catalogItemXML: catalogItemXML{
					kind: i.Kind, version: profile.Version, ptzType: i.PTZType, DeviceID: i.ID, Name: i.Name, Manufacturer: i.Manufacturer, Model: i.Model,
					Owner: i.Owner, CivilCode: i.CivilCode, Address: i.Address, Parental: i.Parental, ParentID: i.ParentID,
					BusinessGroupID: i.BusinessGroupID, Secrecy: i.Secrecy, Status: string(i.Status),
				}, Event: c.Event})
			}
		}
		return manscdp.MarshalProfiledXML(profile, n)
	}
	if len(changes) == 0 {
		b, e := marshal(nil)
		if e != nil {
			return nil, e
		}
		if fits != nil && !fits(b) {
			return nil, fmt.Errorf("catalog notification exceeds UDP limit")
		}
		return []CatalogNotification{{Body: b}}, nil
	}
	var bodies []CatalogNotification
	for start := 0; start < len(changes); {
		end := min(start+batchSize, len(changes))
		b, e := marshal(changes[start:end])
		if e != nil {
			return nil, e
		}
		for fits != nil && !fits(b) {
			if end-start == 1 {
				return nil, fmt.Errorf("catalog notification exceeds UDP limit; configure TCP")
			}
			end = start + (end-start)/2
			b, e = marshal(changes[start:end])
			if e != nil {
				return nil, e
			}
		}
		bodies = append(bodies, CatalogNotification{Body: b, Changes: append([]CatalogChange(nil), changes[start:end]...)})
		start = end
	}
	return bodies, nil
}

// ApplyCatalogChanges advances only events accepted in one SIP notification.
func ApplyCatalogChanges(items []CatalogItem, changes []CatalogChange) []CatalogItem {
	out := append([]CatalogItem(nil), items...)
	for _, change := range changes {
		index := -1
		for i := range out {
			if out[i].ID == change.Item.ID {
				index = i
				break
			}
		}
		switch change.Event {
		case "DEL":
			if index >= 0 {
				out = append(out[:index], out[index+1:]...)
			}
		case "ADD", "UPDATE":
			if index < 0 {
				out = append(out, change.Item)
			} else {
				out[index] = change.Item
			}
		case "ON", "OFF":
			if index >= 0 {
				out[index].Status = ResourceStatus(change.Event)
				out[index].StatusEvent = ""
			}
		case "VLOST", "DEFECT":
			if index >= 0 {
				out[index].Status = ResourceStatusOffline
				out[index].StatusEvent = ResourceStatus(change.Event)
			}
		}
	}
	return out
}
