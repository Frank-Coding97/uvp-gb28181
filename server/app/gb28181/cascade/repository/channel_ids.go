package repository

import (
	"fmt"
	"gorm.io/gorm"
	"regexp"
	"strings"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/model"
)

var publishedIDPattern = regexp.MustCompile(`^[0-9]{20}$`)

// Allocate inside the projection transaction. Inactive mappings remain reserved.
func allocatePublishedChannelIDs(tx *gorm.DB, platformID uint64, devices []DeviceProjectionInput, channels []ChannelProjectionInput) error {
	var existing []model.GbCascadeChannelProjection
	if err := tx.Unscoped().Where("platform_id = ?", platformID).Find(&existing).Error; err != nil {
		return err
	}
	used := map[string]uint64{}
	stable := map[uint64]string{}
	for _, d := range devices {
		used[d.PublishedDeviceID] = 0
	}
	for _, c := range existing {
		used[c.PublishedChannelID] = c.SourceChannelID
		if publishedIDPattern.MatchString(c.PublishedChannelID) && !strings.HasPrefix(c.PublishedChannelID, "99") {
			stable[c.SourceChannelID] = c.PublishedChannelID
		}
	}
	// Reserve all requested original IDs before allocation so replacements cannot steal them.
	for _, c := range channels {
		if _, exists := used[c.PublishedChannelID]; !exists {
			used[c.PublishedChannelID] = c.SourceChannelID
		}
	}
	for i := range channels {
		c := &channels[i]
		if id := stable[c.SourceChannelID]; id != "" {
			c.PublishedChannelID = id
			continue
		}
		id := c.PublishedChannelID
		if !publishedIDPattern.MatchString(id) || strings.HasPrefix(id, "99") {
			return fmt.Errorf("%w: source channel requires a valid original GB identifier", ErrInvalidProjection)
		}
		if owner, ok := used[id]; !ok || owner == c.SourceChannelID {
			used[id] = c.SourceChannelID
			continue
		}
		allocated := false
		for seq := 1; seq <= 999999; seq++ {
			candidate := fmt.Sprintf("%s%06d", id[:14], seq)
			if _, exists := used[candidate]; exists {
				continue
			}
			c.PublishedChannelID = candidate
			used[candidate] = c.SourceChannelID
			allocated = true
			break
		}
		if !allocated {
			return fmt.Errorf("%w: published channel sequence exhausted", ErrInvalidProjection)
		}
	}
	return nil
}
