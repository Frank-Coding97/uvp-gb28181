package launcher

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMaintenanceChildOutputRequiresFinalIdentity(t *testing.T) {
	valid := `{"status":"maintenance_complete","purpose":"db_check","version":"2.0.0"}`
	require.NoError(t, validateMaintenanceChildOutput([]byte("{}\n"+valid), "db_check", "2.0.0"))
	for _, raw := range []string{"", "{}", valid + "\n{}", valid + "\nnull", valid + "broken"} {
		require.Error(t, validateMaintenanceChildOutput([]byte(raw), "db_check", "2.0.0"))
	}
	require.Error(t, validateMaintenanceChildOutput([]byte(valid), "candidate_health", "2.0.0"))
	require.Error(t, validateMaintenanceChildOutput([]byte(valid), "db_check", "3.0.0"))
}

func TestMaintenanceChildOutputDrainsWithoutUnboundedCapture(t *testing.T) {
	var output maintenanceChildOutput
	input := strings.Repeat("x", (1<<20)+4096)
	n, err := io.Copy(&output, io.LimitReader(strings.NewReader(input), int64(len(input))))
	require.NoError(t, err)
	require.Equal(t, int64(len(input)), n)
	require.True(t, output.overflow)
	require.Len(t, output.Bytes(), 1<<20)
}
