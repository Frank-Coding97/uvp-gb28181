package media

import (
	"context"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

func TestConfiguredRevocationRequiresExplicitConfigAndDependencies(t *testing.T) {
	stop, err := StartConfiguredRevocation(context.Background(), nil, nil, "", nil)
	require.ErrorIs(t, err, ErrRevocationNotConfigured)
	require.Nil(t, stop)
	path := writeBindingFile(t, bindingFileFixture(t))
	stop, err = StartConfiguredRevocation(context.Background(), nil, nil, path, nil)
	require.ErrorIs(t, err, ErrRevocationWorkerUnavailable)
	require.Nil(t, stop)
}

func TestConfiguredRevocationRunsWithGatewayAbsentAndStopJoins(t *testing.T) {
	f := newTrustedFactoryFixture(t)
	viewer := f.seed(t, 1, models.ViewerStateRevokePending)
	path := writeBindingFile(t, nodeControlFile{Version: 1, Nodes: []nodeControlEntry{{NodeID: f.n.ID, NodeUUID: f.n.MediaServerUUID,
		BindingRevision: 1, Enabled: true, Endpoint: f.binding.TLS.Endpoint, CAMode: "private", CAPEM: f.caPEM,
		SPKISHA256: hex.EncodeToString(f.binding.TLS.SPKISHA256[:]), HookBase: f.binding.HookBase}}})
	reports := make(chan RevocationTickResult, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop, err := StartConfiguredRevocation(ctx, f.db, f.factory.registry, path, func(result RevocationTickResult, err error) {
		require.NoError(t, err)
		reports <- result
	})
	require.NoError(t, err)
	require.NotNil(t, stop)
	defer stop()
	require.NoError(t, os.WriteFile(path, []byte("invalid after startup"), 0600))
	select {
	case result := <-reports:
		require.Equal(t, 1, result.Pending)
		require.Zero(t, result.Closed)
	case <-time.After(4 * time.Second):
		t.Fatal("configured runner never processed the durable pending viewer")
	}
	stop()
	stop()
	require.Equal(t, RevocationErrorAwaitingLateSession, f.loadViewer(t, viewer.ID).LastErrorClass)
}

func TestConfiguredRevocationRejectsMissingSchemaAndTypedNilRegistry(t *testing.T) {
	for _, missing := range []string{"grant", "viewer", "node-field", "nil-registry"} {
		t.Run(missing, func(t *testing.T) {
			f := newTrustedFactoryFixture(t)
			registry := f.factory.registry
			switch missing {
			case "grant":
				require.NoError(t, f.db.Migrator().DropTable(&models.PlayGrant{}))
			case "viewer":
				require.NoError(t, f.db.Migrator().DropTable(&models.Viewer{}))
			case "node-field":
				require.NoError(t, f.db.Exec("ALTER TABLE meta_node DROP COLUMN revision").Error)
			case "nil-registry":
				var absent *node.Registry
				registry = absent
			}
			stop, err := StartConfiguredRevocation(context.Background(), f.db, registry, writeBindingFile(t, bindingFileFixture(t)), nil)
			require.ErrorIs(t, err, ErrRevocationWorkerUnavailable)
			require.Nil(t, stop)
			f.mu.Lock()
			defer f.mu.Unlock()
			require.Empty(t, f.requests)
		})
	}
}
