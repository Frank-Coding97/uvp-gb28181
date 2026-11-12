package directory

import (
	"testing"
)

func sortDeviceNode(deviceID string, online bool) DirectoryNodeVO {
	return DirectoryNodeVO{
		Key:      "device:" + deviceID,
		Name:     deviceID,
		Type:     "device",
		ReadOnly: true,
		Device:   &DirectoryDeviceVO{DeviceID: deviceID, Name: deviceID, Online: online},
	}
}

func sortDirNode(key string, children ...DirectoryNodeVO) DirectoryNodeVO {
	return DirectoryNodeVO{Key: key, Name: key, Type: "area", ReadOnly: true, Children: children}
}

func nodeKeys(nodes []DirectoryNodeVO) []string {
	keys := make([]string, 0, len(nodes))
	for _, node := range nodes {
		keys = append(keys, node.Key)
	}
	return keys
}

func assertKeys(t *testing.T, got []DirectoryNodeVO, want ...string) {
	t.Helper()
	actual := nodeKeys(got)
	if len(actual) != len(want) {
		t.Fatalf("节点数不对：got %v, want %v", actual, want)
	}
	for i := range want {
		if actual[i] != want[i] {
			t.Fatalf("顺序不对：got %v, want %v", actual, want)
		}
	}
}

func TestSortOnlineDevicesFirstPutsOnlineFirstAndKeepsOriginalOrder(t *testing.T) {
	tree := []DirectoryNodeVO{sortDirNode("root",
		sortDeviceNode("a", false),
		sortDeviceNode("b", true),
		sortDeviceNode("c", false),
		sortDeviceNode("d", true),
	)}
	SortOnlineDevicesFirst(tree)
	assertKeys(t, tree[0].Children, "device:b", "device:d", "device:a", "device:c")
}

func TestSortOnlineDevicesFirstKeepsDirectorySlots(t *testing.T) {
	tree := []DirectoryNodeVO{sortDirNode("root",
		sortDirNode("area-x"),
		sortDeviceNode("off", false),
		sortDirNode("area-y"),
		sortDeviceNode("on", true),
	)}
	SortOnlineDevicesFirst(tree)
	// 设备只在自己的槽位之间换位，目录节点的位置一个都不许动。
	assertKeys(t, tree[0].Children, "area-x", "device:on", "area-y", "device:off")
}

func TestSortOnlineDevicesFirstRecursesIntoSubDirectories(t *testing.T) {
	tree := []DirectoryNodeVO{sortDirNode("province",
		sortDirNode("city-a", sortDeviceNode("a-off", false), sortDeviceNode("a-on", true)),
		sortDirNode("city-b", sortDeviceNode("b-off", false), sortDeviceNode("b-on", true)),
	)}
	SortOnlineDevicesFirst(tree)
	assertKeys(t, tree[0].Children[0].Children, "device:a-on", "device:a-off")
	assertKeys(t, tree[0].Children[1].Children, "device:b-on", "device:b-off")
}

func TestSortOnlineDevicesFirstLeavesSingleOrNoDeviceAlone(t *testing.T) {
	tree := []DirectoryNodeVO{
		sortDirNode("one", sortDeviceNode("only", false)),
		sortDirNode("empty"),
	}
	SortOnlineDevicesFirst(tree)
	assertKeys(t, tree, "one", "empty")
	assertKeys(t, tree[0].Children, "device:only")
}
