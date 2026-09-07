package sqlitedialect

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestNormalizeDSNAddsSQLiteTimeFormatWithoutRewritingQuery(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{name: "memory", dsn: ":memory:", want: ":memory:?_time_format=sqlite"},
		{name: "query", dsn: "file:test?mode=memory&cache=shared", want: "file:test?mode=memory&cache=shared&_time_format=sqlite"},
		{name: "empty query", dsn: "file:test?", want: "file:test?_time_format=sqlite"},
		{name: "existing", dsn: "file:test?_time_format=sqlite&cache=shared", want: "file:test?_time_format=sqlite&cache=shared"},
		{name: "same duplicate", dsn: "file:test?_time_format=sqlite&_time_format=sqlite", want: "file:test?_time_format=sqlite&_time_format=sqlite"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeDSN(tt.dsn)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestNormalizeDSNRejectsConflictingTimeFormats(t *testing.T) {
	for _, dsn := range []string{
		"",
		"?mode=memory",
		"file:test?_time_format=datetime",
		"file:test?_time_format=sqlite&_time_format=datetime",
		"file:test?_time_format=sqlite&_time_format=",
		"file:test?_time_format=",
	} {
		_, err := normalizeDSN(dsn)
		require.Error(t, err, dsn)
	}
}

func TestOpenReportsDSNConflictThroughGORM(t *testing.T) {
	_, err := gorm.Open(Open("file::memory:?_time_format=datetime"), &gorm.Config{})
	require.Error(t, err)
	require.ErrorContains(t, err, "_time_format")
}

func TestOpenUsesSQLiteTimeFormatAndPreservesOffset(t *testing.T) {
	db, err := gorm.Open(Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	rawDB, err := db.DB()
	require.NoError(t, err)
	rawDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = rawDB.Close() })

	require.NoError(t, db.Exec("CREATE TABLE values_with_time (value DATETIME)").Error)
	value := time.Date(2026, 9, 7, 12, 34, 56, 123456789, time.FixedZone("offset", -5*60*60))
	require.NoError(t, db.Exec("INSERT INTO values_with_time(value) VALUES (?)", value).Error)

	var raw string
	require.NoError(t, rawDB.QueryRow("SELECT CAST(value AS TEXT) FROM values_with_time").Scan(&raw))
	require.Equal(t, "2026-09-07 12:34:56.123456789-05:00", raw)

	var loaded time.Time
	require.NoError(t, rawDB.QueryRow("SELECT value FROM values_with_time").Scan(&loaded))
	require.True(t, value.Equal(loaded))
}

func TestOpenUsesModernSQLiteDriver(t *testing.T) {
	db, err := gorm.Open(Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	rawDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = rawDB.Close() })

	var version string
	require.NoError(t, rawDB.QueryRow("SELECT sqlite_version()").Scan(&version))
	require.Equal(t, "3.53.4", version)
}

type translationParent struct {
	ID uint
}

type translationChild struct {
	ID       uint
	ParentID uint
	Parent   translationParent
}

type translationUnique struct {
	ID    uint
	Value string `gorm:"uniqueIndex"`
}

type translationPrimary struct {
	ID uint
}

func TestOpenTranslatesModernSQLiteConstraintErrors(t *testing.T) {
	db, err := gorm.Open(Open(":memory:?_pragma=foreign_keys(1)"), &gorm.Config{TranslateError: true})
	require.NoError(t, err)
	rawDB, err := db.DB()
	require.NoError(t, err)
	rawDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = rawDB.Close() })
	require.NoError(t, db.AutoMigrate(&translationParent{}, &translationChild{}, &translationUnique{}, &translationPrimary{}))

	require.NoError(t, db.Create(&translationUnique{Value: "same"}).Error)
	err = db.Create(&translationUnique{Value: "same"}).Error
	require.ErrorIs(t, err, gorm.ErrDuplicatedKey)

	require.NoError(t, db.Create(&translationPrimary{ID: 7}).Error)
	err = db.Create(&translationPrimary{ID: 7}).Error
	require.ErrorIs(t, err, gorm.ErrDuplicatedKey)

	err = db.Create(&translationChild{ParentID: 999}).Error
	require.ErrorIs(t, err, gorm.ErrForeignKeyViolated)
}
