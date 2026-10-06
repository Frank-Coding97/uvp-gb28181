package recording

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// ⛔ 筛选项「存储节点」口径（2026-10-06 老板拍板：列出**全部在线节点**）。
//
// 起因：老板有两个流媒体节点，但下拉只出现一个。根因是 `FileOptions` 原先拿
// `CatalogOptions` 的结果，而它是 `Distinct("node_id")` **从录像表里取的**
// ⇒ 刚接入、还没出录像的节点根本不会出现在下拉里。
//
// 口径改成：按平台**已注册的节点**全给（`nodeSnapshot().known`），
// 空节点选了返回 0 条，由前端在选项上标状态（离线/维护中）。
func TestFileOptionsListsAllRegisteredNodesNotOnlyOnesWithRecordings(t *testing.T) {
	db := newCatalogServiceDB(t)
	// 只给 node 2 写一条录像；node 5 存在但**没有录像**
	start := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	onlyOnNode2 := catalogFile(1, 1, 2, start, "only-on-node-2.mp4")
	require.NoError(t, db.Create(&onlyOnNode2).Error)

	nodes := catalogTestNodes{nodes: map[int64]*node.Node{
		2: {ID: 2, Name: "zlm-220", State: node.StateActive},
		5: {ID: 5, Name: "192.168.10.220:18090", State: node.StateActive},
	}}
	service := NewCatalogService(CatalogServiceConfig{
		Repo:          NewGormRepo(db),
		Nodes:         nodes,
		ResolveAccess: func(context.Context, uint) (CatalogAccess, error) { return CatalogAccess{DeptIDs: []uint{1}}, nil },
	})

	options, err := service.FileOptions(context.Background(), 7, FileQuery{})
	require.NoError(t, err)

	ids := make([]string, 0, len(options.Nodes))
	for _, item := range options.Nodes {
		ids = append(ids, item.ID)
	}
	require.Equal(t, []string{"2", "5"}, ids, "两个已注册节点都要出现在下拉里，哪怕 5 号还没有录像")
	require.Equal(t, "zlm-220", options.Nodes[0].Name)
}

// 离线节点也要列出来 —— 老板要能筛"这个节点的录像"，
// 即使它此刻离线（录像还在，只是暂时不可播放）。
func TestFileOptionsKeepsOfflineNode(t *testing.T) {
	db := newCatalogServiceDB(t)
	nodes := catalogTestNodes{nodes: map[int64]*node.Node{
		2: {ID: 2, Name: "zlm-220", State: node.StateActive},
		7: {ID: 7, Name: "down-node", State: node.StateOffline},
	}}
	service := NewCatalogService(CatalogServiceConfig{
		Repo:          NewGormRepo(db),
		Nodes:         nodes,
		ResolveAccess: func(context.Context, uint) (CatalogAccess, error) { return CatalogAccess{DeptIDs: []uint{1}}, nil },
	})

	options, err := service.FileOptions(context.Background(), 7, FileQuery{})
	require.NoError(t, err)
	require.Len(t, options.Nodes, 2, "离线节点不能被筛选项藏掉，否则用户看不到它的历史录像")
	require.Equal(t, "offline", options.Nodes[1].State, "离线状态要如实回传，供前端标注")
}

// 没有任何录像时，节点选项依然要全给（这正是老板那个 bug 的极端形态）。
func TestFileOptionsListsNodesEvenWithZeroRecordings(t *testing.T) {
	db := newCatalogServiceDB(t)
	nodes := catalogTestNodes{nodes: map[int64]*node.Node{
		2: {ID: 2, Name: "zlm-220", State: node.StateActive},
		5: {ID: 5, Name: "edge-b", State: node.StateActive},
	}}
	service := NewCatalogService(CatalogServiceConfig{
		Repo:          NewGormRepo(db),
		Nodes:         nodes,
		ResolveAccess: func(context.Context, uint) (CatalogAccess, error) { return CatalogAccess{DeptIDs: []uint{1}}, nil },
	})

	options, err := service.FileOptions(context.Background(), 7, FileQuery{})
	require.NoError(t, err)
	require.Len(t, options.Nodes, 2, "库里一条录像都没有时，节点选项不能是空的")
}
