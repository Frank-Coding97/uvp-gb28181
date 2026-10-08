package gb28181

import "testing"

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
