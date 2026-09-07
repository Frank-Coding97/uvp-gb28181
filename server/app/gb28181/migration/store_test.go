package migration

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	return NewStore(db)
}

func TestEnsureTableIdempotent(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.EnsureTable())
	require.NoError(t, s.EnsureTable())
}

func TestMarkAndListApplied(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.EnsureTable())
	require.NoError(t, s.MarkApplied([]string{"b", "a"}))
	got, err := s.ListApplied()
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b"}, got)
}

func TestIsEmpty(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.EnsureTable())
	empty, err := s.IsEmpty()
	require.NoError(t, err)
	require.True(t, empty)

	require.NoError(t, s.MarkApplied([]string{"a"}))
	empty, err = s.IsEmpty()
	require.NoError(t, err)
	require.False(t, empty)
}

func TestDeleteApplied(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.EnsureTable())
	require.NoError(t, s.MarkApplied([]string{"a", "b"}))
	require.NoError(t, s.DeleteApplied("a"))
	got, err := s.ListApplied()
	require.NoError(t, err)
	require.Equal(t, []string{"b"}, got)
}

func TestMarkAppliedEmptyList(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.EnsureTable())
	require.NoError(t, s.MarkApplied([]string{}))
}
