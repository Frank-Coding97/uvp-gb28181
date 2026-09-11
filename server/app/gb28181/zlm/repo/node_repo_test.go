package repo_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/repo"
)

// setupDB uses the shipped SQLite schema and production connection settings.
func setupDB(t *testing.T) *gorm.DB {
	t.Helper()
	return newSQLiteBaselineRepoDB(t)
}

func TestNodeRepo_CreateGetUpdateDelete(t *testing.T) {
	db := setupDB(t)
	r := repo.NewMetaNodeRepo(db)
	ctx := context.Background()

	in := node.Node{
		Name:            "zlm-1",
		Host:            "1.1.1.1",
		ReceiveHost:     "203.0.113.10",
		PlaybackHost:    "play.example.com",
		APIPort:         18080,
		APISecret:       "s",
		MediaServerUUID: "uuid-1",
		Weight:          50,
		State:           node.StateActive,
		RTPPortStart:    30000,
		RTPPortEnd:      35000,
	}

	id, err := r.Create(ctx, in)
	require.NoError(t, err)
	require.NotZero(t, id)

	got, err := r.Get(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "zlm-1", got.Name)
	require.Equal(t, "uuid-1", got.MediaServerUUID)
	require.Equal(t, "203.0.113.10", got.ReceiveHost)
	require.Equal(t, "play.example.com", got.PlaybackHost)
	require.Equal(t, node.StateActive, got.State)
	require.Equal(t, 30000, got.RTPPortStart)

	got.Name = "zlm-1-renamed"
	got.Weight = 80
	require.NoError(t, r.Update(ctx, *got))

	got2, _ := r.Get(ctx, id)
	require.Equal(t, "zlm-1-renamed", got2.Name)
	require.Equal(t, 80, got2.Weight)

	require.NoError(t, r.Delete(ctx, id))
	got3, _ := r.Get(ctx, id)
	require.Nil(t, got3)
}

func TestNodeRepo_List_OrderedByID(t *testing.T) {
	db := setupDB(t)
	r := repo.NewMetaNodeRepo(db)
	ctx := context.Background()

	for _, name := range []string{"c", "a", "b"} {
		_, err := r.Create(ctx, node.Node{Name: name, MediaServerUUID: "u-" + name, State: node.StateActive})
		require.NoError(t, err)
	}

	list, err := r.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 3)
	// 创建顺序 = ID 顺序 = c, a, b
	require.Equal(t, "c", list[0].Name)
	require.Equal(t, "a", list[1].Name)
	require.Equal(t, "b", list[2].Name)
}

func TestNodeRepo_Get_NotFound_ReturnsNil(t *testing.T) {
	db := setupDB(t)
	r := repo.NewMetaNodeRepo(db)
	got, err := r.Get(context.Background(), 9999)
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestNodeRepo_Update_Idempotent(t *testing.T) {
	db := setupDB(t)
	r := repo.NewMetaNodeRepo(db)
	ctx := context.Background()

	id, _ := r.Create(ctx, node.Node{Name: "a", MediaServerUUID: "u-a", State: node.StateActive, Weight: 50})
	got, _ := r.Get(ctx, id)
	require.NoError(t, r.Update(ctx, *got))
	require.NoError(t, r.Update(ctx, *got))

	got2, _ := r.Get(ctx, id)
	require.Equal(t, 50, got2.Weight)
}

func TestNodeRepo_TagsAsJSON(t *testing.T) {
	db := setupDB(t)
	r := repo.NewMetaNodeRepo(db)
	ctx := context.Background()

	in := node.Node{
		Name:            "a",
		MediaServerUUID: "u-a",
		State:           node.StateActive,
		Tags:            map[string]string{"env": "prod", "zone": "bj"},
	}
	id, err := r.Create(ctx, in)
	require.NoError(t, err)

	got, _ := r.Get(ctx, id)
	require.Equal(t, "prod", got.Tags["env"])
	require.Equal(t, "bj", got.Tags["zone"])
}

func TestNodeRepo_UpdateCASRejectsStaleFullRow(t *testing.T) {
	db := setupDB(t)
	r := repo.NewMetaNodeRepo(db)
	ctx := context.Background()

	id, err := r.Create(ctx, node.Node{
		Name: "a", Host: "old.example", APISecret: "secret", MediaServerUUID: "u-a",
		State: node.StateActive, Weight: 50,
	})
	require.NoError(t, err)
	current, err := r.Get(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, current)
	require.EqualValues(t, 1, current.Revision)

	candidate := *current
	candidate.Weight = 80
	ok, err := r.UpdateCAS(ctx, candidate, current.Revision)
	require.NoError(t, err)
	require.True(t, ok)

	stale := candidate
	stale.Name = "stale"
	ok, err = r.UpdateCAS(ctx, stale, current.Revision)
	require.NoError(t, err)
	require.False(t, ok)

	after, err := r.Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "a", after.Name)
	require.Equal(t, 80, after.Weight)
	require.EqualValues(t, 2, after.Revision)
}
