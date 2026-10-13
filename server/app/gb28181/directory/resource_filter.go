package directory

import "fmt"

// FilterResources prunes resource leaves while preserving their ancestor paths.
// Counts are distinct resources, since one device may appear under several organizations.
func FilterResources(tree []DirectoryNodeVO, devices map[uint]bool, channels map[string]uint) []DirectoryNodeVO {
	var walk func([]DirectoryNodeVO) ([]DirectoryNodeVO, map[string]bool)
	walk = func(items []DirectoryNodeVO) ([]DirectoryNodeVO, map[string]bool) {
		out := make([]DirectoryNodeVO, 0)
		resources := map[string]bool{}
		for _, item := range items {
			var own map[string]bool
			if item.Device != nil {
				if !devices[item.Device.ID] {
					continue
				}
				own = map[string]bool{fmt.Sprintf("device:%d", item.Device.ID): item.Device.Online}
			} else if item.Type == "channel" {
				id, ok := channels[item.Key]
				if !ok {
					continue
				}
				own = map[string]bool{fmt.Sprintf("channel:%d", id): item.OnlineCount > 0}
			} else {
				item.Children, own = walk(item.Children)
				if len(own) == 0 {
					continue
				}
				item.Count = len(own)
				item.OnlineCount = 0
				for _, online := range own {
					if online {
						item.OnlineCount++
					}
				}
			}
			for key, online := range own {
				resources[key] = online
			}
			out = append(out, item)
		}
		return out, resources
	}
	result, _ := walk(tree)
	return result
}
