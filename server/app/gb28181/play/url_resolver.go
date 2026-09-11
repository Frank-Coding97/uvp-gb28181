package play

import (
	"context"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playurl"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// ServerConfigProvider returns the media ports exposed by one ZLM node.
type ServerConfigProvider interface {
	Refresh(context.Context, int64) (node.ServerConfig, error)
}

type PlaybackURLs = playurl.URLs
type PlaybackSelection = playurl.Selection

// URLResolver builds browser and diagnostic playback URLs from ZLM runtime config.
type URLResolver struct {
	configs ServerConfigProvider
}

func NewURLResolver(configs ServerConfigProvider) *URLResolver {
	return &URLResolver{configs: configs}
}

func (r *URLResolver) Resolve(ctx context.Context, mediaNode *node.Node, app, stream string) (PlaybackURLs, []string) {
	if r == nil || r.configs == nil || mediaNode == nil {
		return PlaybackURLs{}, []string{"播放地址解析器未配置"}
	}
	cfg, err := r.configs.Refresh(ctx, mediaNode.ID)
	if err != nil {
		return PlaybackURLs{}, []string{"读取节点媒体端口失败"}
	}

	return BuildPlaybackURLs(mediaNode.EffectivePlaybackHost(), cfg, app, stream), nil
}

// BuildPlaybackURLs builds only the protocol URLs exposed by a ZLM node.
func BuildPlaybackURLs(playbackHost string, cfg node.ServerConfig, app, stream string) PlaybackURLs {
	return playurl.Build(playbackHost, cfg, app, stream)
}

func PlaybackURLsFromMap(values map[string]string) PlaybackURLs {
	return playurl.FromMap(values)
}

// SelectPlaybackSource selects a browser-safe URL using a stable logical
// fallback order. HTTPS callers never receive an insecure media URL.
func SelectPlaybackSource(urls PlaybackURLs, preferred string, secure bool) PlaybackSelection {
	return playurl.Select(urls, preferred, secure)
}

func stringPtr(value string) *string { return &value }
