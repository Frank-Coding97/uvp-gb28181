package gb28181

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"

	gbzlm "uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	openapiplay "uvplatform.cn/uvp-gb28181/app/openapi/play"
	openapiptz "uvplatform.cn/uvp-gb28181/app/openapi/ptz"
)

var openAPIPTZRoot = openapiptz.NewRuntimeRoot()
var openAPIPlayRoot = openapiplay.NewRuntimeRoot()

func OpenAPIPTZRuntime() *openapiptz.RuntimeRoot   { return openAPIPTZRoot }
func OpenAPIPlayRuntime() *openapiplay.RuntimeRoot { return openAPIPlayRoot }

type openAPIHookRuntimeIdentityResolver struct {
	registry *node.Registry
	mu       sync.Mutex
	nonces   map[int64]string
}

type openAPIViewerNodeResolver interface {
	GetByUUID(string) (*node.Node, bool)
}

type openAPIViewerRuntimeClient interface {
	GetRuntimeMediaPlayers(context.Context, gbzlm.StreamTarget) (gbzlm.RuntimePlayers, error)
	GetMediaPlayerList(context.Context, string, string, string, string) ([]gbzlm.MediaPlayer, error)
}

type openAPIViewerRuntimeSnapshotter struct {
	registry  openAPIViewerNodeResolver
	newClient func(*node.Node) openAPIViewerRuntimeClient
}

func (r *openAPIViewerRuntimeSnapshotter) SnapshotViewerRuntime(ctx context.Context, target openapiplay.ViewerRuntimeTarget) (openapiplay.ViewerRuntimeSnapshot, error) {
	if r == nil || r.registry == nil || ctx == nil || target.NodeUUID == "" {
		return openapiplay.ViewerRuntimeSnapshot{}, gbzlm.ErrRuntimeControlUnavailable
	}
	mediaNode, ok := r.registry.GetByUUID(target.NodeUUID)
	if !ok || mediaNode == nil || mediaNode.MediaServerUUID != target.NodeUUID {
		return openapiplay.ViewerRuntimeSnapshot{}, gbzlm.ErrRuntimeControlUnavailable
	}
	newClient := r.newClient
	if newClient == nil {
		newClient = func(n *node.Node) openAPIViewerRuntimeClient { return gbzlm.NewClientForNode(n) }
	}
	client := newClient(mediaNode)
	if client == nil {
		return openapiplay.ViewerRuntimeSnapshot{}, gbzlm.ErrRuntimeControlUnavailable
	}
	players, err := client.GetRuntimeMediaPlayers(ctx, gbzlm.StreamTarget{
		Schema: target.Schema, VHost: target.VHost, App: target.App, Stream: target.Stream,
	})
	if err != nil {
		standard, standardErr := client.GetMediaPlayerList(ctx, target.Schema, target.VHost, target.App, target.Stream)
		if standardErr != nil && !errors.Is(standardErr, gbzlm.ErrMediaNotFound) {
			return openapiplay.ViewerRuntimeSnapshot{}, gbzlm.ErrRuntimeControlUnavailable
		}
		identifiers := make(map[string]struct{}, len(standard))
		for _, player := range standard {
			identifiers[player.Identifier] = struct{}{}
		}
		return openapiplay.ViewerRuntimeSnapshot{TargetAuthoritative: true, Identifiers: identifiers}, nil
	}
	identifiers := make(map[string]struct{}, len(players.Players))
	for _, player := range players.Players {
		identifiers[player.Identifier] = struct{}{}
	}
	return openapiplay.ViewerRuntimeSnapshot{BootNonce: players.BootNonce, Identifiers: identifiers}, nil
}

func (r *openAPIHookRuntimeIdentityResolver) ResolveOpenAPIRuntimeIdentity(ctx context.Context, mediaServerID string) (string, error) {
	if r.registry == nil || ctx == nil || mediaServerID == "" {
		return "", gbzlm.ErrRuntimeControlUnavailable
	}
	mediaNode, ok := r.registry.GetByUUID(mediaServerID)
	if !ok || mediaNode == nil || mediaNode.MediaServerUUID != mediaServerID {
		return "", gbzlm.ErrRuntimeControlUnavailable
	}
	identity, err := gbzlm.NewClientForNode(mediaNode).GetRuntimeIdentity(ctx)
	if err == nil && identity.BootNonce != "" {
		return identity.BootNonce, nil
	}
	return r.nonceFor(mediaNode.ID)
}

func (r *openAPIHookRuntimeIdentityResolver) nonceFor(nodeID int64) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.nonces == nil {
		r.nonces = make(map[int64]string)
	}
	if nonce := r.nonces[nodeID]; nonce != "" {
		return nonce, nil
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	nonce := hex.EncodeToString(raw[:])
	r.nonces[nodeID] = nonce
	return nonce, nil
}

func (r *openAPIHookRuntimeIdentityResolver) OnNodeStarted(nodeID int64) {
	if r == nil || nodeID <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.nonces == nil {
		r.nonces = make(map[int64]string)
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err == nil {
		r.nonces[nodeID] = hex.EncodeToString(raw[:])
	}
}
