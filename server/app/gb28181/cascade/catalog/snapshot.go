// Package catalog turns one persisted cascade projection into a read-only,
// platform-scoped Catalog domain snapshot. XML and SIP response handling belong
// to the responder layer, not this package.
package catalog

import (
	"errors"
	"fmt"
	"sort"

	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/repository"
)

var (
	ErrInvalidProjection  = errors.New("cascade catalog: invalid projection")
	ErrInvalidPublishedID = errors.New("cascade catalog: invalid published id")
)

type CatalogItemKind string

const (
	CatalogItemPlatform CatalogItemKind = "platform"
	CatalogItemDevice   CatalogItemKind = "device"
	CatalogItemChannel  CatalogItemKind = "channel"
)

type ResourceStatus string

const (
	ResourceStatusOnline  ResourceStatus = "ON"
	ResourceStatusOffline ResourceStatus = "OFF"
)

// SourceFacts contains the status facts read with the projection. Missing
// entries are offline so a snapshot never invents an online resource.
type SourceFacts struct {
	DeviceOnline  map[uint64]bool
	ChannelOnline map[uint64]bool
}

// CatalogItem is the metadata whitelist available to an outbound Catalog
// responder. It intentionally excludes source database IDs, source network
// endpoints, credentials, and arbitrary source fields.
type CatalogItem struct {
	ID           string
	ParentID     string
	Kind         CatalogItemKind
	Name         string
	Manufacturer string
	Model        string
	Owner        string
	CivilCode    string
	Address      string
	Parental     int
	Secrecy      int
	Status       ResourceStatus
}

type PublishedChannelTarget struct {
	PlatformID      uint64
	SourceDeviceID  uint64
	SourceChannelID uint64
	PTZAllowed      bool
}

// Snapshot is immutable by convention. Its catalog items contain no source
// identifiers; LookupPublishedChannel provides the platform-scoped reverse
// lookup required by later control and media boundaries.
type Snapshot struct {
	PlatformID uint64
	Revision   uint64
	SumNum     int
	Items      []CatalogItem

	publishedIDs map[string]struct{}
	channels     map[string]PublishedChannelTarget
}

func (s Snapshot) HasPublishedID(id string) bool {
	_, ok := s.publishedIDs[id]
	return ok
}

func (s Snapshot) LookupPublishedChannel(id string) (PublishedChannelTarget, bool) {
	target, ok := s.channels[id]
	return target, ok
}

// Build creates a complete, deterministic view from exactly one repository
// snapshot. It never reads live persistence, so the input revision remains the
// sole revision for this result even if authorization changes concurrently.
func Build(projection repository.ProjectionSnapshot, facts SourceFacts) (Snapshot, error) {
	platformID := projection.Platform.ID
	if platformID == 0 {
		return Snapshot{}, fmt.Errorf("%w: platform id", ErrInvalidProjection)
	}

	devices, channels, err := authorizedResources(projection, platformID)
	if err != nil {
		return Snapshot{}, err
	}

	snapshot := Snapshot{
		PlatformID:   platformID,
		Revision:     projection.Revision,
		publishedIDs: make(map[string]struct{}, len(devices)+len(channels)+1),
		channels:     make(map[string]PublishedChannelTarget, len(channels)),
	}
	if projection.Platform.PublishPlatform {
		if err := addPublishedID(snapshot.publishedIDs, projection.Platform.LocalDeviceID); err != nil {
			return Snapshot{}, err
		}
		snapshot.Items = append(snapshot.Items, CatalogItem{
			ID:     projection.Platform.LocalDeviceID,
			Kind:   CatalogItemPlatform,
			Name:   projection.Platform.Name,
			Status: ResourceStatusOnline,
		})
	}

	if err := appendDeviceItems(&snapshot, devices, projection.Platform, facts); err != nil {
		return Snapshot{}, err
	}
	if err := appendChannelItems(&snapshot, platformID, devices, channels, facts); err != nil {
		return Snapshot{}, err
	}
	snapshot.SumNum = len(snapshot.Items)
	return snapshot, nil
}

