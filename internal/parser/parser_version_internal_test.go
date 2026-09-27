package parser

import "testing"

func TestCacheKeyDiffersForParserVersions(t *testing.T) {
	cfg := DefaultConfig().EffectiveConfig()
	first := cacheKeyForVersion("graph TD\nA-->B", cfg, "bridge-v1\x00mermaid-v11")
	second := cacheKeyForVersion("graph TD\nA-->B", cfg, "bridge-v2\x00mermaid-v12")
	if first == second {
		t.Fatalf("cache keys must differ across parser identities: %q", first)
	}
}
