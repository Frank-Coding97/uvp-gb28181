package catalog

import (
	"sort"
	"strings"

	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
)

const CatalogItemCivil CatalogItemKind = "civil"

type PublicationSettings struct {
	PlatformName string
	Manufacturer string
	Model        string
	CivilCode    string
	Address      string
	CivilName    func(string) string
}

// Publish retains the authorization map while exposing only selected channels
// and the optional platform, business and administrative directories.
func (s Snapshot) Publish(platform model.GbCascadePlatform, settings PublicationSettings) Snapshot {
	out := s.ChannelsOnly()
	omittedDevices := make(map[string]struct{})
	hasBusinessDirectory := false
	if platform.PublishGroup {
		for _, item := range s.Items {
			if item.Kind == CatalogItemBusinessGroup || item.Kind == CatalogItemVirtualOrganization {
				hasBusinessDirectory = true
				break
			}
		}
	}
	if hasBusinessDirectory {
		out = s
		out.Items = append([]CatalogItem(nil), s.Items...)
		out.publishedIDs = make(map[string]struct{}, len(s.publishedIDs))
		for id := range s.publishedIDs {
			out.publishedIDs[id] = struct{}{}
		}
		// Administrative regions remain optional even when the business tree is published.
		filtered := out.Items[:0]
		for _, item := range out.Items {
			if item.Kind == CatalogItemDevice {
				omittedDevices[item.ID] = struct{}{}
				delete(out.publishedIDs, item.ID)
				continue
			}
			if item.Kind == CatalogItemCivil && !platform.PublishCivil {
				delete(out.publishedIDs, item.ID)
				continue
			}
			filtered = append(filtered, item)
		}
		out.Items = filtered
	}
	for i := range out.Items {
		item := &out.Items[i]
		if item.Kind == CatalogItemPlatform && item.ID == platform.LocalDeviceID {
			if settings.PlatformName != "" {
				item.Name = settings.PlatformName
			}
			item.Manufacturer, item.Model = settings.Manufacturer, settings.Model
			item.CivilCode, item.Address = settings.CivilCode, settings.Address
			continue
		}
		if item.Kind != CatalogItemChannel {
			continue
		}
		target, _ := s.LookupPublishedChannel(item.ID)
		metadata := s.channelMetadata[target.SourceChannelID]
		item.Manufacturer, item.Model, item.Owner = metadata.Manufacturer, metadata.Model, metadata.Owner
		item.CivilCode, item.Address, item.Secrecy = metadata.CivilCode, metadata.Address, metadata.Secrecy
		// PTZType is a capability signal: upstream platforms (EasyGBS, for one)
		// hide the whole PTZ panel for a channel whose catalog entry omits it.
		// Publish it only when PTZ is actually shared with this upstream, so the
		// UI cannot offer control that the control boundary would reject.
		if target.PTZAllowed {
			item.PTZType = metadata.PTZType
		}
		if !validCivilCode(item.CivilCode) {
			item.CivilCode = settings.CivilCode
		}
		if platform.PublishPlatform && !hasBusinessDirectory {
			item.ParentID = platform.LocalDeviceID
		}
		// Keep the organization path while replacing an omitted physical parent.
		parent, organization, grouped := strings.Cut(item.ParentID, "/")
		if _, omitted := omittedDevices[parent]; omitted {
			item.ParentID = platform.LocalDeviceID
			if grouped {
				item.ParentID += "/" + organization
			}
		}
	}
	var prefix []CatalogItem
	if platform.PublishPlatform && !hasBusinessDirectory {
		prefix = append(prefix, CatalogItem{ID: platform.LocalDeviceID, Kind: CatalogItemPlatform, Name: settings.PlatformName,
			Manufacturer: settings.Manufacturer, Model: settings.Model, CivilCode: settings.CivilCode, Address: settings.Address, Status: ResourceStatusOnline})
	}
	if platform.PublishCivil {
		codes := make(map[string]struct{})
		add := func(code string) {
			if !validCivilCode(code) {
				return
			}
			for length := 2; length <= len(code); length += 2 {
				part := code[:length]
				if strings.HasSuffix(part, "00") {
					continue
				}
				codes[part] = struct{}{}
			}
		}
		if platform.PublishPlatform {
			add(settings.CivilCode)
		}
		for _, item := range out.Items {
			add(item.CivilCode)
		}
		ordered := make([]string, 0, len(codes))
		for code := range codes {
			ordered = append(ordered, code)
		}
		sort.Strings(ordered)
		for _, code := range ordered {
			name := code
			if settings.CivilName != nil {
				if resolved := settings.CivilName(code); resolved != "" {
					name = resolved
				}
			}
			prefix = append(prefix, CatalogItem{ID: code, Name: name, Kind: CatalogItemCivil})
		}
	}
	out.Items = append(prefix, out.Items...)
	for _, item := range prefix {
		out.publishedIDs[item.ID] = struct{}{}
	}
	out.SumNum = len(out.Items)
	return out
}

func validCivilCode(code string) bool {
	if len(code) != 2 && len(code) != 4 && len(code) != 6 && len(code) != 8 {
		return false
	}
	for _, digit := range code {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return !strings.HasPrefix(code, "00")
}