// appendDeviceItems 把设备投影转为目录项并按发布 ID 注册
func appendDeviceItems(snapshot *Snapshot, devices map[uint64]model.GbCascadeDeviceProjection, platform model.GbCascadePlatform, facts SourceFacts) error {
	for _, device := range sortedDevices(devices) {
		if err := addPublishedID(snapshot.publishedIDs, device.PublishedDeviceID); err != nil {
			return err
		}
		snapshot.Items = append(snapshot.Items, deviceCatalogItem(device, platform.LocalDeviceID, platform.PublishPlatform, facts))
	}
	return nil
}

// appendChannelItems 把通道投影转为目录项与播放目标映射
func appendChannelItems(snapshot *Snapshot, platformID uint64, devices map[uint64]model.GbCascadeDeviceProjection, channels []model.GbCascadeChannelProjection, facts SourceFacts) error {
	for _, channel := range channels {
		if err := addPublishedID(snapshot.publishedIDs, channel.PublishedChannelID); err != nil {
			return err
		}
	}
	for _, channel := range channels {
		parentID := deviceParentID(channel, devices)
		if channel.ParentOverride != "" {
			if _, ok := snapshot.publishedIDs[channel.ParentOverride]; ok {
				parentID = channel.ParentOverride
			}
		}
		device := devices[channel.DeviceProjectionID]
		snapshot.Items = append(snapshot.Items, CatalogItem{
			ID:       channel.PublishedChannelID,
			ParentID: parentID,
			Kind:     CatalogItemChannel,
			Name:     channel.Name,
			Status:   channelStatus(device, channel, facts),
		})
		snapshot.channels[channel.PublishedChannelID] = PublishedChannelTarget{
			PlatformID:      platformID,
			SourceDeviceID:  device.SourceDeviceID,
			SourceChannelID: channel.SourceChannelID,
			PTZAllowed:      channel.PTZAllowed,
		}
	}
	return nil
}

func sortedDevices(devices map[uint64]model.GbCascadeDeviceProjection) []model.GbCascadeDeviceProjection {
	rows := make([]model.GbCascadeDeviceProjection, 0, len(devices))
	for _, device := range devices {
		rows = append(rows, device)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].PublishedDeviceID < rows[j].PublishedDeviceID })
	return rows
}

func authorizedResources(projection repository.ProjectionSnapshot, platformID uint64) (map[uint64]model.GbCascadeDeviceProjection, []model.GbCascadeChannelProjection, error) {
	allDevices, err := validateDeviceProjections(projection, platformID)
	if err != nil {
		return nil, nil, err
	}
	channels, usedDevices, err := validateChannelProjections(projection, platformID, allDevices)
	if err != nil {
		return nil, nil, err
	}
	devices := make(map[uint64]model.GbCascadeDeviceProjection, len(usedDevices))
	for id := range usedDevices {
		devices[id] = allDevices[id]
	}
	return devices, channels, nil
}

// validateDeviceProjections 校验并收集激活设备投影
func validateDeviceProjections(projection repository.ProjectionSnapshot, platformID uint64) (map[uint64]model.GbCascadeDeviceProjection, error) {
	allDevices := make(map[uint64]model.GbCascadeDeviceProjection, len(projection.Devices))
	sourceDevices := make(map[uint64]struct{}, len(projection.Devices))
	for _, device := range projection.Devices {
		if device.PlatformID != platformID {
			return nil, fmt.Errorf("%w: device platform", ErrInvalidProjection)
		}
		if device.ID == 0 || device.SourceDeviceID == 0 {
			return nil, fmt.Errorf("%w: device identity", ErrInvalidProjection)
		}
		if !device.Active {
			continue
		}
		if err := validatePublishedID(device.PublishedDeviceID); err != nil {
			return nil, err
		}
		if _, exists := allDevices[device.ID]; exists {
			return nil, fmt.Errorf("%w: duplicate device projection", ErrInvalidProjection)
		}
		if _, exists := sourceDevices[device.SourceDeviceID]; exists {
			return nil, fmt.Errorf("%w: duplicate source device", ErrInvalidProjection)
		}
		allDevices[device.ID] = device
		sourceDevices[device.SourceDeviceID] = struct{}{}
	}
	return allDevices, nil
}

