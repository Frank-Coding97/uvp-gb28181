package gb28181

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"uvplatform.com/uvp-gb28181/app/gb28181/backendorigin"
	"uvplatform.com/uvp-gb28181/app/gb28181/play"
	"uvplatform.com/uvp-gb28181/app/gb28181/playauth"
	gbroutes "uvplatform.com/uvp-gb28181/app/gb28181/routes"
	gbzlm "uvplatform.com/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
)

// Accessed only under sipLifecycleMu; each preparer captures its generation.
var videoShareBackendOrigins *backendorigin.Registry

type videoShareMediaContextResolver interface {
	ResolvePlaybackMediaContext(string, string, string) (playauth.Binding, error)
}

func configureVideoShareBackendOrigin(service videoShareMediaContextResolver) play.BackendOriginResolver {
	if videoShareBackendOrigins != nil {
		videoShareBackendOrigins.Close()
	}
	origins := backendorigin.New(32)
	videoShareBackendOrigins = origins
	registry := zlmRegistry
	gbroutes.SetVideoShareBackendOriginObserver(origins)
	return func(ctx context.Context, result *play.Result) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
		defer cancel()
		if result == nil || result.Node == nil || result.Generation == 0 || service == nil {
			return "", backendorigin.ErrUnavailable
		}
		target, mediaNode, config, err := videoShareOriginTarget(ctx, registry, result.Node.ID, result.App, result.StreamID, result.Generation)
		if err != nil {
			return "", err
		}
		validate := func(ctx context.Context, expected backendorigin.Target) error {
			binding, err := service.ResolvePlaybackMediaContext(expected.App, expected.Stream, expected.UUID)
			if err != nil || binding.MediaGeneration != expected.MediaGeneration {
				return backendorigin.ErrUnavailable
			}
			actual, _, _, err := videoShareOriginTarget(ctx, registry, expected.NodeID, expected.App, expected.Stream, expected.MediaGeneration)
			if err != nil || actual != expected {
				return backendorigin.ErrUnavailable
			}
			binding, err = service.ResolvePlaybackMediaContext(expected.App, expected.Stream, expected.UUID)
			if err != nil || binding.MediaGeneration != expected.MediaGeneration {
				return backendorigin.ErrUnavailable
			}
			return nil
		}
		rawURL, _, err := videoShareInternalURLs(result, mediaNode, config, false)
		if err != nil {
			return "", err
		}
		return origins.Prove(ctx, target, rawURL, validate)
	}
}

func videoShareOriginTarget(ctx context.Context, registry *node.Registry, nodeID int64, app, stream string, generation uint64) (backendorigin.Target, *node.Node, node.ServerConfig, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if registry == nil {
		return backendorigin.Target{}, nil, node.ServerConfig{}, backendorigin.ErrUnavailable
	}
	n, ok := registry.Get(nodeID)
	if !ok || n == nil || n.MediaServerUUID == "" || n.State == node.StateOffline || n.RecoveryRequired {
		return backendorigin.Target{}, nil, node.ServerConfig{}, backendorigin.ErrUnavailable
	}
	rawConfig, err := gbzlm.NewClientForNode(n).GetServerConfig(ctx)
	if err != nil || rawConfig["general.mediaServerId"] != n.MediaServerUUID {
		return backendorigin.Target{}, nil, node.ServerConfig{}, backendorigin.ErrUnavailable
	}
	current, exists := registry.Get(nodeID)
	if !exists || current.Revision != n.Revision || current.Host != n.Host || current.APIPort != n.APIPort || current.APISecret != n.APISecret || current.MediaServerUUID != n.MediaServerUUID || current.State == node.StateOffline || current.RecoveryRequired {
		return backendorigin.Target{}, nil, node.ServerConfig{}, backendorigin.ErrUnavailable
	}
	// Include forwarded_ip_header and all other runtime config in the fingerprint;
	// neither credentials nor the one-time nonce are logged or exposed to viewers.
	encoded, _ := json.Marshal(struct {
		Host   string
		Port   int
		Secret string
		Config map[string]string
	}{n.Host, n.APIPort, n.APISecret, rawConfig})
	digest := sha256.Sum256(encoded)
	return backendorigin.Target{NodeID: n.ID, UUID: n.MediaServerUUID, MediaGeneration: generation, Revision: n.Revision, EndpointFingerprint: hex.EncodeToString(digest[:]), App: app, Stream: stream}, n, node.ParseServerConfig(rawConfig), nil
}

func captureVideoShareOriginStop() func() {
	if videoShareBackendOrigins == nil {
		return nil
	}
	return videoShareBackendOrigins.Close
}
