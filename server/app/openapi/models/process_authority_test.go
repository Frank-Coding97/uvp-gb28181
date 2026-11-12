package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestProcessAuthoritySchema(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.Exec("PRAGMA foreign_keys=ON").Error)
	require.NoError(t, db.AutoMigrate(&ProcessGeneration{}, &ProcessAuthority{}))
	generation := ProcessGeneration{GenerationID: "00000000000000000000000000000001", DomainID: "00000000000000000000000000000002", StartedAt: time.Now().UTC()}
	authority := ProcessAuthority{ID: 1, DomainID: generation.DomainID, CurrentGenerationID: generation.GenerationID, RowVersion: 1}
	require.Error(t, db.Omit("Generation").Create(&authority).Error, "missing generation cannot become current")
	require.NoError(t, db.Create(&generation).Error)
	require.Error(t, db.Create(&generation).Error, "generation IDs are immutable identities")
	require.NoError(t, db.Omit("Generation").Create(&authority).Error)
	for _, mutation := range []map[string]any{{"id": 2}, {"row_version": 0}, {"domain_id": "00000000000000000000000000000003"}, {"current_generation_id": "00000000000000000000000000000004"}} {
		require.Error(t, db.Model(&ProcessAuthority{}).Where("id=1").Updates(mutation).Error)
	}
	require.Error(t, db.Delete(&generation).Error, "current authority cannot lose its generation")
	encoded, err := json.Marshal(authority)
	require.NoError(t, err)
	require.JSONEq(t, "{}", string(encoded), "internal authority is not an API DTO")
}
