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
	root       string
	configured string
	requested  string
}

func (c *workDirectoryClient) GetServerConfig(context.Context) (map[string]string, error) {
	return map[string]string{"record.filePath": c.configured}, nil
}
func (c *workDirectoryClient) GetMP4RecordFilesInDirectory(_ context.Context, _, _, _, period, directory string) (*gbzlm.MP4RecordListing, error) {
	c.requested = directory
	return &gbzlm.MP4RecordListing{RootPath: c.root}, nil
}
func TestPrepareWorkDirectoryRequiresNodeResolvedJobRoot(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	client := &workDirectoryClient{configured: "./record", root: "/node/record/work-recordings/" + id + "/rtp/stream/"}
	root, err := prepareWorkDirectory(context.Background(), client, workrecording.MediaTarget{VHost: "__defaultVhost__", App: "rtp", Stream: "stream"}, id)
	require.NoError(t, err)
	require.Equal(t, "/node/record/work-recordings/"+id, root)
	require.Equal(t, "record/work-recordings/"+id, client.requested)
	client.root = "/node/record/rtp/stream/"
	_, err = prepareWorkDirectory(context.Background(), client, workrecording.MediaTarget{}, id)
	require.Error(t, err)
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