// validateChannelProjections 校验并收集激活通道投影及被引用的设备
func validateChannelProjections(projection repository.ProjectionSnapshot, platformID uint64, allDevices map[uint64]model.GbCascadeDeviceProjection) ([]model.GbCascadeChannelProjection, map[uint64]struct{}, error) {
	channels := make([]model.GbCascadeChannelProjection, 0, len(projection.Channels))
	usedDevices := make(map[uint64]struct{}, len(projection.Channels))
	sourceChannels := make(map[uint64]struct{}, len(projection.Channels))
	for _, channel := range projection.Channels {
		if channel.PlatformID != platformID {
			return nil, nil, fmt.Errorf("%w: channel platform", ErrInvalidProjection)
		}
		if !channel.Active {
			continue
		}
		device, ok := allDevices[channel.DeviceProjectionID]
		if !ok || channel.SourceChannelID == 0 {
			return nil, nil, fmt.Errorf("%w: channel device", ErrInvalidProjection)
		}
		if err := validatePublishedID(device.PublishedDeviceID); err != nil {
			return nil, nil, err
		}
		if err := validatePublishedID(channel.PublishedChannelID); err != nil {
			return nil, nil, err
		}
		if _, exists := sourceChannels[channel.SourceChannelID]; exists {
			return nil, nil, fmt.Errorf("%w: duplicate source channel", ErrInvalidProjection)
		}
		usedDevices[device.ID] = struct{}{}
		sourceChannels[channel.SourceChannelID] = struct{}{}
		channels = append(channels, channel)
	}
	sort.Slice(channels, func(i, j int) bool { return channels[i].PublishedChannelID < channels[j].PublishedChannelID })
	return channels, usedDevices, nil
}

func deviceCatalogItem(device model.GbCascadeDeviceProjection, platformID string, publishPlatform bool, facts SourceFacts) CatalogItem {
	parentID := ""
	if publishPlatform {
		parentID = platformID
	}
	return CatalogItem{
		ID:           device.PublishedDeviceID,
		ParentID:     parentID,
		Kind:         CatalogItemDevice,
		Name:         device.Name,
		Manufacturer: device.Manufacturer,
		Model:        device.Model,
		Owner:        device.Owner,
		CivilCode:    device.CivilCode,
		Address:      device.Address,
		Parental:     device.Parental,
		Secrecy:      device.Secrecy,
		Status:       deviceStatus(device, facts),
	}
}

func deviceParentID(channel model.GbCascadeChannelProjection, devices map[uint64]model.GbCascadeDeviceProjection) string {
	return devices[channel.DeviceProjectionID].PublishedDeviceID
}

func deviceStatus(device model.GbCascadeDeviceProjection, facts SourceFacts) ResourceStatus {
	if facts.DeviceOnline[device.SourceDeviceID] {
		return ResourceStatusOnline
	}
	return ResourceStatusOffline
}

func channelStatus(device model.GbCascadeDeviceProjection, channel model.GbCascadeChannelProjection, facts SourceFacts) ResourceStatus {
	if facts.DeviceOnline[device.SourceDeviceID] && facts.ChannelOnline[channel.SourceChannelID] {
		return ResourceStatusOnline
	}
	return ResourceStatusOffline
}

func addPublishedID(ids map[string]struct{}, id string) error {
	if err := validatePublishedID(id); err != nil {
		return err
	}
	if _, exists := ids[id]; exists {
		return fmt.Errorf("%w: duplicate %q", ErrInvalidPublishedID, id)
	}
	ids[id] = struct{}{}
	return nil
}

func validatePublishedID(id string) error {
	if len(id) != 20 {
		return fmt.Errorf("%w: %q", ErrInvalidPublishedID, id)
	}
	for _, char := range id {
		if char < '0' || char > '9' {
			return fmt.Errorf("%w: %q", ErrInvalidPublishedID, id)
		}
	}
	return nil
}
