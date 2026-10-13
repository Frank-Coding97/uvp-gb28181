package directory

import (
	"sort"
	"strconv"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	basemodels "uvplatform.com/uvp-gb28181/app/models"
)

// BuildDepartmentTree projects visible platform departments and their owned
// devices into the device-management directory. The input department slice
// must already be permission-scoped; missing parents are detached so an
// unauthorized ancestor is never exposed or able to hide an allowed child.
func BuildDepartmentTree(departments basemodels.SysDepartmentList, devices []gbmodels.GbDevice) []DirectoryNodeVO {
	if departments == nil {
		departments = basemodels.SysDepartmentList{}
	}
	visible := make(map[uint]struct{}, len(departments))
	for _, department := range departments {
		visible[department.ID] = struct{}{}
	}
	for _, department := range departments {
		if department.ParentID == nil {
			continue
		}
		if _, ok := visible[*department.ParentID]; !ok {
			department.ParentID = nil
		}
	}
	departmentTree := departments.BuildTree()

	devicesByDepartment := make(map[uint][]gbmodels.GbDevice, len(departments))
	var unassigned []gbmodels.GbDevice
	for _, device := range devices {
		if device.OwnerDeptID == 0 {
			unassigned = append(unassigned, device)
			continue
		}
		if _, ok := visible[device.OwnerDeptID]; ok {
			devicesByDepartment[device.OwnerDeptID] = append(devicesByDepartment[device.OwnerDeptID], device)
		} else {
			// A device can outlive a deleted or missing sys_department row. Keep
			// that visible resource discoverable instead of silently dropping it.
			unassigned = append(unassigned, device)
		}
	}
	byID := func(items []gbmodels.GbDevice) {
		sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	}
	for id := range devicesByDepartment {
		byID(devicesByDepartment[id])
	}
	byID(unassigned)

	var build func(basemodels.SysDepartmentList, int) ([]DirectoryNodeVO, int, int)
	build = func(items basemodels.SysDepartmentList, depth int) ([]DirectoryNodeVO, int, int) {
		result := make([]DirectoryNodeVO, 0, len(items))
		total, onlineTotal := 0, 0
		for _, department := range items {
			children, childCount, childOnline := build(department.Children, depth+1)
			departmentDevices := devicesByDepartment[department.ID]
			count, onlineCount := childCount, childOnline
			for _, device := range departmentDevices {
				online := device.Status == gbmodels.DeviceStatusOnline
				if online {
					onlineCount++
				}
				count++
				children = append(children, deviceDirectoryNode(device, directoryDeviceKey("department", department.ID, device.ID), depth+1))
			}
			result = append(result, DirectoryNodeVO{
				Key:         "department:dept:" + departmentIDString(department.ID),
				Name:        department.Name,
				Type:        "department",
				ReadOnly:    true,
				Count:       count,
				OnlineCount: onlineCount,
				Depth:       depth,
				Children:    children,
			})
			total += count
			onlineTotal += onlineCount
		}
		return result, total, onlineTotal
	}

	tree, _, _ := build(departmentTree, 0)
	if len(unassigned) > 0 {
		children := make([]DirectoryNodeVO, 0, len(unassigned))
		onlineCount := 0
		for _, device := range unassigned {
			if device.Status == gbmodels.DeviceStatusOnline {
				onlineCount++
			}
			children = append(children, deviceDirectoryNode(device, directoryDeviceKey("department-unassigned", 0, device.ID), 1))
		}
		tree = append(tree, DirectoryNodeVO{
			Key:         "department:unassigned",
			Name:        "未分配部门",
			Type:        "unassigned",
			ReadOnly:    true,
			Count:       len(unassigned),
			OnlineCount: onlineCount,
			Depth:       0,
			Children:    children,
		})
	}
	return tree
}

// CollectDepartmentSubtreeIDs returns only departments present in the supplied
// visible set. Callers must pass a permission-scoped list, never the full tree.
func CollectDepartmentSubtreeIDs(departments basemodels.SysDepartmentList, rootID uint) ([]uint, bool) {
	byID := make(map[uint]*basemodels.SysDepartment, len(departments))
	children := make(map[uint][]uint, len(departments))
	for _, department := range departments {
		byID[department.ID] = department
		if department.ParentID != nil {
			children[*department.ParentID] = append(children[*department.ParentID], department.ID)
		}
	}
	if byID[rootID] == nil {
		return nil, false
	}
	ids := []uint{rootID}
	seen := map[uint]struct{}{rootID: {}}
	for i := 0; i < len(ids); i++ {
		for _, childID := range children[ids[i]] {
			if _, ok := seen[childID]; ok {
				continue
			}
			seen[childID] = struct{}{}
			ids = append(ids, childID)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, true
}

func departmentIDString(value uint) string {
	return strconv.FormatUint(uint64(value), 10)
}
