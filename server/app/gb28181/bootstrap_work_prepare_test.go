package gb28181

import (
	"context"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"path/filepath"
	"testing"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/workrecording"
	gbzlm "uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
)

type workDirectoryClient struct {
	root       string // rootPath answered for a customized-path call
	configured string // record.filePath exactly as the node reports it
	appName    string // record.appName exactly as the node reports it
	probeRoot  string // rootPath answered for the uncustomized probe
	requested  string
	probed     bool
}

func (c *workDirectoryClient) GetServerConfig(context.Context) (map[string]string, error) {
	config := map[string]string{}
	// A node only reports the keys its config.ini actually carries.
	if c.configured != "" {
		config["record.filePath"] = c.configured
	}
	if c.appName != "" {
		config["record.appName"] = c.appName
	}
	return config, nil
}
func (c *workDirectoryClient) GetMP4RecordFilesInDirectory(_ context.Context, _, _, _, period, directory string) (*gbzlm.MP4RecordListing, error) {
	c.requested = directory
	return &gbzlm.MP4RecordListing{RootPath: c.root}, nil
}
func (c *workDirectoryClient) ProbeMP4RecordRoot(context.Context, string, string, string) (*gbzlm.MP4RecordListing, error) {
	c.probed = true
	return &gbzlm.MP4RecordListing{RootPath: c.probeRoot}, nil
}

func TestPrepareWorkDirectoryRequiresNodeResolvedJobRoot(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	client := &workDirectoryClient{configured: "/node/record", root: "/node/record/work-recordings/" + id + "/rtp/stream/"}
	root, err := prepareWorkDirectory(context.Background(), client, workrecording.MediaTarget{VHost: "__defaultVhost__", App: "rtp", Stream: "stream"}, id)
	require.NoError(t, err)
	require.Equal(t, "/node/record/work-recordings/"+id, root)
	require.Equal(t, "/node/record/work-recordings/"+id, client.requested)
	require.False(t, client.probed, "已显式配置绝对路径时不应再去探测节点默认根")

	client.root = "/node/record/rtp/stream/"
	_, err = prepareWorkDirectory(context.Background(), client, workrecording.MediaTarget{}, id)
	require.Error(t, err)
}

// ZLM keeps its record root in code, so a node that never had record.filePath
// written into config.ini reports no such key. Recording must still work there:
// the node is asked where it would record instead of a human being asked to copy
// a value the node already knows.
func TestPrepareWorkDirectoryDerivesTheRootWhenTheNodeHasNoRecordPath(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	client := &workDirectoryClient{
		appName:   "record",
		probeRoot: "/opt/media/bin/www/record/rtp/stream/",
		root:      "/opt/media/bin/www/work-recordings/" + id + "/record/rtp/stream/",
	}
	root, err := prepareWorkDirectory(context.Background(), client, workrecording.MediaTarget{VHost: "__defaultVhost__", App: "rtp", Stream: "stream"}, id)
	require.NoError(t, err)
	require.True(t, client.probed)
	require.Equal(t, "/opt/media/bin/www/work-recordings/"+id, root)
	require.Equal(t, "/opt/media/bin/www/work-recordings/"+id, client.requested)
}

// A relative record.filePath can never own a job directory: the node echoes a
// relative custom path back relative and ResolvedWorkDirectory refuses that. Fall
// back to the node's own root rather than failing a node that can record fine.
func TestPrepareWorkDirectoryIgnoresARelativeRecordPath(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	client := &workDirectoryClient{
		configured: "./www/record",
		appName:    "record",
		probeRoot:  "/opt/media/bin/www/record/rtp/stream/",
		root:       "/opt/media/bin/www/work-recordings/" + id + "/record/rtp/stream/",
	}
	root, err := prepareWorkDirectory(context.Background(), client, workrecording.MediaTarget{VHost: "__defaultVhost__", App: "rtp", Stream: "stream"}, id)
	require.NoError(t, err)
	require.True(t, client.probed)
	require.Equal(t, "/opt/media/bin/www/work-recordings/"+id, root)
}

func TestPrepareWorkDirectoryRejectsAnUnusableProbeAnswer(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	// The probe answered about a different stream, so it says nothing about our root.
	client := &workDirectoryClient{appName: "record", probeRoot: "/opt/media/bin/www/record/rtp/other-stream/"}
	_, err := prepareWorkDirectory(context.Background(), client, workrecording.MediaTarget{VHost: "__defaultVhost__", App: "rtp", Stream: "stream"}, id)
	require.Error(t, err)
	require.Contains(t, err.Error(), "无法确定节点录像根目录")
	require.Empty(t, client.requested, "根目录不可用时不得再向节点请求作业目录")
}

func TestPersistedWorkLeaseProtectsBoundAndReservedSources(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "claims.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.GbChannel{}, &models.GbRecorderClaim{}))
	channel := models.GbChannel{DeviceID: "d", ChannelID: "c", StreamID: "reserved"}
	require.NoError(t, db.Create(&channel).Error)
	claim := models.GbRecorderClaim{ResourceKey: workrecording.ChannelResource(channel.ID), ChannelID: channel.ID, OwnerKind: workrecording.OwnerWork, OwnerID: "job", State: workrecording.StateStarting, Version: 1}
	require.NoError(t, db.Create(&claim).Error)
	require.True(t, hasPersistedWorkLease(context.Background(), db, "reserved"))
	require.False(t, hasPersistedWorkLease(context.Background(), db, "other"))
	require.NoError(t, db.Model(&claim).Update("stream", "bound").Error)
	require.True(t, hasPersistedWorkLease(context.Background(), db, "bound"))
	require.NoError(t, db.Model(&claim).Update("state", workrecording.StateIdle).Error)
	require.False(t, hasPersistedWorkLease(context.Background(), db, "reserved"))
	require.NoError(t, db.Migrator().DropTable(&models.GbRecorderClaim{}))
	require.True(t, hasPersistedWorkLease(context.Background(), db, "reserved"))
}
