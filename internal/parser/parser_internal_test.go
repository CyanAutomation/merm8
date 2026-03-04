package parser

import (
	"testing"
	"time"
)

func TestConfigEffectiveConfig_Defaults(t *testing.T) {
	effective := (Config{}).EffectiveConfig()
	if effective.Timeout != defaultTimeout {
		t.Fatalf("expected default timeout %s, got %s", defaultTimeout, effective.Timeout)
	}
	if effective.NodeMaxOldSpaceMB != defaultNodeMaxOldSpaceSizeMB {
		t.Fatalf("expected default memory %d, got %d", defaultNodeMaxOldSpaceSizeMB, effective.NodeMaxOldSpaceMB)
	}
}

func TestConfigEffectiveConfig_ClampsToSafeBounds(t *testing.T) {
	effective := (Config{Timeout: 999 * time.Second, NodeMaxOldSpaceMB: 999999}).EffectiveConfig()
	if effective.Timeout != maxTimeout {
		t.Fatalf("expected timeout clamped to %s, got %s", maxTimeout, effective.Timeout)
	}
	if effective.NodeMaxOldSpaceMB != maxNodeMaxOldSpaceSizeMB {
		t.Fatalf("expected memory clamped to %d, got %d", maxNodeMaxOldSpaceSizeMB, effective.NodeMaxOldSpaceMB)
	}

	effective = (Config{Timeout: 100 * time.Millisecond, NodeMaxOldSpaceMB: 1}).EffectiveConfig()
	if effective.Timeout != minTimeout {
		t.Fatalf("expected timeout clamped to %s, got %s", minTimeout, effective.Timeout)
	}
	if effective.NodeMaxOldSpaceMB != minNodeMaxOldSpaceSizeMB {
		t.Fatalf("expected memory clamped to %d, got %d", minNodeMaxOldSpaceSizeMB, effective.NodeMaxOldSpaceMB)
	}
}

func TestReadMaxOldSpaceMB_DefaultOnMissing(t *testing.T) {
	t.Setenv("PARSER_MAX_OLD_SPACE_MB", "")
	if got := readMaxOldSpaceMB(); got != defaultNodeMaxOldSpaceSizeMB {
		t.Fatalf("expected default %d, got %d", defaultNodeMaxOldSpaceSizeMB, got)
	}
}

func TestReadMaxOldSpaceMB_DefaultOnInvalid(t *testing.T) {
	t.Setenv("PARSER_MAX_OLD_SPACE_MB", "not-a-number")
	if got := readMaxOldSpaceMB(); got != defaultNodeMaxOldSpaceSizeMB {
		t.Fatalf("expected default %d, got %d", defaultNodeMaxOldSpaceSizeMB, got)
	}
}

func TestReadMaxOldSpaceMB_UsesConfiguredValue(t *testing.T) {
	t.Setenv("PARSER_MAX_OLD_SPACE_MB", "256")
	if got := readMaxOldSpaceMB(); got != 256 {
		t.Fatalf("expected configured value 256, got %d", got)
	}
}

func TestReadMaxOldSpaceMB_DefaultOnOutOfRangeValue(t *testing.T) {
	t.Setenv("PARSER_MAX_OLD_SPACE_MB", "999999")
	if got := readMaxOldSpaceMB(); got != defaultNodeMaxOldSpaceSizeMB {
		t.Fatalf("expected default %d for out-of-range value, got %d", defaultNodeMaxOldSpaceSizeMB, got)
	}
}
