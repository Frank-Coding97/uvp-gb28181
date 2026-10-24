// Package catalog turns one persisted cascade projection into a read-only,
// platform-scoped Catalog domain snapshot. XML and SIP response handling belong
// to the responder layer, not this package.
package catalog

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
)

var (
	ErrInvalidProjection  = errors.New("cascade catalog: invalid projection")
	ErrInvalidPublishedID = errors.New("cascade catalog: invalid published id")
)

type CatalogItemKind string

const (
	CatalogItemPlatform            CatalogItemKind = "platform"
	CatalogItemBusinessGroup       CatalogItemKind = "business_group"
	CatalogItemVirtualOrganization CatalogItemKind = "virtual_organization"
	CatalogItemDevice              CatalogItemKind = "device"
	CatalogItemChannel             CatalogItemKind = "channel"
)

type ResourceStatus string

const (
	ResourceStatusOnline    ResourceStatus = "ON"
	ResourceStatusOffline   ResourceStatus = "OFF"
	ResourceStatusVideoLost ResourceStatus = "VLOST"
	ResourceStatusDefect    ResourceStatus = "DEFECT"
)

// SourceFacts contains the status and channel metadata read with the projection.
// Missing status entries are offline so a snapshot never invents an online resource.
type SourceFacts struct {
	DeviceOnline       map[uint64]bool
	ChannelOnline      map[uint64]bool
	ChannelStatusEvent map[uint64]ResourceStatus
	ChannelMetadata    map[uint64]ChannelMetadata
}

type ChannelMetadata struct {
	Manufacturer string
	Model        string
	Owner        string
	CivilCode    string
}

