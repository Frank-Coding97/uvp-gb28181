package directory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"sync"
)

// FlatNode separates hierarchy from node data so a leaf update does not resend its ancestors.
type FlatNode struct {
	Node     DirectoryNodeVO `json:"node"`
	Children []string        `json:"children"`
}
type TreeSync struct {
	Revision string            `json:"revision"`
	Reset    bool              `json:"reset"`
	List     []DirectoryNodeVO `json:"list,omitempty"`
	Roots    []string          `json:"roots"`
	Upserts  []FlatNode        `json:"upserts"`
	Removed  []string          `json:"removed"`
}
type treeSnapshot struct {
	nodes map[string]json.RawMessage
	bytes int
	order uint64
}
type SyncCache struct {
	mu      sync.Mutex
	entries map[string]treeSnapshot
	bytes   int
	order   uint64
}

func NewSyncCache() *SyncCache { return &SyncCache{entries: map[string]treeSnapshot{}} }

// Sync keeps only bounded comparison snapshots, not an authoritative resource cache.
// A missing baseline (restart, eviction, scope change) safely falls back to a full tree.
func (c *SyncCache) Sync(scope, since string, tree []DirectoryNodeVO) TreeSync {
	nodes := map[string]json.RawMessage{}
	roots := make([]string, 0, len(tree))
	size := 0
	var walk func([]DirectoryNodeVO)
	walk = func(items []DirectoryNodeVO) {
		for _, item := range items {
			childKeys := make([]string, 0, len(item.Children))
			for _, child := range item.Children {
				childKeys = append(childKeys, child.Key)
			}
			walk(item.Children)
			item.Children = nil
			encoded, _ := json.Marshal(FlatNode{Node: item, Children: childKeys})
			nodes[item.Key] = encoded
			size += len(encoded) + len(item.Key)
		}
	}
	for _, item := range tree {
		roots = append(roots, item.Key)
	}
	walk(tree)
	encoded, _ := json.Marshal(struct {
		Scope string
		Roots []string
		Nodes map[string]json.RawMessage
	}{scope, roots, nodes})
	digest := sha256.Sum256(encoded)
	revision := hex.EncodeToString(digest[:])
	result := TreeSync{Revision: revision, Roots: roots, Upserts: []FlatNode{}, Removed: []string{}}
	c.mu.Lock()
	defer c.mu.Unlock()
	old, exists := c.entries[scope+":"+since]
	if !exists || since == "" {
		result.Reset = true
		result.List = tree
	} else {
		keys := make([]string, 0, len(nodes))
		for key := range nodes {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if string(old.nodes[key]) == string(nodes[key]) {
				continue
			}
			var item FlatNode
			_ = json.Unmarshal(nodes[key], &item)
			result.Upserts = append(result.Upserts, item)
		}
		for key := range old.nodes {
			if _, ok := nodes[key]; !ok {
				result.Removed = append(result.Removed, key)
			}
		}
		sort.Strings(result.Removed)
	}
	const maxBytes = 32 * 1024 * 1024
	key := scope + ":" + revision
	if _, ok := c.entries[key]; !ok && size <= maxBytes {
		for len(c.entries) >= 64 || c.bytes+size > maxBytes {
			var oldest string
			var order uint64 = ^uint64(0)
			for k, v := range c.entries {
				if v.order < order {
					oldest, order = k, v.order
				}
			}
			c.bytes -= c.entries[oldest].bytes
			delete(c.entries, oldest)
		}
		c.order++
		c.entries[key] = treeSnapshot{nodes: nodes, bytes: size, order: c.order}
		c.bytes += size
	}
	return result
}
