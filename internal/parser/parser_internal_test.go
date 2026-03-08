package parser

import (
	"strings"
	"testing"
	"time"
)

func TestParserConfig_EffectiveConfig_Boundaries(t *testing.T) {
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
			name:            "minimum boundary clamps to usable floor to prevent unusable parser limits",
			config:          Config{Timeout: 100 * time.Millisecond, NodeMaxOldSpaceMB: 1},
			expectedTimeout: minTimeout,
			expectedMemory:  minMemory,
		},
		{
			name:            "maximum boundary clamps to hard cap",
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

	t.Run("valid env parsing", func(t *testing.T) {
		testCases := []struct {
			name            string
			env             map[string]string
			expectedTimeout time.Duration
			expectedMemory  int
		}{
			{
				name: "PARSER_TIMEOUT_SECONDS within bounds is parsed while PARSER_MAX_OLD_SPACE_MB falls back to default when unset",
				env: map[string]string{
					"PARSER_TIMEOUT_SECONDS": "12",
				},
				expectedTimeout: 12 * time.Second,
				expectedMemory:  defaults.NodeMaxOldSpaceMB,
			},
			{
				name: "PARSER_MAX_OLD_SPACE_MB within bounds is parsed while PARSER_TIMEOUT_SECONDS falls back to default when unset",
				env: map[string]string{
					"PARSER_MAX_OLD_SPACE_MB": "256",
				},
				expectedTimeout: defaults.Timeout,
				expectedMemory:  256,
			},
			{
				name: "PARSER_TIMEOUT_SECONDS and PARSER_MAX_OLD_SPACE_MB are both parsed when valid",
				env: map[string]string{
					"PARSER_TIMEOUT_SECONDS":  "12",
					"PARSER_MAX_OLD_SPACE_MB": "256",
				},
				expectedTimeout: 12 * time.Second,
				expectedMemory:  256,
			},
		}

		for _, tc := range testCases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				for key, value := range tc.env {
					t.Setenv(key, value)
				}

				effective := ConfigFromEnv().EffectiveConfig()
				if effective.Timeout != tc.expectedTimeout {
					t.Fatalf("expected timeout %s, got %s", tc.expectedTimeout, effective.Timeout)
				}
				if effective.NodeMaxOldSpaceMB != tc.expectedMemory {
					t.Fatalf("expected NodeMaxOldSpaceMB %d, got %d", tc.expectedMemory, effective.NodeMaxOldSpaceMB)
				}
			})
		}
	})

	t.Run("invalid format fallback", func(t *testing.T) {
		// Fallback policy: malformed PARSER_TIMEOUT_SECONDS/PARSER_MAX_OLD_SPACE_MB values are rejected and defaulted.
		testCases := []struct {
			name            string
			env             map[string]string
			expectedTimeout time.Duration
			expectedMemory  int
		}{
			{
				name: "PARSER_TIMEOUT_SECONDS non-numeric value uses default timeout",
				env: map[string]string{
					"PARSER_TIMEOUT_SECONDS": "not-a-number",
				},
				expectedTimeout: defaults.Timeout,
				expectedMemory:  defaults.NodeMaxOldSpaceMB,
			},
			{
				name: "PARSER_MAX_OLD_SPACE_MB non-numeric value uses default memory",
				env: map[string]string{
					"PARSER_MAX_OLD_SPACE_MB": "not-a-number",
				},
				expectedTimeout: defaults.Timeout,
				expectedMemory:  defaults.NodeMaxOldSpaceMB,
			},
			{
				name: "PARSER_TIMEOUT_SECONDS and PARSER_MAX_OLD_SPACE_MB malformed values both use defaults",
				env: map[string]string{
					"PARSER_TIMEOUT_SECONDS":  "not-a-number",
					"PARSER_MAX_OLD_SPACE_MB": "not-a-number",
				},
				expectedTimeout: defaults.Timeout,
				expectedMemory:  defaults.NodeMaxOldSpaceMB,
			},
		}

		for _, tc := range testCases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				for key, value := range tc.env {
					t.Setenv(key, value)
				}

				effective := ConfigFromEnv().EffectiveConfig()
				if effective.Timeout != tc.expectedTimeout {
					t.Fatalf("expected timeout %s, got %s", tc.expectedTimeout, effective.Timeout)
				}
				if effective.NodeMaxOldSpaceMB != tc.expectedMemory {
					t.Fatalf("expected NodeMaxOldSpaceMB %d, got %d", tc.expectedMemory, effective.NodeMaxOldSpaceMB)
				}
			})
		}
	})

	t.Run("out-of-range clamping/rejection semantics", func(t *testing.T) {
		// Rejection policy: out-of-range PARSER_TIMEOUT_SECONDS/PARSER_MAX_OLD_SPACE_MB env values fall back to defaults (not clamped).
		testCases := []struct {
			name            string
			env             map[string]string
			expectedTimeout time.Duration
			expectedMemory  int
		}{
			{
				name: "PARSER_TIMEOUT_SECONDS above max is rejected and defaults timeout",
				env: map[string]string{
					"PARSER_TIMEOUT_SECONDS": "999",
				},
				expectedTimeout: defaults.Timeout,
				expectedMemory:  defaults.NodeMaxOldSpaceMB,
			},
			{
				name: "PARSER_MAX_OLD_SPACE_MB above max is rejected and defaults memory",
				env: map[string]string{
					"PARSER_MAX_OLD_SPACE_MB": "999999",
				},
				expectedTimeout: defaults.Timeout,
				expectedMemory:  defaults.NodeMaxOldSpaceMB,
			},
			{
				name: "PARSER_TIMEOUT_SECONDS and PARSER_MAX_OLD_SPACE_MB out-of-range values both default",
				env: map[string]string{
					"PARSER_TIMEOUT_SECONDS":  "999",
					"PARSER_MAX_OLD_SPACE_MB": "999999",
				},
				expectedTimeout: defaults.Timeout,
				expectedMemory:  defaults.NodeMaxOldSpaceMB,
			},
		}

		for _, tc := range testCases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				for key, value := range tc.env {
					t.Setenv(key, value)
				}

				effective := ConfigFromEnv().EffectiveConfig()
				if effective.Timeout != tc.expectedTimeout {
					t.Fatalf("expected timeout %s, got %s", tc.expectedTimeout, effective.Timeout)
				}
				if effective.NodeMaxOldSpaceMB != tc.expectedMemory {
					t.Fatalf("expected NodeMaxOldSpaceMB %d, got %d", tc.expectedMemory, effective.NodeMaxOldSpaceMB)
				}
			})
		}
	})
}