// CatalogItem is the metadata whitelist available to an outbound Catalog
// responder. It intentionally excludes source database IDs, source network
// endpoints, credentials, and arbitrary source fields.
type CatalogItem struct {
	ID              string
	ParentID        string
	Kind            CatalogItemKind
	Name            string
	Manufacturer    string
	Model           string
	Owner           string
	CivilCode       string
	Address         string
	BusinessGroupID string
	Parental        int
	Secrecy         int
	Status          ResourceStatus
	StatusEvent     ResourceStatus
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

	publishedIDs    map[string]struct{}
	channels        map[string]PublishedChannelTarget
	channelMetadata map[uint64]CatalogItem
	// deviceChannels is an authorization-only lookup. A publication may omit
	// physical device nodes while still needing to answer a scoped Catalog query
	// addressed to one of those nodes; the map never contains resources outside
	// this platform snapshot.
	deviceChannels map[string][]CatalogItem
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

	groupNodes, bindings, rootCode, err := businessDirectory(projection, devices, channels)
	if err != nil {
		return Snapshot{}, err
	}
	// Old platforms may have the toggle set before a tree is configured. Keep
	// their legacy flat catalog until there is a complete tree to publish.
	publishSystem := projection.Platform.PublishPlatform || (projection.Platform.PublishGroup && len(groupNodes) > 0)
	snapshot := Snapshot{
		PlatformID:      platformID,
		Revision:        projection.Revision,
		publishedIDs:    make(map[string]struct{}, len(groupNodes)+len(devices)+len(channels)+1),
		channels:        make(map[string]PublishedChannelTarget, len(channels)),
		channelMetadata: make(map[uint64]CatalogItem, len(channels)),
		deviceChannels:  make(map[string][]CatalogItem, len(devices)),
	}
	if publishSystem {
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

	if err := appendBusinessDirectoryItems(&snapshot, groupNodes, rootCode, projection.Platform.LocalDeviceID); err != nil {
		return Snapshot{}, err
	}
	if err := appendDeviceItems(&snapshot, devices, projection.Platform, facts, projection.Platform.PublishPlatform || (projection.Platform.PublishGroup && len(groupNodes) > 0)); err != nil {
		return Snapshot{}, err
	}
	if err := appendChannelItems(&snapshot, platformID, devices, channels, facts, bindings); err != nil {
		return Snapshot{}, err
	}
	for _, device := range devices {
		snapshot.deviceChannels[device.PublishedDeviceID] = nil
	}
	for _, item := range snapshot.Items {
		if item.Kind != CatalogItemChannel {
			continue
		}
		parent := item.ParentID
		if slash := strings.IndexByte(parent, '/'); slash >= 0 {
			parent = parent[:slash]
		}
		if _, ok := snapshot.deviceChannels[parent]; ok {
			snapshot.deviceChannels[parent] = append(snapshot.deviceChannels[parent], item)
		}
	}
	snapshot.SumNum = len(snapshot.Items)
	return snapshot, nil
}

// appendDeviceItems 把设备投影转为目录项并按发布 ID 注册
func appendDeviceItems(snapshot *Snapshot, devices map[uint64]model.GbCascadeDeviceProjection, platform model.GbCascadePlatform, facts SourceFacts, publishSystem bool) error {
	for _, device := range sortedDevices(devices) {
		if err := addPublishedID(snapshot.publishedIDs, device.PublishedDeviceID); err != nil {
			return err
		}
		snapshot.Items = append(snapshot.Items, deviceCatalogItem(device, platform.LocalDeviceID, publishSystem, facts))
	}
	return nil
}

// appendChannelItems 把通道投影转为目录项与播放目标映射
func appendChannelItems(snapshot *Snapshot, platformID uint64, devices map[uint64]model.GbCascadeDeviceProjection, channels []model.GbCascadeChannelProjection, facts SourceFacts, bindings map[uint64]string) error {
	for _, channel := range channels {
		if err := addPublishedID(snapshot.publishedIDs, channel.PublishedChannelID); err != nil {
			return err
		}
	}
	for _, channel := range channels {
		parentID := deviceParentID(channel, devices)
		if nodeCode, assigned := bindings[channel.SourceChannelID]; assigned {
			parentID += "/" + nodeCode
		} else if channel.ParentOverride != "" {
			if _, ok := snapshot.publishedIDs[channel.ParentOverride]; ok {
				parentID = channel.ParentOverride
			}
		}
		device := devices[channel.DeviceProjectionID]
		metadata := deviceCatalogItem(device, "", false, facts)
		if source, ok := facts.ChannelMetadata[channel.SourceChannelID]; ok {
			metadata.Manufacturer, metadata.Model, metadata.Owner = source.Manufacturer, source.Model, source.Owner
			metadata.CivilCode = source.CivilCode
		}
		snapshot.channelMetadata[channel.SourceChannelID] = metadata
		item := CatalogItem{
			ID:          channel.PublishedChannelID,
			ParentID:    parentID,
			Kind:        CatalogItemChannel,
			Name:        channel.Name,
			Status:      channelStatus(device, channel, facts),
			StatusEvent: channelStatusEvent(device, channel, facts),
		}
		snapshot.Items = append(snapshot.Items, item)
		snapshot.channels[channel.PublishedChannelID] = PublishedChannelTarget{
			PlatformID:      platformID,
			SourceDeviceID:  device.SourceDeviceID,
			SourceChannelID: channel.SourceChannelID,
			PTZAllowed:      channel.PTZAllowed,
		}
	}
	return nil
}

func businessDirectory(projection repository.ProjectionSnapshot, devices map[uint64]model.GbCascadeDeviceProjection, channels []model.GbCascadeChannelProjection) ([]model.GbCascadeBusinessNode, map[uint64]string, string, error) {
	if !projection.Platform.PublishGroup || len(projection.Nodes) == 0 {
		return nil, map[uint64]string{}, "", nil
	}
	const invalid = "business catalog tree"
	if len(projection.Platform.LocalDeviceID) < 13 || projection.Platform.LocalDeviceID[10:13] != "200" {
		return nil, nil, "", fmt.Errorf("%w: %s requires a system type 200 local identity", ErrInvalidProjection, invalid)
	}
	nodes := append([]model.GbCascadeBusinessNode(nil), projection.Nodes...)
	byCode := make(map[string]model.GbCascadeBusinessNode, len(nodes))
	rootCode := ""
	for _, node := range nodes {
		if node.PlatformID != projection.Platform.ID || validatePublishedID(node.Code) != nil {
			return nil, nil, "", fmt.Errorf("%w: %s node identity", ErrInvalidProjection, invalid)
		}
		if node.Code == projection.Platform.LocalDeviceID || node.Code == projection.Platform.UpstreamServerID {
			return nil, nil, "", fmt.Errorf("%w: %s node conflicts with platform identity", ErrInvalidProjection, invalid)
		}
		if _, exists := byCode[node.Code]; exists {
			return nil, nil, "", fmt.Errorf("%w: %s duplicate node", ErrInvalidProjection, invalid)
		}
		switch node.Kind {
		case "group":
			if len(node.Code) < 13 || node.Code[10:13] != "215" || node.ParentCode != "" || rootCode != "" {
				return nil, nil, "", fmt.Errorf("%w: %s root", ErrInvalidProjection, invalid)
			}
			rootCode = node.Code
		case "organization":
			if len(node.Code) < 13 || node.Code[10:13] != "216" || node.ParentCode == "" {
				return nil, nil, "", fmt.Errorf("%w: %s organization", ErrInvalidProjection, invalid)
			}
		default:
			return nil, nil, "", fmt.Errorf("%w: %s kind", ErrInvalidProjection, invalid)
		}
		byCode[node.Code] = node
	}
	if rootCode == "" {
		return nil, nil, "", fmt.Errorf("%w: %s missing root", ErrInvalidProjection, invalid)
	}
	for _, node := range nodes {
		if node.Kind == "organization" {
			parent, ok := byCode[node.ParentCode]
			if !ok || (parent.Kind != "group" && parent.Kind != "organization") {
				return nil, nil, "", fmt.Errorf("%w: %s parent", ErrInvalidProjection, invalid)
			}
		}
	}
	if err := validateBusinessDirectoryReachability(nodes, rootCode); err != nil {
		return nil, nil, "", err
	}

	published := make(map[string]struct{}, len(nodes)+len(devices)+len(channels)+1)
	if err := addPublishedID(published, projection.Platform.LocalDeviceID); err != nil {
		return nil, nil, "", err
	}
	for _, device := range devices {
		published[device.PublishedDeviceID] = struct{}{}
	}
	for _, channel := range channels {
		published[channel.PublishedChannelID] = struct{}{}
	}
	for _, node := range nodes {
		if err := addPublishedID(published, node.Code); err != nil {
			return nil, nil, "", err
		}
	}

	activeChannels := make(map[uint64]struct{}, len(channels))
	for _, channel := range channels {
		activeChannels[channel.SourceChannelID] = struct{}{}
	}
	bindings := make(map[uint64]string, len(projection.Bindings))
	for _, binding := range projection.Bindings {
		if binding.PlatformID != projection.Platform.ID {
			return nil, nil, "", fmt.Errorf("%w: business binding platform", ErrInvalidProjection)
		}
		if _, active := activeChannels[binding.SourceChannelID]; !active {
			continue
		}
		node, ok := byCode[binding.NodeCode]
		if !ok || node.Kind != "organization" {
			return nil, nil, "", fmt.Errorf("%w: business binding node", ErrInvalidProjection)
		}
		if _, duplicate := bindings[binding.SourceChannelID]; duplicate {
			return nil, nil, "", fmt.Errorf("%w: duplicate business binding", ErrInvalidProjection)
		}
		bindings[binding.SourceChannelID] = binding.NodeCode
	}

	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Kind != nodes[j].Kind {
			return nodes[i].Kind == "group"
		}
		if nodes[i].Sort != nodes[j].Sort {
			return nodes[i].Sort < nodes[j].Sort
		}
		return nodes[i].Code < nodes[j].Code
	})
	return nodes, bindings, rootCode, nil
}

