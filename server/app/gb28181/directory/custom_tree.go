package directory

import (
	"context"
	"fmt"
	"sort"

	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func BuildCustomTree(ctx context.Context, db *gorm.DB, ownerDeptID uint) ([]DirectoryNodeVO, error) {
	var groups []gbmodels.GbCustomGroup
	if err := db.WithContext(ctx).Where("owner_dept_id = ?", ownerDeptID).Order("depth, name, id").Find(&groups).Error; err != nil {
		return nil, err
	}
	var relations []gbmodels.GbCustomGroupDevice
	if err := db.WithContext(ctx).Joins("JOIN gb_custom_group g ON g.id = gb_custom_group_device.group_id AND g.owner_dept_id = ?", ownerDeptID).Find(&relations).Error; err != nil {
		return nil, err
	}
	children := map[uint][]gbmodels.GbCustomGroup{}
	for _, group := range groups {
		children[group.ParentID] = append(children[group.ParentID], group)
	}
	direct := map[uint]map[uint]struct{}{}
	for _, relation := range relations {
		if direct[relation.GroupID] == nil {
			direct[relation.GroupID] = map[uint]struct{}{}
		}
		direct[relation.GroupID][relation.DeviceID] = struct{}{}
	}
	var build func(uint, int) ([]DirectoryNodeVO, map[uint]struct{})
	build = func(parentID uint, depth int) ([]DirectoryNodeVO, map[uint]struct{}) {
		rows := children[parentID]
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
		out := make([]DirectoryNodeVO, 0, len(rows))
		all := map[uint]struct{}{}
		for _, group := range rows {
			childNodes, descendants := build(group.ID, depth+1)
			members := map[uint]struct{}{}
			for id := range direct[group.ID] {
				members[id] = struct{}{}
			}
			for id := range descendants {
				members[id] = struct{}{}
			}
			for id := range members {
				all[id] = struct{}{}
			}
			out = append(out, DirectoryNodeVO{Key: fmt.Sprintf("custom:group:%d", group.ID), Name: group.Name, Type: "group", Count: len(members), Depth: depth, Children: childNodes})
		}
		return out, all
	}
	tree, _ := build(0, 0)
	var ungrouped int64
	if err := db.WithContext(ctx).Model(&gbmodels.GbDevice{}).Where("owner_dept_id = ?", ownerDeptID).
		Where("NOT EXISTS (?)", db.Model(&gbmodels.GbCustomGroupDevice{}).Select("1").Where("gb_custom_group_device.device_id = gb_device.id")).Count(&ungrouped).Error; err != nil {
		return nil, err
	}
	if ungrouped > 0 {
		tree = append(tree, DirectoryNodeVO{Key: "custom:ungrouped", Name: "未分组", Type: "ungrouped", ReadOnly: true, Count: int(ungrouped)})
	}
	return tree, nil
}