func TestReadParserMode(t *testing.T) {
	t.Run("defaults to subprocess", func(t *testing.T) {
		t.Setenv("PARSER_MODE", "")
		if got := readParserMode(); got != "subprocess" {
			t.Fatalf("expected subprocess default, got %q", got)
		}
	})

	t.Run("supports pool mode", func(t *testing.T) {
		t.Setenv("PARSER_MODE", "pool")
		if got := readParserMode(); got != "pool" {
			t.Fatalf("expected pool mode, got %q", got)
		}
	})

	t.Run("invalid mode falls back", func(t *testing.T) {
		t.Setenv("PARSER_MODE", "garbage")
		if got := readParserMode(); got != "subprocess" {
			t.Fatalf("expected subprocess fallback, got %q", got)
		}
	})
}

func TestReadWorkerPoolSize(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		t.Setenv("PARSER_WORKER_POOL_SIZE", "")
		if got := readWorkerPoolSize(); got != 4 {
			t.Fatalf("expected default pool size 4, got %d", got)
		}
	})

	t.Run("clamps min and max", func(t *testing.T) {
		t.Setenv("PARSER_WORKER_POOL_SIZE", "0")
		if got := readWorkerPoolSize(); got != 1 {
			t.Fatalf("expected min pool size 1, got %d", got)
		}

		t.Setenv("PARSER_WORKER_POOL_SIZE", "999")
		if got := readWorkerPoolSize(); got != 64 {
			t.Fatalf("expected max pool size 64, got %d", got)
		}
	})
}

func TestNewWorkerRequestID(t *testing.T) {
	first := newWorkerRequestID()
	second := newWorkerRequestID()

	if !strings.HasPrefix(first, "req-") {
		t.Fatalf("expected request id to include req- prefix, got %q", first)
	}
	if first == second {
		t.Fatalf("expected unique request ids across invocations")
	}
}
