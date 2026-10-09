package gb28181

import (
	"testing"

	gbconfig "uvplatform.com/uvp-gb28181/app/gb28181/config"
)

func TestPlatformStreamHostKeepsYamlFallbackWhenOptionalValueIsEmpty(t *testing.T) {
	if got := platformStreamHost("stream.example.com", "yaml.example.com"); got != "stream.example.com" {
		t.Fatalf("configured stream host = %q", got)
	}
	if got := platformStreamHost("", "yaml.example.com"); got != "yaml.example.com" {
		t.Fatalf("empty configured stream host should use YAML fallback, got %q", got)
	}
	if got := platformStreamHost("  ", "  "); got != "" {
		t.Fatalf("blank stream hosts should remain empty, got %q", got)
	}
}

func TestPlatformStreamHostUsesZLMHostWhenPlaybackHostIsEmpty(t *testing.T) {
	cfg := gbconfig.ZLMConfig{Host: "192.168.10.220"}
	if got := platformStreamHost("", cfg.EffectivePlaybackHost()); got != "192.168.10.220" {
		t.Fatalf("empty playback host should fall back to ZLM host, got %q", got)
	}
}
