package management

import (
	"context"
	"errors"
	"strconv"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

const DefaultNodeExecutorTimeout = 3 * time.Second

// NodeOperation identifies the direction of a node operation. Read and write
// are intentionally the only operations exposed by this boundary; callers do
// not get a generic ZLM API name or parameter map to forward.
type NodeOperation string

const (
	NodeOperationRead  NodeOperation = "read"
	NodeOperationWrite NodeOperation = "write"
)

// ClientOperation is a typed operation against one node-bound ZLM client. The
// context supplied to the operation always carries the executor deadline.
type ClientOperation func(context.Context, *zlm.Client) error

// NodeAuthorizer is called after the node has been resolved and before its
// lifecycle state is considered. A denied node is represented by a stable
// 403 error and the authorizer's diagnostic text is never returned to callers.
type NodeAuthorizer func(context.Context, *node.Node, NodeOperation) error

// NodeLookup is the small read-only registry surface needed by NodeExecutor.
// node.Registry implements it; keeping this interface narrow makes the
// executor easy to test without granting it persistence or mutation access.
type NodeLookup interface {
	Get(int64) (*node.Node, bool)
}

// ClientFactory creates a client bound to the exact node snapshot selected for
// this request. Production uses zlm.NewClientForNode; tests can observe that
// each node gets its own endpoint and Secret without exposing either to the
// caller.
type ClientFactory func(*node.Node) *zlm.Client

type NodeExecutorOption func(*NodeExecutor)

// WithNodeTimeout changes the per-call upstream deadline. Non-positive values
// are ignored so a caller cannot accidentally create an unbounded operation.
func WithNodeTimeout(timeout time.Duration) NodeExecutorOption {
	return func(executor *NodeExecutor) {
		if timeout > 0 {
			executor.timeout = timeout
		}
	}
}

func WithNodeAuthorizer(authorizer NodeAuthorizer) NodeExecutorOption {
	return func(executor *NodeExecutor) { executor.authorizer = authorizer }
}

// ErrNodeForbidden is the private/common cause used by authorization
// callbacks. Its text is not sent to an HTTP client.
var ErrNodeForbidden = errors.New("node access forbidden")

type NodeExecutor struct {
	registry      NodeLookup
	clientFactory ClientFactory
	timeout       time.Duration
	authorizer    NodeAuthorizer
}

func NewNodeExecutor(registry NodeLookup, factory ClientFactory, options ...NodeExecutorOption) *NodeExecutor {
	if factory == nil {
		factory = zlm.NewClientForNode
	}
	executor := &NodeExecutor{
		registry:      registry,
		clientFactory: factory,
		timeout:       DefaultNodeExecutorTimeout,
	}
	for _, option := range options {
		if option != nil {
			option(executor)
		}
	}
	return executor
}

func (e *NodeExecutor) ExecuteRead(ctx context.Context, nodeID int64, operation ClientOperation) error {
	return e.execute(ctx, nodeID, NodeOperationRead, operation)
}

func (e *NodeExecutor) ExecuteWrite(ctx context.Context, nodeID int64, operation ClientOperation) error {
	return e.execute(ctx, nodeID, NodeOperationWrite, operation)
}

// guard resolves and authorizes a node without constructing a Client or
// touching the upstream. RuntimeReader calls it before consulting a shared
// cache; execute calls it again before a cache miss reaches ZLM.
func (e *NodeExecutor) guard(ctx context.Context, nodeID int64, operationType NodeOperation) (*node.Node, error) {
	n, err := e.resolveNode(nodeID)
	if err != nil {
		return nil, err
	}
	if operationType != NodeOperationRead && operationType != NodeOperationWrite {
		return nil, NewInternalError(nodeIDString(nodeID), "unsupported node operation")
	}
	if e.authorizer != nil {
		authorizationNode := cloneNode(n)
		authorizationNode.APISecret = ""
		if authorizeErr := e.authorizer(nonNilContext(ctx), authorizationNode, operationType); authorizeErr != nil {
			return nil, NewForbiddenError(nodeIDString(nodeID), ErrNodeForbidden)
		}
	}
	if err := validateNodeState(n); err != nil {
		return nil, err
	}
	if err := nonNilContext(ctx).Err(); err != nil {
		return nil, normalizeNodeExecutorError(err, n)
	}
	return n, nil
}

// execute resolves, authorizes and checks the lifecycle state before a
// client is constructed. This ordering is the fail-closed boundary: missing,
// forbidden, maintenance, offline and unknown-state nodes never reach ZLM.
func (e *NodeExecutor) execute(ctx context.Context, nodeID int64, operationType NodeOperation, operation ClientOperation) error {
	n, err := e.guard(ctx, nodeID, operationType)
	if err != nil {
		return err
	}
	if operation == nil {
		return NewInternalError(nodeIDString(nodeID), "node operation is not configured")
	}
	if e.registry == nil || e.clientFactory == nil {
		return NewInternalError(nodeIDString(nodeID), "node executor is not configured")
	}

	operationCtx, cancel := context.WithTimeout(nonNilContext(ctx), e.effectiveTimeout())
	defer cancel()
	client := e.clientFactory(n)
	if client == nil {
		return NewInternalError(nodeIDString(nodeID), "node client is not configured")
	}
	if err := operation(operationCtx, client); err != nil {
		return normalizeNodeExecutorError(err, n)
	}
	return nil
}

func (e *NodeExecutor) resolveNode(nodeID int64) (*node.Node, error) {
	if e == nil || e.registry == nil {
		return nil, NewInternalError(nodeIDString(nodeID), "node executor is not configured")
	}
	n, ok := e.registry.Get(nodeID)
	if !ok || n == nil {
		return nil, NewNodeNotFoundError(nodeIDString(nodeID), node.ErrNotFound)
	}
	return cloneNode(n), nil
}

func validateNodeState(n *node.Node) error {
	if n == nil {
		return NewInternalError("", "node snapshot is not configured")
	}
	nodeID := nodeIDString(n.ID)
	switch n.State {
	case node.StateActive:
		return nil
	case node.StateMaintenance:
		return NewNodeMaintenanceError(nodeID)
	case node.StateOffline:
		return NewNodeOfflineError(nodeID, ErrNodeOffline)
	default:
		return NewInternalError(nodeID, "node state is unavailable")
	}
}

func (e *NodeExecutor) effectiveTimeout() time.Duration {
	if e == nil || e.timeout <= 0 {
		return DefaultNodeExecutorTimeout
	}
	return e.timeout
}

func normalizeNodeExecutorError(err error, n *node.Node) error {
	if err == nil {
		return nil
	}
	nodeID := ""
	secret := ""
	if n != nil {
		nodeID = nodeIDString(n.ID)
		secret = n.APISecret
	}
	if managementErr, ok := AsManagementError(err); ok {
		return managementErr
	}
	var runtimeErr *zlm.RuntimeError
	if errors.As(err, &runtimeErr) && runtimeErr != nil {
		if runtimeErr.Kind == zlm.RuntimeErrorCapabilityUnsupported {
			return NewUnsupportedCapabilityError(nodeID, runtimeErr.API, err)
		}
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, ErrUpstreamTimeout):
		return NewUpstreamTimeoutError(nodeID, err)
	case errors.Is(err, zlm.ErrCapabilityUnsupported):
		return NewUnsupportedCapabilityError(nodeID, runtimeAPIName(err), err)
	case errors.Is(err, ErrNodeOffline):
		return NewNodeOfflineError(nodeID, err)
	default:
		// Keep the upstream cause for server-side errors.Is checks, but expose
		// only a stable, redacted management message. The secret is deliberately
		// not included in the user-facing text.
		_ = RedactSensitiveText(err.Error(), secret)
		return NewInternalError(nodeID, "ZLM node operation failed", err)
	}
}

func runtimeAPIName(err error) string {
	var runtimeErr *zlm.RuntimeError
	if errors.As(err, &runtimeErr) && runtimeErr != nil {
		return runtimeErr.API
	}
	return ""
}

func cloneNode(n *node.Node) *node.Node {
	if n == nil {
		return nil
	}
	copy := *n
	if n.Tags != nil {
		copy.Tags = make(map[string]string, len(n.Tags))
		for key, value := range n.Tags {
			copy.Tags[key] = value
		}
	}
	return &copy
}

func nodeIDString(nodeID int64) string {
	return strconv.FormatInt(nodeID, 10)
}

func nonNilContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
