package recording

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/workrecording"
)

type guardedRecorderFake struct{ starts, stops int }

func (c *guardedRecorderFake) IsRecording(context.Context, string, string, string) (bool, error) {
	return false, nil
}
func (c *guardedRecorderFake) StartRecord(context.Context, string, string, string, int) error {
	c.starts++
	return nil
}
func (c *guardedRecorderFake) StopRecord(context.Context, string, string, string) error {
	c.stops++
	return nil
}

func TestGuardedLegacyClientCannotMutateReservedWorkChannel(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "guard.db")), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	defer raw.Close()
	require.NoError(t, db.AutoMigrate(&models.GbRecorderClaim{}))
	claims := workrecording.NewClaims(db)
	core := workrecording.NewRecorder(claims, nil, nil)
	_, err = core.Reserve(context.Background(), 9, workrecording.Owner{Kind: workrecording.OwnerWork, ID: "work"})
	require.NoError(t, err)
	underlying := &guardedRecorderFake{}
	client := GuardRecorderClient(underlying, core.MutationGate(), 1, func(context.Context, string) (*models.GbChannel, error) { return &models.GbChannel{ID: 9}, nil })
	require.ErrorIs(t, client.StartRecord(context.Background(), "v", "rtp", "s", 0), workrecording.ErrOwnerConflict)
	require.ErrorIs(t, client.StopRecord(context.Background(), "v", "rtp", "s"), workrecording.ErrOwnerConflict)
	active, err := client.IsRecording(context.Background(), "v", "rtp", "s")
	require.False(t, active)
	require.ErrorIs(t, err, workrecording.ErrOwnerConflict)
	require.Zero(t, underlying.starts)
	require.Zero(t, underlying.stops)
}

func TestGuardedLegacyClientAllowsIdleAndRejectsLookupFailure(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "guard.db")), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	defer raw.Close()
	require.NoError(t, db.AutoMigrate(&models.GbRecorderClaim{}))
	core := workrecording.NewRecorder(workrecording.NewClaims(db), nil, nil)
	underlying := &guardedRecorderFake{}
	client := GuardRecorderClient(underlying, core.MutationGate(), 1, func(context.Context, string) (*models.GbChannel, error) { return nil, nil })
	require.NoError(t, client.StartRecord(context.Background(), "v", "rtp", "s", 0))
	require.NoError(t, client.StopRecord(context.Background(), "v", "rtp", "s"))
	require.Equal(t, 1, underlying.starts)
	require.Equal(t, 1, underlying.stops)
	client = GuardRecorderClient(underlying, core.MutationGate(), 1, func(context.Context, string) (*models.GbChannel, error) { return nil, gorm.ErrInvalidDB })
	require.Error(t, client.StopRecord(context.Background(), "v", "rtp", "s"))
	require.Equal(t, 1, underlying.stops)
}
