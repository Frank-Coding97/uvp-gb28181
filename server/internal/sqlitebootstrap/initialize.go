package sqlitebootstrap

import (
	"context"

	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/civilcode"
	"uvplatform.cn/uvp-gb28181/resource/database/sqlitebaseline"
)

type Result struct {
	Version  string `json:"version"`
	Checksum string `json:"checksum"`
	Created  bool   `json:"created"`
}

// Initialize applies the pinned release baseline once. Existing user data is
// never reset; an unrelated non-empty database is rejected.
func Initialize(ctx context.Context, db *gorm.DB) (*Result, error) {
	created, err := applyBaseline(ctx, db, sqlitebaseline.Version, sqlitebaseline.SQL, sqlitebaseline.SHA256, func(tx *gorm.DB) error {
		_, err := civilcode.SeedIfEmpty(tx)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &Result{Version: sqlitebaseline.Version, Checksum: sqlitebaseline.SHA256, Created: created}, nil
}
