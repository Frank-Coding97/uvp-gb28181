package contracts

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPublicActionsHaveShortExplicitRoutesAndTargetParameters(t *testing.T) {
	operations := Operations()
	require.GreaterOrEqual(t, len(operations), 99)
	require.NoError(t, Validate(operations))
	for _, operation := range operations {
		require.NotContains(t, operation.ExternalPath, "{")
		require.NotContains(t, operation.GinPath, ":")
		require.Contains(t, []string{"GET", "POST"}, operation.Method)
		require.LessOrEqual(t, len(strings.Split(strings.TrimPrefix(operation.ExternalPath, "/openapi/v1/"), "/")), 4)
	}
	for _, operation := range operations {
		if operation.Scope == "channel:detail" {
			require.Equal(t, "/openapi/v1/channel/info", operation.ExternalPath)
		}
		if operation.Scope == "play:live:stop" {
			require.Equal(t, "POST", operation.Method)
			require.Equal(t, "/openapi/v1/live/stop", operation.ExternalPath)
		}
	}
}
