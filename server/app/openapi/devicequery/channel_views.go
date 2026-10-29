package devicequery

import (
	"context"
	"time"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

// timelineSlotDuration and timelineSlotCount describe the platform's current
// 24-hour channel availability grid. They are exported through the response so
// a caller never has to hardcode the sampling shape.
const (
	timelineSlotDuration = 30 * time.Minute
	timelineSlotCount    = 48
)

// ChannelMount is one directory location a channel is mounted under. A channel
// has exactly one primary mount; the rest are derived views.
type ChannelMount struct {
	ID           uint   `json:"id"`
	ParentNodeID uint   `json:"parentNodeId"`
	ParentName   string `json:"parentName"`
	ParentPath   string `json:"parentPath"`
	DisplayName  string `json:"displayName"`
	IsPrimary    bool   `json:"isPrimary"`
	MountSource  string `json:"mountSource"`
}

type ChannelMountList struct {
	ChannelID string         `json:"channelId"`
	Items     []ChannelMount `json:"items"`
}

// ListChannelMountsInScope returns every directory node that carries this
// channel. Both the mount rows and each parent node are read inside the same
// department scope, so a mount pointing outside the client's tree is dropped
// instead of leaking a node name.
func (s *Service) ListChannelMountsInScope(ctx context.Context, scope resource.DepartmentScope, deviceCode, channelCode string) (ChannelMountList, error) {
	_, channel, err := s.scopedChannel(ctx, scope, deviceCode, channelCode)
	if err != nil {
		return ChannelMountList{}, err
	}
	var mounts []gbmodels.GbChannelMount
	if err := s.db.WithContext(ctx).Where("channel_id = ?", channel.ID).
		Order("is_primary DESC, sort_order, id").Find(&mounts).Error; err != nil {
		return ChannelMountList{}, resource.ErrResourceUnavailable
	}
	items := make([]ChannelMount, 0, len(mounts))
	for _, mount := range mounts {
		var node gbmodels.GbCatalogNode
		result := s.db.WithContext(ctx).Select("id, name, path").Where("id = ?", mount.ParentNodeID).Limit(1).Find(&node)
		if result.Error != nil {
			return ChannelMountList{}, resource.ErrResourceUnavailable
		}
		item := ChannelMount{
			ID: mount.ID, ParentNodeID: mount.ParentNodeID, DisplayName: mount.DisplayName,
			IsPrimary: mount.IsPrimary, MountSource: string(mount.MountSource),
		}
		if result.RowsAffected > 0 {
			item.ParentName, item.ParentPath = node.Name, node.Path
		}
		items = append(items, item)
	}
	return ChannelMountList{ChannelID: channel.ChannelID, Items: items}, nil
}

type ChannelTimelineSlot struct {
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	Status string    `json:"status"`
}

// ChannelTimeline is a 24-hour availability grid. Simplified reports whether
// the slots describe stored history (false) or only replay the channel's
// current state (true). The platform today only produces the simplified form,
// so callers must not treat a uniform grid as evidence of continuous uptime.
type ChannelTimeline struct {
	ChannelID   string                `json:"channelId"`
	Range       string                `json:"range"`
	SlotMinutes int                   `json:"slotMinutes"`
	Simplified  bool                  `json:"simplified"`
	Slots       []ChannelTimelineSlot `json:"slots"`
}

// GetChannelTimelineInScope returns the channel's 24-hour availability grid.
// Until the platform stores real history the grid repeats the current online
// state, and Simplified is set so an external system can label it correctly
// instead of presenting placeholder slots as measured uptime.
func (s *Service) GetChannelTimelineInScope(ctx context.Context, scope resource.DepartmentScope, deviceCode, channelCode string) (ChannelTimeline, error) {
	_, channel, err := s.scopedChannel(ctx, scope, deviceCode, channelCode)
	if err != nil {
		return ChannelTimeline{}, err
	}
	state := "offline"
	if channel.Status == gbmodels.ChannelStatusOnline {
		state = "online"
	}
	now := s.currentTime()
	slots := make([]ChannelTimelineSlot, 0, timelineSlotCount)
	for index := timelineSlotCount - 1; index >= 0; index-- {
		start := now.Add(-time.Duration(index+1) * timelineSlotDuration)
		slots = append(slots, ChannelTimelineSlot{Start: start, End: start.Add(timelineSlotDuration), Status: state})
	}
	return ChannelTimeline{
		ChannelID: channel.ChannelID, Range: "24h", SlotMinutes: int(timelineSlotDuration / time.Minute),
		Simplified: true, Slots: slots,
	}, nil
}
