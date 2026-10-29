package directory

import "testing"

func TestFilterResourcesPreservesAncestorsAndDeduplicates(t *testing.T) {
	tree := []DirectoryNodeVO{{Key: "root", Children: []DirectoryNodeVO{
		{Key: "empty"},
		{Key: "a", Device: &DirectoryDeviceVO{ID: 1, Online: true}},
		{Key: "b", Device: &DirectoryDeviceVO{ID: 1, Online: true}},
		{Key: "c", Device: &DirectoryDeviceVO{ID: 2}},
	}}}
	result := FilterResources(tree, map[uint]bool{1: true}, nil)
	if len(result) != 1 || len(result[0].Children) != 2 || result[0].Count != 1 || result[0].OnlineCount != 1 {
		t.Fatalf("unexpected tree: %+v", result)
	}
	if len(FilterResources(tree, nil, nil)) != 0 {
		t.Fatal("no matching resources must yield empty tree")
	}
}
