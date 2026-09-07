package app

import (
	"gorm.io/gorm"
	"testing"
)

func TestDatabaseSelectionRejectsUnknownAndSQLiteMultiDatabase(t *testing.T) {
	cases := []struct {
		primary    string
		enabled    map[string]bool
		standalone bool
		valid      bool
	}{
		{"mysql", map[string]bool{"mysql": true}, false, true},
		{"postgresql", map[string]bool{"postgresql": true}, false, true},
		{"sqlserver", map[string]bool{"sqlserver": true}, false, true},
		{"sqlite", map[string]bool{"sqlite": true}, true, true},
		{"unknown", map[string]bool{"mysql": true}, false, false},
		{"sqlite", map[string]bool{"sqlite": true, "mysql": true}, true, false},
		{"mysql", map[string]bool{"mysql": true}, true, false},
	}
	for _, c := range cases {
		err := ValidateDatabaseSelection(c.primary, c.enabled, c.standalone)
		if (err == nil) != c.valid {
			t.Errorf("%s standalone=%v error=%v", c.primary, c.standalone, err)
		}
	}
}

func TestDBSelectsSQLiteWithoutChangingServerSelections(t *testing.T) {
	oldSQLite, oldMySQL, oldPostgres, oldSQLServer := GormDbSQLite, GormDbMysql, GormDbPostgreSql, GormDbSqlserver
	t.Cleanup(func() {
		GormDbSQLite, GormDbMysql, GormDbPostgreSql, GormDbSqlserver = oldSQLite, oldMySQL, oldPostgres, oldSQLServer
	})
	GormDbSQLite, GormDbMysql, GormDbPostgreSql, GormDbSqlserver = &gorm.DB{}, &gorm.DB{}, &gorm.DB{}, &gorm.DB{}
	for name, expected := range map[string]*gorm.DB{"sqlite": GormDbSQLite, "mysql": GormDbMysql, "postgresql": GormDbPostgreSql, "sqlserver": GormDbSqlserver} {
		if DB(name) != expected {
			t.Errorf("%s selected the wrong connection", name)
		}
	}
}
