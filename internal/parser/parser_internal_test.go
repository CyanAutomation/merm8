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
		expectedTimeout time.Duration
		expectedMemory  int
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
			config:          Config{Timeout: maxTimeout + time.Second, NodeMaxOldSpaceMB: maxMemory + 1},
			expectedTimeout: maxTimeout,
			expectedMemory:  maxMemory,
		},
		{
			name:            "in-range values are preserved",
			config:          Config{Timeout: minTimeout + time.Second, NodeMaxOldSpaceMB: minMemory + 1},
			expectedTimeout: minTimeout + time.Second,
			expectedMemory:  minMemory + 1,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			effective := tc.config.EffectiveConfig()

			if effective.Timeout != tc.expectedTimeout {
				t.Fatalf("expected timeout %s, got %s", tc.expectedTimeout, effective.Timeout)
			}
			if effective.NodeMaxOldSpaceMB != tc.expectedMemory {
				t.Fatalf("expected NodeMaxOldSpaceMB %d, got %d", tc.expectedMemory, effective.NodeMaxOldSpaceMB)
			}
		})
	}
}

func TestParserConfigFromEnvNormalization(t *testing.T) {
	defaults := DefaultConfig()

	testCases := []struct {
		name           string
		env            map[string]string
		expectedConfig Config
	}{
		{
			name: "environment values are parsed when valid",
			env: map[string]string{
				"PARSER_TIMEOUT_SECONDS":  "12",
				"PARSER_MAX_OLD_SPACE_MB": "256",
			},
			expectedConfig: Config{Timeout: 12 * time.Second, NodeMaxOldSpaceMB: 256}.EffectiveConfig(),
		},
		{
			name: "invalid non-numeric environment values fall back to defaults",
			env: map[string]string{
				"PARSER_TIMEOUT_SECONDS":  "not-a-number",
				"PARSER_MAX_OLD_SPACE_MB": "not-a-number",
			},
			expectedConfig: defaults,
		},
		{
			name: "out-of-range environment values fall back to defaults",
			env: map[string]string{
				"PARSER_TIMEOUT_SECONDS":  "999",
				"PARSER_MAX_OLD_SPACE_MB": "999999",
			},
			expectedConfig: defaults,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			for key, value := range tc.env {
				t.Setenv(key, value)
			}

			effective := ConfigFromEnv().EffectiveConfig()
			if effective.Timeout != tc.expectedConfig.Timeout || effective.NodeMaxOldSpaceMB != tc.expectedConfig.NodeMaxOldSpaceMB {
				t.Fatalf("expected config %#v, got %#v", tc.expectedConfig, effective)
			}
		})
	}
}
