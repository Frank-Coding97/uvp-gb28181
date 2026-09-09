package workrecording

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
)

func registerProductionNotFoundHook(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("workrecording:production_not_found", gormhelper.MaskNotDataError))
}

func TestWorkRecordingQueriesHandleProductionNotFoundHook(t *testing.T) {
	db := claimDB(t)
	require.NoError(t, db.AutoMigrate(&models.GbChannel{}, &models.GbWorkRecording{}))
	registerProductionNotFoundHook(t, db)
	ctx := context.Background()

	_, err := NewClaims(db).Get(ctx, ChannelResource(3020))
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)

	service := NewService(db, nil, nil)
	_, found, err := service.findRequest(ctx, 100, "missing-request")
	require.NoError(t, err)
	require.False(t, found)

	_, err = service.findJob(ctx, uuid.NewString())
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)

	_, err = loadLegacyIdleChannel(ctx, db, 3020)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)

	jobID := uuid.NewString()
	job, err := NewFileAttributionRepository(db).Resolve(ctx, FileAttributionInput{
		NodeID: 1, VHost: "v", App: "rtp", Stream: "stream",
		FilePath: filepath.Join("/opt/zlm/work-recordings", jobID, "clip.mp4"),
	})
	require.Nil(t, job)
	require.ErrorIs(t, err, ErrAttributionUnknown)
}
