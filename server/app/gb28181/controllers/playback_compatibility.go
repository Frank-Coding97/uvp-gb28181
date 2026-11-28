package controllers

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	gbconfig "uvplatform.com/uvp-gb28181/app/gb28181/config"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/logcleanup"
)

// PlaybackCompatibility serializes node admission changes with playback policy
// saves. It is shared by both controllers; browser state is never authoritative.
type PlaybackCompatibility struct {
	mu    sync.Mutex
	nodes interface{ List() []*node.Node }
}

func NewPlaybackCompatibility(nodes interface{ List() []*node.Node }) *PlaybackCompatibility {
	return &PlaybackCompatibility{nodes: nodes}
}

func (p *PlaybackCompatibility) Lock() func() { p.mu.Lock(); return p.mu.Unlock }

func (p *PlaybackCompatibility) singlePortNodes() []gbconfig.SinglePortNode {
	var result []gbconfig.SinglePortNode
	for _, n := range p.nodes.List() {
		if n.IsEnabled() && n.EffectiveRTPReceiveMode() == "single" {
			result = append(result, gbconfig.SinglePortNode{ID: n.ID, Name: n.Name})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

// Settings is called with Lock held by HTTP handlers. Reconciliation persists
// historical conflicts at startup; the effective view also stays safe if disk
// recovery is required.
func (p *PlaybackCompatibility) Settings() gbconfig.FixedAddressPlaybackSettings {
	settings := gbconfig.CurrentFixedAddressPlaybackSettings()
	settings.SinglePortNodes = p.singlePortNodes()
	if len(settings.SinglePortNodes) > 0 {
		settings.FixedAddressEnabled = false
		settings.AutoOnDemandEnabled = false
	}
	return settings
}

func (p *PlaybackCompatibility) Validate(settings gbconfig.FixedAddressPlaybackSettings) error {
	if !settings.FixedAddressEnabled && !settings.AutoOnDemandEnabled {
		return nil
	}
	nodes := p.singlePortNodes()
	if len(nodes) == 0 {
		return nil
	}
	names := make([]string, len(nodes))
	for i, n := range nodes {
		names[i] = n.Name
	}
	return fmt.Errorf("已启用的媒体节点使用单端口收流（%s），不能开启固定播放地址及自动点播；请先将这些节点改为多端口或禁用", strings.Join(names, "、"))
}

func savePlaybackSettings(settings gbconfig.FixedAddressPlaybackSettings) error {
	unlock := logcleanup.LockConfig()
	defer unlock()
	return gbconfig.SaveFixedAddressPlaybackSettings(app.ConfigYml, settings)
}

// Reconcile handles installations that already persisted the incompatible pair.
func (p *PlaybackCompatibility) Reconcile() error {
	unlock := p.Lock()
	defer unlock()
	if len(p.singlePortNodes()) == 0 {
		return nil
	}
	current := gbconfig.CurrentFixedAddressPlaybackSettings()
	if !current.FixedAddressEnabled && !current.AutoOnDemandEnabled {
		return nil
	}
	if err := savePlaybackSettings(gbconfig.FixedAddressPlaybackSettings{}); err != nil {
		gbconfig.UseDynamicPlaybackUntilConfigRepair()
		return fmt.Errorf("固定播放地址及自动点播已在运行时关闭，但未能写入配置文件: %w", err)
	}
	return nil
}

// WithNodeChange requires Lock to be held before any per-node lock, including
// while building the candidate, so creation/convergence cannot invert lock order.
func (p *PlaybackCompatibility) WithNodeChange(candidate *node.Node, apply func() error) (bool, error) {
	previous := gbconfig.CurrentFixedAddressPlaybackSettings()
	disable := candidate.IsEnabled() && candidate.EffectiveRTPReceiveMode() == "single" && (previous.FixedAddressEnabled || previous.AutoOnDemandEnabled)
	if disable {
		if err := savePlaybackSettings(gbconfig.FixedAddressPlaybackSettings{}); err != nil {
			return false, fmt.Errorf("无法关闭固定播放地址及自动点播，节点配置未变更: %w", err)
		}
	}
	if err := apply(); err != nil {
		if disable {
			// A failed node rollback can leave an enabled single-port node persisted.
			// Never restore an incompatible policy in that case.
			if len(p.singlePortNodes()) > 0 {
				return false, fmt.Errorf("%w；仍存在已启用单端口节点，固定播放地址及自动点播保持关闭，请核对节点配置", err)
			}
			if rollbackErr := savePlaybackSettings(previous); rollbackErr != nil {
				// The helper restores its in-memory previous (disabled) pair on failure.
				return false, fmt.Errorf("%w；恢复固定播放地址及自动点播失败，当前保持关闭: %v", err, rollbackErr)
			}
		}
		return false, err
	}
	return disable, nil
}
