package catalog_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/gb28181/catalog"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
)

func TestPipelineRefreshSkipsUnchangedCatalogNodeWrites(t *testing.T) {
	body, err := os.ReadFile("testdata/standard-scenario.xml")
	require.NoError(t, err)
	response, err := manscdp.ParseCatalogResponse(body)
	require.NoError(t, err)

	items := make([]catalog.CatalogItem, 0, len(response.DeviceList.Items))
	for _, item := range response.DeviceList.Items {
		items = append(items, catalog.CatalogItem{
			DeviceID: item.DeviceID, Name: item.Name, Manufacturer: item.Manufacturer, Model: item.Model,
			CivilCode: item.CivilCode, ParentID: item.ParentID, BusinessGroupID: item.BusinessGroupID,
			StatusOn: item.IsOnline(),
		})
	}
	db := newPipelineTestDB(t)
	pipeline := catalog.New(db)
	sender := catalog.Sender{OwnerDeptID: 4, SourceDeviceID: response.DeviceID}
	require.NoError(t, pipeline.Ingest(context.Background(), sender, items))

	updates := map[string]int{"gb_channel": 0, "gb_catalog_node": 0}
	nodeReads := 0
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:catalog-refresh-update-count", func(tx *gorm.DB) {
		if _, ok := updates[tx.Statement.Table]; ok {
			updates[tx.Statement.Table]++
		}
	}))
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:catalog-refresh-query-count", func(tx *gorm.DB) {
		if tx.Statement.Table == "gb_catalog_node" {
			nodeReads++
		}
	}))
	require.NoError(t, pipeline.Ingest(context.Background(), sender, items))

	require.Equal(t, map[string]int{"gb_channel": 2, "gb_catalog_node": 0}, updates,
		"keep channel updates for their visible UpdatedAt, but skip unchanged catalog-node writes")
	// Identity reconciliation adds one read per item to the cached directory lookups.
	require.Less(t, nodeReads, 20+len(items), "repeated administrative-area chains should reuse nodes within one refresh")
	t.Logf("repeat refresh: catalog-node SELECTs=%d, channel UPDATEs=%d, catalog-node UPDATEs=%d",
		nodeReads, updates["gb_channel"], updates["gb_catalog_node"])
}
