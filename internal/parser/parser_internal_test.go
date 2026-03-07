package parser

import (
	"testing"
	"time"
)

func TestParserConfigNormalization(t *testing.T) {
	defaults := DefaultConfig()
	minTimeout, maxTimeout, minMemory, maxMemory := LimitBounds()

	testCases := []struct {
		name            string
		config          Config
		env             map[string]string
		useEnv          bool
		expectedTimeout time.Duration
		expectedMemory  int
		expectFallback  bool
	}{
		{
			name:            "zero-value config uses defaults",
			config:          Config{},
			expectedTimeout: defaults.Timeout,
			expectedMemory:  defaults.NodeMaxOldSpaceMB,
		},
		{
			name:            "config values below bounds are clamped to minimums",
			config:          Config{Timeout: 100 * time.Millisecond, NodeMaxOldSpaceMB: 1},
			expectedTimeout: minTimeout,
			expectedMemory:  minMemory,
		},
		{
			name:            "config values above bounds are clamped to maximums",
			config:          Config{Timeout: 999 * time.Second, NodeMaxOldSpaceMB: 999999},
			expectedTimeout: maxTimeout,
			expectedMemory:  maxMemory,
		},
		{
			name: "environment values are parsed when valid",
			env: map[string]string{
				"PARSER_TIMEOUT_SECONDS": "12",
				"PARSER_MAX_OLD_SPACE_MB": "256",
			},
			useEnv:          true,
			expectedTimeout: 12 * time.Second,
			expectedMemory:  256,
		},
		{
			name: "invalid non-numeric environment values fall back to defaults",
			env: map[string]string{
				"PARSER_TIMEOUT_SECONDS": "not-a-number",
				"PARSER_MAX_OLD_SPACE_MB": "not-a-number",
			},
			useEnv:          true,
			expectedTimeout: defaults.Timeout,
			expectedMemory:  defaults.NodeMaxOldSpaceMB,
			expectFallback:  true,
		},
		{
			name: "out-of-range environment values fall back to defaults",
			env: map[string]string{
				"PARSER_TIMEOUT_SECONDS": "999",
				"PARSER_MAX_OLD_SPACE_MB": "999999",
			},
			useEnv:          true,
			expectedTimeout: defaults.Timeout,
			expectedMemory:  defaults.NodeMaxOldSpaceMB,
			expectFallback:  true,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			for key, value := range tc.env {
				t.Setenv(key, value)
			}

			var effective Config
			if tc.useEnv {
				effective = ConfigFromEnv().EffectiveConfig()
			} else {
				effective = tc.config.EffectiveConfig()
			}

			if effective.Timeout != tc.expectedTimeout {
				t.Fatalf("expected timeout %s, got %s", tc.expectedTimeout, effective.Timeout)
			}
			if effective.NodeMaxOldSpaceMB != tc.expectedMemory {
				t.Fatalf("expected NodeMaxOldSpaceMB %d, got %d", tc.expectedMemory, effective.NodeMaxOldSpaceMB)
			}

			if tc.expectFallback {
				if effective != defaults {
					t.Fatalf("expected fallback to defaults %#v, got %#v", defaults, effective)
				}
			}
		})
	}
}
