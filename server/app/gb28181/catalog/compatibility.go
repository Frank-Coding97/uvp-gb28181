package catalog

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// Parents are ingested before their children even when a device reverses the list.
func orderCatalogItems(items []CatalogItem) ([]CatalogItem, map[string]bool) {
	byCode := make(map[string]int, len(items))
	parents := make(map[string]bool)
	for i, it := range items {
		byCode[it.DeviceID] = i
		for _, parent := range SplitParentIDs(it.ParentID) {
			if parent != it.DeviceID {
				parents[parent] = true
			}
		}
	}
	ordered := make([]CatalogItem, 0, len(items))
	visited := make(map[int]bool)
	var visit func(int)
	visit = func(i int) {
		if visited[i] {
			return
		}
		visited[i] = true
		it := items[i]
		for _, parent := range append(SplitParentIDs(it.ParentID), SplitParentIDs(it.BusinessGroupID)...) {
			if j, ok := byCode[parent]; ok {
				visit(j)
			}
		}
		ordered = append(ordered, it)
	}
	for i := range items {
		visit(i)
	}
	return ordered, parents
}

// Scope identity recovery to resources reported by this device, not merely a code
// shared by other devices in the department.
func sourceCatalogNodes(tx *gorm.DB, sender Sender, code string) *gorm.DB {
	return tx.Model(&gbmodels.GbCatalogNode{}).
		Where("owner_dept_id = ? AND code = ? AND source = ?", sender.OwnerDeptID, code, gbmodels.NodeSourceCatalog).
		Where(`(channel_id IN (SELECT id FROM gb_channel WHERE owner_dept_id = ? AND device_id = ? AND deleted_at IS NULL)
   OR device_id IN (SELECT id FROM gb_device WHERE owner_dept_id = ? AND device_id = ? AND deleted_at IS NULL)
   OR id IN (SELECT catalog_node_id FROM gb_anomaly_record WHERE owner_dept_id = ? AND source_device_id IN
    (SELECT id FROM gb_device WHERE owner_dept_id = ? AND device_id = ? AND deleted_at IS NULL)))`,
			sender.OwnerDeptID, sender.SourceDeviceID, sender.OwnerDeptID, sender.SourceDeviceID,
			sender.OwnerDeptID, sender.OwnerDeptID, sender.SourceDeviceID)
}

func classifyCatalogItem(tx *gorm.DB, sender Sender, it CatalogItem, hasChildren, preserveIdentity bool) (Classification, error) {
	cls := Classify(it.DeviceID)
	// Explicit non-video types retain their meaning, including alarm resources.
	if !cls.Anomaly && cls.NodeType != gbmodels.NodeTypeChannel {
		return cls, nil
	}
	if it.Parental == 1 || (hasChildren && !it.ParentalPresent) {
		cls.NodeType = gbmodels.NodeTypeVirtualOrg
		if it.DeviceID == sender.SourceDeviceID || !cls.Anomaly {
			cls.NodeType = gbmodels.NodeTypeDevice
		}
		return cls, nil
	}
	if !it.ParentalPresent {
		var existing gbmodels.GbCatalogNode
		q := sourceCatalogNodes(tx, sender, it.DeviceID)
		// A sparse UPDATE does not declare a new resource identity. A root remains
		// a device until an explicit leaf declaration is received.
		sparse := preserveIdentity && it.Name == "" && it.ParentID == "" && it.BusinessGroupID == "" && it.Manufacturer == "" && it.Model == ""
		if sparse || it.DeviceID == sender.SourceDeviceID {
			if !sparse {
				q = q.Where("node_type = ?", gbmodels.NodeTypeDevice)
			}
			result := q.Order("id").Limit(1).Find(&existing)
			if result.Error != nil {
				return cls, result.Error
			}
			if result.RowsAffected > 0 {
				cls.NodeType = existing.NodeType
				return cls, nil
			}
		}
	}
	if cls.Anomaly {
		cls.NodeType = gbmodels.NodeTypeChannel
	}
	return cls, nil
}

