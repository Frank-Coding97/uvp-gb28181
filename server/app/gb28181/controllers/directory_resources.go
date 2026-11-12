package controllers

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	gbdirectory "uvplatform.com/uvp-gb28181/app/gb28181/directory"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

func filterDirectoryResources(c *gin.Context, db *gorm.DB, tree []gbdirectory.DirectoryNodeVO) ([]gbdirectory.DirectoryNodeVO, error) {
	keyword := strings.TrimSpace(c.Query("q"))
	status := c.Query("status")
	if keyword == "" && status == "" {
		return tree, nil
	}
	devices := db.WithContext(c.Request.Context()).Model(&gbmodels.GbDevice{}).Scopes(ownerDeptScope(c))
	channels := db.WithContext(c.Request.Context()).Model(&gbmodels.GbChannel{}).Scopes(ownerDeptScope(c))
	if c.Query("assetKind") == "channel" {
		if keyword != "" {
			channels = channels.Where("name LIKE ? OR channel_id LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
		}
		if status == "online" {
			channels = channels.Where("status = ?", gbmodels.ChannelStatusOnline)
		}
		if status == "offline" {
			channels = channels.Where("status = ?", gbmodels.ChannelStatusOffline)
		}
		devices = devices.Where("device_id IN (?)", channels.Session(&gorm.Session{}).Select("device_id"))
	} else {
		if keyword != "" {
			devices = devices.Where("name LIKE ? OR device_id LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
		}
		if status == "online" {
			devices = devices.Where("status = ?", gbmodels.DeviceStatusOnline)
		}
		if status == "offline" {
			devices = devices.Where("status = ?", gbmodels.DeviceStatusOffline)
		}
		channels = channels.Where("device_id IN (?)", devices.Session(&gorm.Session{}).Select("device_id"))
	}
	var deviceIDs []uint
	if err := devices.Pluck("id", &deviceIDs).Error; err != nil {
		return nil, err
	}
	allowedDevices := map[uint]bool{}
	for _, id := range deviceIDs {
		allowedDevices[id] = true
	}
	allowedChannels := map[string]uint{}
	if c.Query("view") == "business" {
		var nodes []gbmodels.GbCatalogNode
		err := db.WithContext(c.Request.Context()).Model(&gbmodels.GbCatalogNode{}).Scopes(ownerDeptScope(c)).
			Select("id", "channel_id").Where("node_type = ? AND channel_id IN (?)", gbmodels.NodeTypeChannel, channels.Select("id")).Find(&nodes).Error
		if err != nil {
			return nil, err
		}
		for _, node := range nodes {
			if node.ChannelID != nil {
				allowedChannels[fmt.Sprintf("business:catalog:%d", node.ID)] = *node.ChannelID
			}
		}
	}
	return gbdirectory.FilterResources(tree, allowedDevices, allowedChannels), nil
}
