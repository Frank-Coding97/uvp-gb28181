package management

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type executorMemoryRepo struct {
	mu     sync.Mutex
	nextID int64
	rows   map[int64]node.Node
}

func newExecutorMemoryRepo() *executorMemoryRepo {
	return &executorMemoryRepo{rows: make(map[int64]node.Node)}
}

func (r *executorMemoryRepo) List(_ context.Context) ([]node.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rows := make([]node.Node, 0, len(r.rows))
	for _, item := range r.rows {
		rows = append(rows, item)
	}
	return rows, nil
}

func (r *executorMemoryRepo) Get(_ context.Context, id int64) (*node.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.rows[id]
	if !ok {
		return nil, nil
	}
	return &item, nil
}

func (r *executorMemoryRepo) Create(_ context.Context, item node.Node) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	item.ID = r.nextID
	r.rows[item.ID] = item
	return item.ID, nil
}

func (r *executorMemoryRepo) Update(_ context.Context, item node.Node) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows[item.ID] = item
	return nil
}

func (r *executorMemoryRepo) Delete(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.rows, id)
	return nil
}

func addExecutorNode(t *testing.T, registry *node.Registry, item node.Node) *node.Node {
	t.Helper()
	added, err := registry.Add(context.Background(), item)
	require.NoError(t, err)
	return added
}

func TestNodeExecutorReadWriteFailClosedByNodeStateAndAccess(t *testing.T) {
	registry := node.NewRegistry(newExecutorMemoryRepo())
	maintenance := addExecutorNode(t, registry, node.Node{Name: "maintenance", State: node.StateMaintenance})
	offline := addExecutorNode(t, registry, node.Node{Name: "offline", State: node.StateOffline})
	denied := addExecutorNode(t, registry, node.Node{Name: "denied", State: node.StateActive})
	active := addExecutorNode(t, registry, node.Node{Name: "active", State: node.StateActive})

	var factoryCalls atomic.Int32
	executor := NewNodeExecutor(registry, func(current *node.Node) *zlm.Client {
		factoryCalls.Add(1)
		return zlm.NewClientForNode(current)
	}, WithNodeAuthorizer(func(_ context.Context, current *node.Node, _ NodeOperation) error {
		if current.ID == denied.ID {
			return ErrNodeForbidden
		}
		return nil
	}))

	tests := []struct {
		name       string
		nodeID     int64
		wantStatus int
		wantCode   ManagementErrorCode
	}{
		{name: "missing read", nodeID: 9999, wantStatus: http.StatusNotFound, wantCode: CodeNodeNotFound},
		{name: "missing write", nodeID: 9999, wantStatus: http.StatusNotFound, wantCode: CodeNodeNotFound},
		{name: "forbidden read", nodeID: denied.ID, wantStatus: http.StatusForbidden, wantCode: CodeForbidden},
		{name: "maintenance read", nodeID: maintenance.ID, wantStatus: http.StatusConflict, wantCode: CodeNodeMaintenance},
		{name: "maintenance write", nodeID: maintenance.ID, wantStatus: http.StatusConflict, wantCode: CodeNodeMaintenance},
		{name: "offline read", nodeID: offline.ID, wantStatus: http.StatusServiceUnavailable, wantCode: CodeNodeOffline},
		{name: "offline write", nodeID: offline.ID, wantStatus: http.StatusServiceUnavailable, wantCode: CodeNodeOffline},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.name[len(tt.name)-4:] == "read" {
				err = executor.ExecuteRead(context.Background(), tt.nodeID, func(context.Context, *zlm.Client) error {
					t.Fatal("rejected read must not invoke client")
					return nil
				})
			} else {
				err = executor.ExecuteWrite(context.Background(), tt.nodeID, func(context.Context, *zlm.Client) error {
					t.Fatal("rejected write must not invoke client")
					return nil
				})
			}
			got, ok := AsManagementError(err)
			require.True(t, ok)
			require.Equal(t, tt.wantCode, got.Code)
			require.Equal(t, tt.wantStatus, got.HTTPStatus())
		})
	}

	require.NoError(t, executor.ExecuteRead(context.Background(), active.ID, func(_ context.Context, client *zlm.Client) error {
		require.NotNil(t, client, "factory result is observable only after state/access checks")
		return nil
	}))
	require.Equal(t, int32(1), factoryCalls.Load())
}

func TestNodeExecutorUsesCurrentNodeClientAndSecret(t *testing.T) {
	var seenMu sync.Mutex
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenMu.Lock()
		seen = append(seen, r.URL.Query().Get("secret"))
		seenMu.Unlock()
		_, _ = w.Write([]byte(`{"code":0,"data":{"MediaSource":1}}`))
	}))
	defer server.Close()

	parsed, err := url.Parse(server.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(parsed.Port())
	require.NoError(t, err)
	registry := node.NewRegistry(newExecutorMemoryRepo())
	first := addExecutorNode(t, registry, node.Node{Host: parsed.Hostname(), APIPort: port, APISecret: "secret-a", State: node.StateActive})
	second := addExecutorNode(t, registry, node.Node{Host: parsed.Hostname(), APIPort: port, APISecret: "secret-b", State: node.StateActive})
	executor := NewNodeExecutor(registry, nil)

	for _, id := range []int64{first.ID, second.ID} {
		err := executor.ExecuteRead(context.Background(), id, func(ctx context.Context, client *zlm.Client) error {
			_, err := client.GetStatistic(ctx)
			return err
		})
		require.NoError(t, err)
	}

	seenMu.Lock()
	defer seenMu.Unlock()
	require.Equal(t, []string{"secret-a", "secret-b"}, seen)
}

func TestNodeExecutorDeadlineMapsToGatewayTimeout(t *testing.T) {
	registry := node.NewRegistry(newExecutorMemoryRepo())
	active := addExecutorNode(t, registry, node.Node{State: node.StateActive})
	executor := NewNodeExecutor(registry, nil, WithNodeTimeout(25*time.Millisecond))

	started := time.Now()
	err := executor.ExecuteRead(context.Background(), active.ID, func(ctx context.Context, _ *zlm.Client) error {
		<-ctx.Done()
		return ctx.Err()
	})
	require.Less(t, time.Since(started), time.Second)
	got, ok := AsManagementError(err)
	require.True(t, ok)
	require.Equal(t, CodeUpstreamTimeout, got.Code)
	require.Equal(t, http.StatusGatewayTimeout, got.HTTPStatus())
	require.True(t, got.Retryable)
}

func TestNodeExecutorUnknownStateFailsClosed(t *testing.T) {
	registry := node.NewRegistry(newExecutorMemoryRepo())
	unknown := addExecutorNode(t, registry, node.Node{State: node.State("unknown")})
	executor := NewNodeExecutor(registry, nil)

	err := executor.ExecuteWrite(context.Background(), unknown.ID, func(context.Context, *zlm.Client) error {
		t.Fatal("unknown state must not invoke client")
		return nil
	})
	got, ok := AsManagementError(err)
	require.True(t, ok)
	require.Equal(t, CodeInternal, got.Code)
}