// Reclassify a catalog-owned node in place so its children and historical audit
// records retain their identity. Active or manually mounted channels need to be
// stopped/unmounted before their video identity can be removed.
func reconcileCatalogIdentity(tx *gorm.DB, sender Sender, it CatalogItem, cls Classification, parent *gbmodels.GbCatalogNode) error {
	var nodes []gbmodels.GbCatalogNode
	if err := sourceCatalogNodes(tx, sender, it.DeviceID).Where("node_type <> ?", cls.NodeType).Find(&nodes).Error; err != nil {
		return err
	}
	for _, node := range nodes {
		if node.ChannelID != nil {
			var ch gbmodels.GbChannel
			result := tx.Where("id = ? AND owner_dept_id = ? AND device_id = ?", *node.ChannelID, sender.OwnerDeptID, sender.SourceDeviceID).Limit(1).Find(&ch)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				continue
			}
			var manual int64
			if err := tx.Model(&gbmodels.GbChannelMount{}).Where("owner_dept_id = ? AND channel_id = ? AND mount_source = ?", sender.OwnerDeptID, ch.ID, gbmodels.MountSourceManual).Count(&manual).Error; err != nil {
				return err
			}
			if ch.StreamID != "" || manual > 0 {
				return fmt.Errorf("catalog: stop or unmount channel %s before correcting its resource type", it.DeviceID)
			}
			if err := tx.Where("owner_dept_id = ? AND channel_id = ? AND mount_source = ?", sender.OwnerDeptID, ch.ID, gbmodels.MountSourceCatalog).Delete(&gbmodels.GbChannelMount{}).Error; err != nil {
				return err
			}
			if err := tx.Delete(&ch).Error; err != nil {
				return err
			}
		}
		parentPath := "/"
		var parentID *uint
		if parent != nil {
			parentPath = parent.Path
			parentID = &parent.ID
		}
		var target gbmodels.GbCatalogNode
		q := sourceCatalogNodes(tx, sender, it.DeviceID).Where("node_type = ? AND id <> ?", cls.NodeType, node.ID)
		if parentID == nil {
			q = q.Where("parent_id IS NULL")
		} else {
			q = q.Where("parent_id = ?", *parentID)
		}
		found := q.Order("id").Limit(1).Find(&target)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected > 0 && node.ChannelID == nil {
			var mounts []gbmodels.GbChannelMount
			if err := tx.Where("owner_dept_id = ? AND parent_node_id = ?", sender.OwnerDeptID, node.ID).Find(&mounts).Error; err != nil {
				return err
			}
			for _, mount := range mounts {
				var existing gbmodels.GbChannelMount
				res := tx.Where("channel_id = ? AND parent_node_id = ?", mount.ChannelID, target.ID).Limit(1).Find(&existing)
				if res.Error != nil {
					return res.Error
				}
				if res.RowsAffected == 0 {
					if err := tx.Model(&mount).Update("parent_node_id", target.ID).Error; err != nil {
						return err
					}
				} else {
					// Keep manual mounting and primary selection when collapsing duplicates.
					if mount.MountSource == gbmodels.MountSourceManual {
						return fmt.Errorf("catalog: unmount duplicate directory %s before correcting its resource type", it.DeviceID)
					}
					if mount.IsPrimary {
						if err := tx.Model(&existing).Update("is_primary", true).Error; err != nil {
							return err
						}
					}
					if err := tx.Delete(&mount).Error; err != nil {
						return err
					}
				}
			}
			if err := tx.Model(&gbmodels.GbCatalogNode{}).Where("owner_dept_id = ? AND parent_id = ?", sender.OwnerDeptID, node.ID).Update("parent_id", target.ID).Error; err != nil {
				return err
			}
		}
		path := BuildPath(parentPath, node.ID)
		if found.RowsAffected > 0 && node.ChannelID == nil {
			path = target.Path
		}
		// Empty placeholder paths must not match the whole department.
		if node.Path != "/" && node.Path != "" && node.Path != path {
			var children []gbmodels.GbCatalogNode
			if err := tx.Where("owner_dept_id = ? AND path LIKE ? AND id <> ?", sender.OwnerDeptID, node.Path+"%", node.ID).Find(&children).Error; err != nil {
				return err
			}
			for _, child := range children {
				childPath := path + strings.TrimPrefix(child.Path, node.Path)
				if err := tx.Model(&child).Updates(map[string]any{"path": childPath, "depth": DepthFromPath(childPath) - 1}).Error; err != nil {
					return err
				}
			}
		}
		if found.RowsAffected > 0 && node.ChannelID == nil {
			if err := tx.Delete(&node).Error; err != nil {
				return err
			}
			continue
		}
		if err := tx.Model(&node).Updates(map[string]any{"node_type": cls.NodeType, "channel_id": nil, "device_id": nil, "parent_id": parentID, "path": path, "depth": DepthFromPath(parentPath)}).Error; err != nil {
			return err
		}
	}
	return nil
}
