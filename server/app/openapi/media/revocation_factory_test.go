package media

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/openapi/config"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

type revocationRegistryFunc func(string) (*node.Node, bool)

func (f revocationRegistryFunc) GetByUUID(uuid string) (*node.Node, bool) { return f(uuid) }

type revocationBindingFunc func(context.Context, string) (NodeControlBinding, error)

func (f revocationBindingFunc) Lookup(ctx context.Context, uuid string) (NodeControlBinding, error) {
	return f(ctx, uuid)
}

type trustedFactoryFixture struct {
	*revocationWorkerFixture
	factory  *TrustedRevocationFactory
	binding  NodeControlBinding
	n        node.Node
	store    *config.NodeRuntimeStore
	mu       sync.Mutex
	requests []string
	boot     string
	caPEM    string
	onConfig func()
}

func newTrustedFactoryFixture(t *testing.T) *trustedFactoryFixture {
	w := newRevocationWorkerFixture(t, time.Second)
	require.NoError(t, w.db.Exec(nodeAuthorityTableSQL).Error)
	require.NoError(t, w.db.Exec("ALTER TABLE meta_node ADD COLUMN retired_boot_history TEXT DEFAULT '[]'").Error)
	values := validNodeAuthorityValues()
	values["media_server_uuid"] = "node-a"
	insertNodeAuthorityRow(t, w.db, values)
	f := &trustedFactoryFixture{revocationWorkerFixture: w, boot: w.boot}
	f.n = node.Node{ID: 1, Revision: 9, MediaServerUUID: "node-a", State: node.StateActive, APISecret: "factory-fixture-secret"}
	f.binding = NodeControlBinding{NodeID: 1, NodeUUID: "node-a", BindingRevision: 1, Enabled: true, HookBase: "https://hooks.example.test/hook"}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/index/api/")
		f.mu.Lock()
		f.requests = append(f.requests, path)
		boot, onConfig := f.boot, f.onConfig
		f.mu.Unlock()
		require.Equal(t, "factory-fixture-secret", r.Header.Get("secret"))
		w.Header().Set("Cache-Control", "no-store")
		var body any
		switch path {
		case "getRuntimeIdentity":
			body = map[string]any{"code": 0, "data": map[string]any{"protocolVersion": 1, "bootNonce": boot}}
		case "getServerConfig":
			if onConfig != nil {
				onConfig()
			}
			cfg := map[string]string{"general.mediaServerId": "node-a", "hook.enable": "1", "general.flowThreshold": "0", "api.apiDebug": "0", "hook.timeoutSec": "1", "hook.retry": "0", "hook.retry_delay": "0"}
			for _, event := range playauth.ManagedHookEvents() {
				cap, err := playauth.HookCapability("factory-fixture-secret", "node-a", event)
				require.NoError(t, err)
				cfg["hook."+string(event)] = "https://hooks.example.test/hook/" + string(event) + "?" + url.Values{"node": {"node-a"}, "cap": {cap}}.Encode()
			}
			body = map[string]any{"code": 0, "data": []any{cfg}}
		case "getAllSession", "getMediaPlayerList":
			body = map[string]any{"code": 0, "bootNonce": boot, "data": []any{}}
		default:
			t.Errorf("unexpected control request %s", path)
			w.WriteHeader(404)
			return
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(server.Close)
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	f.caPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	f.binding.TLS = zlm.OpenAPIControlTLS{Endpoint: server.URL + "/index/api", Roots: roots, SPKISHA256: sha256.Sum256(server.Certificate().RawSubjectPublicKeyInfo)}
	f.store = config.NewNodeRuntimeStore(w.db, w.now)
	f.factory = NewTrustedRevocationFactory(revocationRegistryFunc(func(uuid string) (*node.Node, bool) {
		f.mu.Lock()
		defer f.mu.Unlock()
		copy := f.n
		return &copy, uuid == copy.MediaServerUUID
	}), revocationBindingFunc(func(ctx context.Context, uuid string) (NodeControlBinding, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.binding, nil
	}), f.store)
	return f
}

func TestTrustedRevocationFactoryFreshTLSAndDurableIdentity(t *testing.T) {
	f := newTrustedFactoryFixture(t)
	runtime, err := f.factory.Resolve(context.Background(), "node-a")
	require.NoError(t, err)
	require.True(t, runtime.Trusted)
	require.Equal(t, f.boot, runtime.CurrentBootNonce)
	require.Equal(t, time.Second, runtime.HookBudget)
	require.NotNil(t, runtime.Release)
	defer runtime.Release()
	_, err = runtime.Control.GetRuntimeSessions(context.Background())
	require.NoError(t, err)
	snapshot, err := f.store.Load(context.Background(), config.NodeRuntimeRef{NodeID: 1, NodeUUID: "node-a", NodeRevision: 9})
	require.NoError(t, err)
	require.Equal(t, runtime.CurrentBootNonce, snapshot.CurrentBootNonce)
	require.Equal(t, config.NodeRuntimeStatusActive, snapshot.IdentityStatus)
	f.mu.Lock()
	defer f.mu.Unlock()
	require.Equal(t, []string{"getRuntimeIdentity", "getServerConfig", "getAllSession", "getRuntimeIdentity", "getAllSession"}, f.requests)
}

func TestTrustedRevocationFactoryRejectsUntrustedInputs(t *testing.T) {
	for _, kind := range []string{"missing", "disabled", "binding-revision", "node-id", "node-uuid", "offline", "recovery", "pin", "wrong-pin", "ca", "http", "hook-http", "ca-mode"} {
		t.Run(kind, func(t *testing.T) {
			f := newTrustedFactoryFixture(t)
			switch kind {
			case "missing":
				f.factory.bindings = nil
			case "disabled":
				f.binding.Enabled = false
			case "binding-revision":
				f.binding.BindingRevision = 0
			case "node-id":
				f.binding.NodeID = 2
			case "node-uuid":
				f.binding.NodeUUID = "node-b"
			case "offline":
				f.n.State = node.StateOffline
			case "recovery":
				f.n.RecoveryRequired = true
			case "pin":
				f.binding.TLS.SPKISHA256 = [32]byte{}
			case "wrong-pin":
				f.binding.TLS.SPKISHA256[0] ^= 1
			case "ca":
				f.binding.TLS.Roots = x509.NewCertPool()
			case "http":
				f.binding.TLS.Endpoint = strings.Replace(f.binding.TLS.Endpoint, "https:", "http:", 1)
			case "hook-http":
				f.binding.HookBase = "http://hooks.example.test/hook"
			case "ca-mode":
				f.binding.UseSystemRoots = true
			}
			runtime, err := f.factory.Resolve(context.Background(), "node-a")
			require.ErrorIs(t, err, config.ErrNodeRuntimeUnavailable)
			require.False(t, runtime.Trusted)
			require.Nil(t, runtime.Control)
			require.Nil(t, runtime.Release)
		})
	}
}

func TestRevocationWorkerReleasesResolvedControlOnAllPaths(t *testing.T) {
	for _, path := range []string{"success", "resolver-error", "untrusted", "boot", "budget", "network"} {
		t.Run(path, func(t *testing.T) {
			f := newRevocationWorkerFixture(t, time.Second)
			f.seed(t, 1, models.ViewerStateRevokePending)
			released := 0
			f.factory.runtime.Release = func() { released++ }
			switch path {
			case "resolver-error":
				f.factory.err = context.DeadlineExceeded
			case "untrusted":
				f.factory.runtime.Trusted = false
			case "boot":
				f.factory.runtime.CurrentBootNonce = strings.Repeat("f", 32)
			case "budget":
				f.factory.runtime.HookBudget = 0
			case "network":
				f.control.playersErr = context.DeadlineExceeded
			}
			_, err := f.worker(1).Tick(context.Background())
			require.NoError(t, err)
			require.Equal(t, 1, released)
		})
	}
}

func TestTrustedRevocationFactoryDiscardsChangedProbe(t *testing.T) {
	for _, change := range []string{"node-revision", "binding-revision", "endpoint-without-revision", "roots-without-revision", "offline", "db-revision"} {
		t.Run(change, func(t *testing.T) {
			f := newTrustedFactoryFixture(t)
			f.onConfig = func() {
				// A real query succeeds while the network request is outstanding:
				// the coordinator must not hold its SQL transaction over I/O.
				snapshot, err := f.store.Load(context.Background(), config.NodeRuntimeRef{NodeID: 1, NodeUUID: "node-a", NodeRevision: 9})
				require.NoError(t, err)
				require.Equal(t, config.NodeRuntimeStatusUnknown, snapshot.IdentityStatus)
				f.mu.Lock()
				defer f.mu.Unlock()
				switch change {
				case "node-revision":
					f.n.Revision++
				case "binding-revision":
					f.binding.BindingRevision++
				case "endpoint-without-revision":
					f.binding.TLS.Endpoint += "/different"
				case "roots-without-revision":
					f.binding.TLS.Roots = x509.NewCertPool()
				case "offline":
					f.n.State = node.StateOffline
				case "db-revision":
					require.NoError(t, f.db.Table("meta_node").Where("id = ?", 1).Update("revision", 10).Error)
				}
			}
			runtime, err := f.factory.Resolve(context.Background(), "node-a")
			require.ErrorIs(t, err, config.ErrNodeRuntimeUnavailable)
			require.Nil(t, runtime.Control)
			f.mu.Lock()
			require.NotContains(t, f.requests, "kick_session_if_match")
			f.mu.Unlock()
		})
	}
}

func TestTrustedRevocationFactoryRechecksDurableStateAfterProbe(t *testing.T) {
	for _, column := range []string{"runtime_identity_status", "current_boot_nonce"} {
		t.Run(column, func(t *testing.T) {
			f := newTrustedFactoryFixture(t)
			provider := f.factory.bindings
			reads := 0
			f.factory.bindings = revocationBindingFunc(func(ctx context.Context, uuid string) (NodeControlBinding, error) {
				reads++
				if reads == 3 { // Final recheck, after coordinator's durable commit.
					value := "unknown"
					if column == "current_boot_nonce" {
						value = strings.Repeat("f", 32)
					}
					require.NoError(t, f.db.Table("meta_node").Where("id = ?", 1).Update(column, value).Error)
				}
				return provider.Lookup(ctx, uuid)
			})
			runtime, err := f.factory.Resolve(context.Background(), "node-a")
			require.ErrorIs(t, err, config.ErrNodeRuntimeUnavailable)
			require.Nil(t, runtime.Control)
		})
	}
}

func TestTrustedRevocationFactoryRefreshesLifecycleRevisionAndRetainsOldViewer(t *testing.T) {
	f := newTrustedFactoryFixture(t)
	viewer := f.seed(t, 1, models.ViewerStateRevokePending)
	runtime, err := f.factory.Resolve(context.Background(), "node-a")
	require.NoError(t, err)
	runtime.Release()
	// A later active snapshot may carry a new lifecycle revision without
	// changing operator-controlled TLS binding revision 1.
	f.mu.Lock()
	f.n.Revision = 10
	f.boot = strings.Repeat("f", 32)
	f.mu.Unlock()
	require.NoError(t, f.db.Table("meta_node").Where("id = ?", 1).Update("revision", 10).Error)
	worker := NewRevocationWorker(f.db, f.factory, f.now, 1)
	result, err := worker.Tick(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, result.Pending)
	require.Zero(t, result.Closed)
	row := f.loadViewer(t, viewer.ID)
	require.Equal(t, RevocationErrorRuntimeMismatch, row.LastErrorClass)
	snapshot, err := f.store.Load(context.Background(), config.NodeRuntimeRef{NodeID: 1, NodeUUID: "node-a", NodeRevision: 10})
	require.NoError(t, err)
	require.Equal(t, []string{f.revocationWorkerFixture.boot}, snapshot.RetiredBootHistory)
	require.Equal(t, int64(2), snapshot.RuntimeEpoch)
	f.mu.Lock()
	f.boot = f.revocationWorkerFixture.boot
	f.mu.Unlock()
	runtime, err = f.factory.Resolve(context.Background(), "node-a")
	require.ErrorIs(t, err, config.ErrNodeRuntimeUnavailable, "a retired boot cannot become current again")
	require.Nil(t, runtime.Control)
}

func TestTrustedRevocationFactoryWorkerRequiresRepeatedFreshAbsence(t *testing.T) {
	f := newTrustedFactoryFixture(t)
	viewer := f.seed(t, 1, models.ViewerStateRevokePending)
	worker := NewRevocationWorker(f.db, f.factory, f.now, 1)
	first, err := worker.Tick(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, first.Pending)
	require.Zero(t, first.Closed)
	require.Equal(t, RevocationErrorAwaitingLateSession, f.loadViewer(t, viewer.ID).LastErrorClass)
	f.clock = f.clock.Add(2 * time.Second)
	second, err := worker.Tick(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, second.Closed)
	row := f.loadViewer(t, viewer.ID)
	require.Equal(t, models.ViewerStateClosed, row.State)
	require.Equal(t, RevocationErrorAlreadyGone, row.LastErrorClass)
}
