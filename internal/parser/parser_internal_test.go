package parser

import (
	"testing"
	"time"
)

func TestParserConfig_AppliesDefaultsAndBounds(t *testing.T) {
	tests := []struct {
		name       string
		config     Config
		envVars    map[string]string
		wantErr    bool
		wantMinMax bool
	}{
		{
			name:    "empty config uses defaults",
			config:  Config{},
			envVars: map[string]string{"PARSER_MAX_OLD_SPACE_MB": ""},
		},
		{
			name: "clamps timeout to max",
			config: Config{Timeout: 999 * time.Second},
		},
		{
			name: "clamps memory to max",
			config: Config{NodeMaxOldSpaceMB: 999999},
		},
		{
			name: "clamps timeout and memory to min",
			config: Config{Timeout: 100 * time.Millisecond, NodeMaxOldSpaceMB: 1},
			wantMinMax: true,
		},
		{
			name:    "env var invalid uses default",
			envVars: map[string]string{"PARSER_MAX_OLD_SPACE_MB": "not-a-number"},
		},
		{
			name:    "env var out of range uses default",
			envVars: map[string]string{"PARSER_MAX_OLD_SPACE_MB": "999999"},
		},
		{
			name:    "env var valid value applied",
			envVars: map[string]string{"PARSER_MAX_OLD_SPACE_MB": "256"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envVars != nil {
				for k, v := range tt.envVars {
					t.Setenv(k, v)
				}
			}

			effective := tt.config.EffectiveConfig()

			// Verify defaults are applied
			if effective.Timeout == 0 {
				t.Fatal("timeout should never be zero")
			}
			if effective.NodeMaxOldSpaceMB == 0 {
				t.Fatal("NodeMaxOldSpaceMB should never be zero")
			}

			// Verify bounds are respected
			if effective.Timeout < minTimeout || effective.Timeout > maxTimeout {
				t.Fatalf("timeout out of bounds: %v not in [%s, %s]", effective.Timeout, minTimeout, maxTimeout)
			}
			if effective.NodeMaxOldSpaceMB < minNodeMaxOldSpaceSizeMB || effective.NodeMaxOldSpaceMB > maxNodeMaxOldSpaceSizeMB {
				t.Fatalf("memory out of bounds: %d not in [%d, %d]", effective.NodeMaxOldSpaceMB, minNodeMaxOldSpaceSizeMB, maxNodeMaxOldSpaceSizeMB)
			}
		
			// For min/max case, verify clamping
			if tt.wantMinMax {
				if effective.Timeout != minTimeout {
					t.Errorf("expected min timeout %s, got %s", minTimeout, effective.Timeout)
				}
				if effective.NodeMaxOldSpaceMB != minNodeMaxOldSpaceSizeMB {
					t.Errorf("expected min memory, got %d", effective.NodeMaxOldSpaceMB)
				}
			}
		})
	}
}
