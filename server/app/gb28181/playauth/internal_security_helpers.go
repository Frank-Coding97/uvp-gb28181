package playauth

import (
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func validNodeUUID(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || len(value) > 64 || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

func validBootNonce(value string) bool {
	if len(value) != 32 || value != strings.ToLower(value) {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func lockedModel(tx *gorm.DB, model any, table string) *gorm.DB {
	if tx != nil && tx.Name() == "sqlserver" {
		return tx.Table(table + " WITH (UPDLOCK, HOLDLOCK)")
	}
	return tx.Model(model).Clauses(clause.Locking{Strength: "UPDATE"})
}
