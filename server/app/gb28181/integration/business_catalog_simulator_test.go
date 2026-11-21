package integration

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/catalog"
	"uvplatform.com/uvp-gb28181/app/gb28181/directory"
	"uvplatform.com/uvp-gb28181/app/gb28181/handler"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// Uses a real simulator process, digest registration and SIP Catalog exchange;
// all ingested resources live in the harness's isolated SQLite database.
func TestDesktopSimulatorBusinessCatalogRoundTrip(t *testing.T) {
	t.Setenv("CATALOG_TEMPLATE", "business-platform")
	h := newUpgradeHarness(t)
	require.NoError(t, h.db.AutoMigrate(&gbmodels.GbCatalogNode{}, &gbmodels.GbChannelMount{}, &gbmodels.GbAnomalyRecord{}, &gbmodels.GbAlarmResource{}))
	handler.SetCatalogPipeline(catalog.New(h.db))
	t.Cleanup(func() { handler.SetCatalogPipeline(nil) })
	h.startSimulator(t)
	device := h.device(t)
	body, err := manscdp.BuildCatalogQuery(simulatorDeviceCode, 910001)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := h.uac.SendMessageTracked(ctx, simulatorDeviceCode, net.JoinHostPort(device.IP, strconv.Itoa(device.Port)), "UDP", body)
	require.NoError(t, err)
	require.Equal(t, 200, result.StatusCode)
	require.Eventually(t, func() bool {
		var count int64
		return h.db.Model(&gbmodels.GbChannel{}).Where("device_id = ?", simulatorDeviceCode).Count(&count).Error == nil && count >= 3
	}, 10*time.Second, 50*time.Millisecond, "business template channels must arrive through SIP and be ingested")
	tree, err := directory.BuildBusinessTree(context.Background(), h.db, 1)
	require.NoError(t, err)
	require.NotEmpty(t, tree)
	var groups, orgs, channels int
	var walk func([]directory.DirectoryNodeVO, bool, string)
	walk = func(nodes []directory.DirectoryNodeVO, inGroup bool, parentType string) {
		for _, node := range nodes {
			childInGroup := inGroup
			switch node.Type {
			case string(gbmodels.NodeTypeBizGroup):
				groups++
				require.False(t, inGroup, "business groups cannot nest")
				childInGroup = true
			case string(gbmodels.NodeTypeVirtualOrg):
				orgs++
				require.True(t, inGroup, "virtual organizations must belong to a group")
			case "channel":
				channels++
				require.Equal(t, string(gbmodels.NodeTypeVirtualOrg), parentType)
				require.True(t, inGroup)
				require.Nil(t, node.Device, "a business channel cannot turn into the whole registered device")
			case "device":
				t.Fatal("business tree must not insert the registered device")
			}
			walk(node.Children, childInGroup, node.Type)
		}
	}
	walk(tree, false, "")
	require.Equal(t, 1, groups)
	require.Equal(t, 3, orgs)
	require.Equal(t, 3, channels)
}
