package parser

import (
	"testing"
	"time"
)

func TestParserConfig_EffectiveConfig_Defaults(t *testing.T) {
	effective := Config{}.EffectiveConfig()

	if effective.Timeout != defaultTimeout {
		t.Fatalf("expected default timeout %s, got %s", defaultTimeout, effective.Timeout)
	}
	if effective.NodeMaxOldSpaceMB != defaultNodeMaxOldSpaceSizeMB {
		t.Fatalf("expected default NodeMaxOldSpaceMB %d, got %d", defaultNodeMaxOldSpaceSizeMB, effective.NodeMaxOldSpaceMB)
	}
}

func TestParserConfig_EffectiveConfig_MaxClamp(t *testing.T) {
	effective := Config{Timeout: 999 * time.Second, NodeMaxOldSpaceMB: 999999}.EffectiveConfig()

	if effective.Timeout != maxTimeout {
		t.Fatalf("expected max timeout %s, got %s", maxTimeout, effective.Timeout)
	}
	if effective.NodeMaxOldSpaceMB != maxNodeMaxOldSpaceSizeMB {
		t.Fatalf("expected max NodeMaxOldSpaceMB %d, got %d", maxNodeMaxOldSpaceSizeMB, effective.NodeMaxOldSpaceMB)
	}
}

func TestParserConfig_EffectiveConfig_MinClamp(t *testing.T) {
	effective := Config{Timeout: 100 * time.Millisecond, NodeMaxOldSpaceMB: 1}.EffectiveConfig()

	if effective.Timeout != minTimeout {
		t.Fatalf("expected min timeout %s, got %s", minTimeout, effective.Timeout)
	}
	if effective.NodeMaxOldSpaceMB != minNodeMaxOldSpaceSizeMB {
		t.Fatalf("expected min NodeMaxOldSpaceMB %d, got %d", minNodeMaxOldSpaceSizeMB, effective.NodeMaxOldSpaceMB)
	}
}

func TestParserConfigFromEnv_OverrideValid(t *testing.T) {
	t.Setenv("PARSER_TIMEOUT_SECONDS", "12")
	t.Setenv("PARSER_MAX_OLD_SPACE_MB", "256")

	effective := ConfigFromEnv().EffectiveConfig()

	if effective.Timeout != 12*time.Second {
		t.Fatalf("expected timeout %s, got %s", 12*time.Second, effective.Timeout)
	}
	if effective.NodeMaxOldSpaceMB != 256 {
		t.Fatalf("expected NodeMaxOldSpaceMB %d, got %d", 256, effective.NodeMaxOldSpaceMB)
	}
}

func TestParserConfigFromEnv_OverrideInvalid(t *testing.T) {
	t.Run("non-numeric values use defaults", func(t *testing.T) {
		t.Setenv("PARSER_TIMEOUT_SECONDS", "not-a-number")
		t.Setenv("PARSER_MAX_OLD_SPACE_MB", "not-a-number")

		effective := ConfigFromEnv().EffectiveConfig()

		if effective.Timeout != defaultTimeout {
			t.Fatalf("expected default timeout %s, got %s", defaultTimeout, effective.Timeout)
		}
		if effective.NodeMaxOldSpaceMB != defaultNodeMaxOldSpaceSizeMB {
			t.Fatalf("expected default NodeMaxOldSpaceMB %d, got %d", defaultNodeMaxOldSpaceSizeMB, effective.NodeMaxOldSpaceMB)
		}
	})

	t.Run("out-of-range values use defaults", func(t *testing.T) {
		t.Setenv("PARSER_TIMEOUT_SECONDS", "999")
		t.Setenv("PARSER_MAX_OLD_SPACE_MB", "999999")

		effective := ConfigFromEnv().EffectiveConfig()

		if effective.Timeout != defaultTimeout {
			t.Fatalf("expected default timeout %s, got %s", defaultTimeout, effective.Timeout)
		}
		if effective.NodeMaxOldSpaceMB != defaultNodeMaxOldSpaceSizeMB {
			t.Fatalf("expected default NodeMaxOldSpaceMB %d, got %d", defaultNodeMaxOldSpaceSizeMB, effective.NodeMaxOldSpaceMB)
		}
	})
}
