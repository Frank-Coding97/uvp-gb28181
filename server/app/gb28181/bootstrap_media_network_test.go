package gb28181

import (
	"testing"

	gbconfig "uvplatform.com/uvp-gb28181/app/gb28181/config"
)

// 媒体地址的运行时兜底：配置里的值优先，留空时依次落到「按当前网卡推断出的地址」
// →「YAML 里的旧地址」。留空是正常的编辑结果（向导里选真实网卡就能不填）。
func TestFirstNonEmptyKeepsConfiguredValueAndFallsBack(t *testing.T) {
	if got := firstNonEmpty("stream.example.com", "yaml.example.com"); got != "stream.example.com" {
		t.Fatalf("configured stream host = %q", got)
	}
	if got := firstNonEmpty("", "yaml.example.com"); got != "yaml.example.com" {
		t.Fatalf("empty configured stream host should use fallback, got %q", got)
	}
	if got := firstNonEmpty("  ", "  "); got != "" {
		t.Fatalf("blank hosts should remain empty, got %q", got)
	}
	// 三档：配置 → 按网卡推断 → YAML。推断出来的地址必须压过 YAML 旧值。
	if got := firstNonEmpty("", "192.168.1.10", "yaml.example.com"); got != "192.168.1.10" {
		t.Fatalf("inferred NIC address should win over the YAML fallback, got %q", got)
	}
}

func TestFirstNonEmptyUsesZLMHostWhenPlaybackHostIsEmpty(t *testing.T) {
	cfg := gbconfig.ZLMConfig{Host: "192.168.10.220"}
	if got := firstNonEmpty("", "", cfg.EffectivePlaybackHost()); got != "192.168.10.220" {
		t.Fatalf("empty playback host should fall back to ZLM host, got %q", got)
	}
}
