//go:build openapi_live

package integration

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	openapiconfig "uvplatform.cn/uvp-gb28181/app/openapi/config"
	"uvplatform.cn/uvp-gb28181/app/openapi/media"
)

// This checks actual ZLM configuration readback and durable identity, NOT
// successful Hook delivery or playback qualification. All listeners are owned
// loopback fixtures; no ordinary registered media node is loaded or contacted.
func TestOpenAPINodePreflightActualReadback(t *testing.T) {
	f := newMediaProbeFixture(t)
	f.stop()
	hooks := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = io.WriteString(w, `{"code":0}`)
	}))
	defer hooks.Close()
	base := hooks.URL + "/hook"
	path := filepath.Join(f.dir, "probe.ini")
	original, err := os.ReadFile(path)
	require.NoError(t, err)
	prefix, _, ok := strings.Cut(string(original), "[hook]")
	require.True(t, ok)
	var hookConfig strings.Builder
	hookConfig.WriteString("[hook]\nenable=1\ntimeoutSec=1\nretry=1\nretry_delay=0.5\n")
	for _, event := range playauth.ManagedHookEvents() {
		capability, err := playauth.HookCapability(f.secret, "openapi-isolated-probe", event)
		require.NoError(t, err)
		query := url.Values{"node": {"openapi-isolated-probe"}, "cap": {capability}}
		fmt.Fprintf(&hookConfig, "%s=%s/%s?%s\n", event, base, event, query.Encode())
	}
	require.NoError(t, os.WriteFile(path, []byte(prefix+hookConfig.String()), 0600))
	f.start()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "runtime.sqlite")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.Exec(`CREATE TABLE meta_node (
		id INTEGER PRIMARY KEY, media_server_uuid TEXT NOT NULL, revision INTEGER NOT NULL,
		current_boot_nonce TEXT, retired_boot_history TEXT, runtime_epoch INTEGER NOT NULL DEFAULT 0,
		runtime_protocol_version INTEGER NOT NULL DEFAULT 0, runtime_confirmed_revision INTEGER NOT NULL DEFAULT 0,
		runtime_confirmed_at DATETIME, runtime_identity_status TEXT NOT NULL DEFAULT 'unknown')`).Error)
	require.NoError(t, db.Exec("INSERT INTO meta_node (id, media_server_uuid, revision) VALUES (1, 'openapi-isolated-probe', 1)").Error)
	n := node.Node{ID: 1, Revision: 1, MediaServerUUID: "openapi-isolated-probe", State: node.StateActive, APISecret: f.secret}
	settings := zlm.OpenAPIControlTLS{Endpoint: fmt.Sprintf("https://127.0.0.1:%d/index/api", f.tlsPort), Roots: f.tlsConfig.RootCAs, SPKISHA256: f.controlPin}
	probe := media.NewNodePreflight(openapiconfig.NewNodeRuntimeStore(db, nil))
	got, err := probe.Probe(context.Background(), n, settings, base)
	require.NoError(t, err)
	require.Equal(t, openapiconfig.NodeRuntimeStatusActive, got.IdentityStatus)
	require.EqualValues(t, 1, got.RuntimeEpoch)
	replayed, err := probe.Probe(context.Background(), n, settings, base)
	require.NoError(t, err)
	require.Equal(t, got.CurrentBootNonce, replayed.CurrentBootNonce)
	require.Equal(t, got.RuntimeEpoch, replayed.RuntimeEpoch)

	// A bad pin cannot keep the prior mapping marked usable, nor erase history.
	settings.SPKISHA256[0] ^= 1
	_, err = probe.Probe(context.Background(), n, settings, base)
	require.Error(t, err)
	stored, err := openapiconfig.NewNodeRuntimeStore(db, nil).Load(context.Background(), got.NodeRuntimeRef)
	require.NoError(t, err)
	require.Equal(t, openapiconfig.NodeRuntimeStatusUnknown, stored.IdentityStatus)
	require.Equal(t, got.CurrentBootNonce, stored.CurrentBootNonce)
}
