package migration

import (
	"regexp"
	"strings"
	"sync"
	"testing"

	dameng "github.com/godoes/gorm-dameng"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/schema"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// Compare the embedded DDL with the application's native DM dialector rather
// than assuming Oracle identity syntax works in every DM compatibility mode.
// The opt-in native database test remains the syntax and lifecycle acceptance.
func TestVideoShareDMIdentityMatchesNativeDialect(t *testing.T) {
	body, err := (&embedSource{dialect: DialectDM}).ReadSQL("2026-11-17-channel-video-share-dm.sql")
	require.NoError(t, err)
	statements := splitStatements(body)
	for _, model := range []any{&gbmodels.GbChannelVideoShare{}, &gbmodels.GbDeviceVideoSharePolicy{}} {
		parsed, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
		require.NoError(t, err)
		t.Run(parsed.Table, func(t *testing.T) {
			id := parsed.LookUpField("ID")
			require.NotNil(t, id)
			require.True(t, id.AutoIncrement)
			expectedType := dameng.Dialector{}.DataTypeOf(id)
			var ddl string
			for _, statement := range statements {
				if strings.Contains(statement, "CREATE TABLE "+parsed.Table+" (") {
					ddl = statement
					break
				}
			}
			require.NotEmpty(t, ddl, "migration must create the model table")
			definition := regexp.MustCompile(`(?i)\bid\s+(.+?)\s+PRIMARY KEY`).FindStringSubmatch(ddl)
			require.Len(t, definition, 2)
			require.Equal(t, expectedType, strings.TrimSpace(definition[1]), "DM ID DDL must use the native autoincrement type")
		})
	}
}
