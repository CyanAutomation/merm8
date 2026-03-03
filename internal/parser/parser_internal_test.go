package parser

import "testing"

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
