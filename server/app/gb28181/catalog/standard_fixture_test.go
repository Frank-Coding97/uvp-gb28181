package catalog_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/catalog"
	"uvplatform.com/uvp-gb28181/app/gb28181/directory"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// The hand-written Appendix J fixture is independent of either simulator.
// Reverse delivery is a robustness case, not an Appendix M sending requirement.
func TestAppendixJIndependentFixture(t *testing.T) {
	body, err := os.ReadFile("../../../../docs/device-channel-tree/business-audit/standard-scenario.xml")
	require.NoError(t, err)
	response, err := manscdp.ParseCatalogResponse(body)
	require.NoError(t, err)
	require.Equal(t, 6, response.SumNum)
	items := make([]catalog.CatalogItem, 0, 6)
	for _, item := range response.DeviceList.Items {
		items = append(items, catalog.CatalogItem{DeviceID: item.DeviceID, Name: item.Name,
			CivilCode: item.CivilCode, ParentID: item.ParentID,
			BusinessGroupID: item.BusinessGroupID, StatusOn: item.IsOnline()})
	}
	for _, reverse := range []bool{false, true} {
		name := "ordered"
		if reverse {
			name = "reverse_individual_batches"
		}
		t.Run(name, func(t *testing.T) {
			db := newPipelineTestDB(t)
			pipeline := catalog.New(db)
			sender := catalog.Sender{OwnerDeptID: 4, SourceDeviceID: response.DeviceID}
			for index := range items {
				if reverse {
					index = len(items) - 1 - index
				}
				require.NoError(t, pipeline.Ingest(context.Background(), sender, items[index:index+1]))
			}
			var nodes []gbmodels.GbCatalogNode
			require.NoError(t, db.Where("source_device_id = ?", response.DeviceID).Find(&nodes).Error)
			require.Len(t, nodes, 6, "repeated delivery and late parents must reuse identities")
			byID := map[uint]gbmodels.GbCatalogNode{}
			var all []gbmodels.GbCatalogNode
			require.NoError(t, db.Find(&all).Error)
			for _, node := range all {
				byID[node.ID] = node
			}
			for _, node := range nodes {
				if node.ParentID != nil {
					parent := byID[*node.ParentID]
					require.Equal(t, catalog.BuildPath(parent.Path, node.ID), node.Path)
					require.Equal(t, parent.Depth+1, node.Depth, "late-parent descendant depth: %s path=%s parent=%s", node.Code, node.Path, parent.Code)
				}
			}
			tree, err := directory.BuildBusinessTree(context.Background(), db, 4)
			require.NoError(t, err)
			require.Len(t, tree, 1)
			if tree[0].Count != 2 {
				for _, node := range nodes {
					t.Logf("code=%s type=%s parent=%v path=%s channel=%v", node.Code, node.NodeType, node.ParentID, node.Path, node.ChannelID)
				}
			}
			require.Equal(t, "37010301022150000007", tree[0].Code)
			require.EqualValues(t, 2, tree[0].Count)
			require.EqualValues(t, 2, tree[0].OnlineCount)
			require.Len(t, tree[0].Children, 1)
			org := tree[0].Children[0]
			require.Equal(t, "37010301022160000007", org.Code)
			require.Len(t, org.Children, 2)
			var channels, organizations int
			var walk func([]directory.DirectoryNodeVO)
			walk = func(nodes []directory.DirectoryNodeVO) {
				for _, node := range nodes {
					switch node.Type {
					case "channel":
						channels++
						require.Nil(t, node.Device)
					case "virtual_org":
						organizations++
					case "device":
						t.Fatal("registered system must not become a business leaf")
					}
					walk(node.Children)
				}
			}
			walk(tree)
			require.Equal(t, 2, channels)
			require.Equal(t, 2, organizations)
			require.NoError(t, pipeline.Ingest(context.Background(), sender, items))
			var repeatedCount int64
			require.NoError(t, db.Model(&gbmodels.GbCatalogNode{}).Where("source_device_id = ?", response.DeviceID).Count(&repeatedCount).Error)
			require.EqualValues(t, 6, repeatedCount)
		})
	}
}