func validateBusinessDirectoryReachability(nodes []model.GbCascadeBusinessNode, rootCode string) error {
	reachable := map[string]bool{rootCode: true}
	remaining := len(nodes) - 1
	for changed := true; changed && remaining > 0; {
		changed = false
		for _, node := range nodes {
			if node.Kind == "organization" && !reachable[node.Code] && reachable[node.ParentCode] {
				reachable[node.Code] = true
				remaining--
				changed = true
			}
		}
	}
	if remaining != 0 {
		return fmt.Errorf("%w: business catalog tree has orphan or cycle", ErrInvalidProjection)
	}
	return nil
}

func appendBusinessDirectoryItems(snapshot *Snapshot, nodes []model.GbCascadeBusinessNode, rootCode, localDeviceID string) error {
	if len(nodes) == 0 {
		return nil
	}
	if err := addPublishedID(snapshot.publishedIDs, rootCode); err != nil {
		return err
	}
	nodesByCode := make(map[string]model.GbCascadeBusinessNode, len(nodes))
	for _, node := range nodes {
		if node.Code != rootCode {
			if err := addPublishedID(snapshot.publishedIDs, node.Code); err != nil {
				return err
			}
		}
		nodesByCode[node.Code] = node
	}
	root := nodesByCode[rootCode]
	snapshot.Items = append(snapshot.Items, CatalogItem{ID: root.Code, ParentID: localDeviceID, Kind: CatalogItemBusinessGroup, Name: root.Name, Parental: 1, Status: ResourceStatusOnline})
	for _, node := range nodes {
		if node.Kind != "organization" {
			continue
		}
		item := CatalogItem{ID: node.Code, Kind: CatalogItemVirtualOrganization, Name: node.Name, BusinessGroupID: rootCode, Parental: 1, Status: ResourceStatusOnline}
		if parent := nodesByCode[node.ParentCode]; parent.Kind == "organization" {
			item.ParentID = parent.Code
		}
		snapshot.Items = append(snapshot.Items, item)
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

func channelStatusEvent(device model.GbCascadeDeviceProjection, channel model.GbCascadeChannelProjection, facts SourceFacts) ResourceStatus {
	if !facts.DeviceOnline[device.SourceDeviceID] {
		return ""
	}
	event := facts.ChannelStatusEvent[channel.SourceChannelID]
	if event == ResourceStatusVideoLost || event == ResourceStatusDefect {
		return event
	}
	return ""
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

// ChannelsOnly exposes selected video resources without publishing device grouping nodes.
// Source-device ownership and the reverse lookup remain available internally.
func (s Snapshot) ChannelsOnly() Snapshot {
	result := s
	result.Items = make([]CatalogItem, 0, len(s.channels))
	result.publishedIDs = make(map[string]struct{}, len(s.channels))
	result.deviceChannels = make(map[string][]CatalogItem, len(s.deviceChannels))
	for _, item := range s.Items {
		if item.Kind != CatalogItemChannel {
			continue
		}
		item.ParentID = ""
		item.Parental = 0
		result.Items = append(result.Items, item)
		result.publishedIDs[item.ID] = struct{}{}
	}
	for deviceID, items := range s.deviceChannels {
		for _, item := range items {
			item.ParentID = ""
			item.Parental = 0
			result.deviceChannels[deviceID] = append(result.deviceChannels[deviceID], item)
		}
	}
	result.SumNum = len(result.Items)
	return result
}
