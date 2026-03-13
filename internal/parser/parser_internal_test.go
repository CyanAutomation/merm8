package parser

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/CyanAutomation/merm8/internal/model"
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

func TestParserConfig_EnvParsing_ValidInputs(t *testing.T) {
	defaults := DefaultConfig()

	tests := []struct {
		name            string
		env             map[string]string
		expectedTimeout time.Duration
		expectedMemory  int
		expectedSource  bool
	}{
		{
			name:            "PARSER_TIMEOUT_SECONDS parsed within bounds, defaults for unset env vars",
			env:             map[string]string{"PARSER_TIMEOUT_SECONDS": "12"},
			expectedTimeout: 12 * time.Second,
			expectedMemory:  defaults.NodeMaxOldSpaceMB,
			expectedSource:  true,
		},
		{
			name:            "PARSER_MAX_OLD_SPACE_MB parsed within bounds, defaults for unset env vars",
			env:             map[string]string{"PARSER_MAX_OLD_SPACE_MB": "256"},
			expectedTimeout: defaults.Timeout,
			expectedMemory:  256,
			expectedSource:  true,
		},
		{
			name:            "both timeout and memory env vars parsed when valid",
			env:             map[string]string{"PARSER_TIMEOUT_SECONDS": "12", "PARSER_MAX_OLD_SPACE_MB": "256"},
			expectedTimeout: 12 * time.Second,
			expectedMemory:  256,
			expectedSource:  true,
		},
		{
			name:            "PARSER_SOURCE_ENHANCEMENT can disable source analysis",
			env:             map[string]string{"PARSER_SOURCE_ENHANCEMENT": "false"},
			expectedTimeout: defaults.Timeout,
			expectedMemory:  defaults.NodeMaxOldSpaceMB,
			expectedSource:  false,
		},
	}

	for _, tc := range tests {
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
			if effective.SourceEnhancement == nil || *effective.SourceEnhancement != tc.expectedSource {
				got := "<nil>"
				if effective.SourceEnhancement != nil {
					got = strconv.FormatBool(*effective.SourceEnhancement)
				}
				t.Fatalf("expected SourceEnhancement %t, got %s", tc.expectedSource, got)
			}
		})
	}
}

func TestReadSourceEnhancementEnabled(t *testing.T) {
	t.Run("default true", func(t *testing.T) {
		t.Setenv("PARSER_SOURCE_ENHANCEMENT", "")
		if got := readSourceEnhancementEnabled(); got == nil || !*got {
			t.Fatal("expected source enhancement to default to enabled")
		}
	})

	t.Run("parse false", func(t *testing.T) {
		t.Setenv("PARSER_SOURCE_ENHANCEMENT", "false")
		if got := readSourceEnhancementEnabled(); got == nil || *got {
			t.Fatal("expected source enhancement to be disabled")
		}
	})

	t.Run("invalid falls back to true", func(t *testing.T) {
		t.Setenv("PARSER_SOURCE_ENHANCEMENT", "invalid")
		if got := readSourceEnhancementEnabled(); got == nil || !*got {
			t.Fatal("expected invalid value to fall back to enabled")
		}
	})
}

func TestReadParserMode(t *testing.T) {
	t.Run("defaults to pool", func(t *testing.T) {
		t.Setenv("PARSER_MODE", "")
		if got := readParserMode(); got != "pool" {
			t.Fatalf("expected pool default, got %q", got)
		}
	})

	t.Run("supports pool mode", func(t *testing.T) {
		t.Setenv("PARSER_MODE", "pool")
		if got := readParserMode(); got != "pool" {
			t.Fatalf("expected pool mode, got %q", got)
		}
	})

	t.Run("supports auto mode alias", func(t *testing.T) {
		t.Setenv("PARSER_MODE", "auto")
		if got := readParserMode(); got != "pool" {
			t.Fatalf("expected auto alias to resolve to pool mode, got %q", got)
		}
	})

	t.Run("invalid mode falls back", func(t *testing.T) {
		t.Setenv("PARSER_MODE", "garbage")
		if got := readParserMode(); got != "pool" {
			t.Fatalf("expected pool fallback, got %q", got)
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

func TestShouldEnhanceSourceAnalysis(t *testing.T) {
	t.Run("disabled config skips analysis", func(t *testing.T) {
		d := &model.Diagram{Type: model.DiagramTypeFlowchart}
		if shouldEnhanceSourceAnalysis(d, Config{SourceEnhancement: boolPtr(false)}) {
			t.Fatal("expected source analysis to be skipped when config is disabled")
		}
	})

	t.Run("only flowchart family enables analysis", func(t *testing.T) {
		flowchart := &model.Diagram{Type: model.DiagramTypeFlowchart}
		if shouldEnhanceSourceAnalysis(flowchart, Config{SourceEnhancement: boolPtr(true)}) {
			t.Fatal("expected flowchart diagrams to skip source analysis when request does not need it")
		}
		if !shouldEnhanceSourceAnalysis(flowchart, Config{SourceEnhancement: boolPtr(true), NeedSourceEnhancement: true}) {
			t.Fatal("expected flowchart diagrams to enable source analysis when requested")
		}

		sequence := &model.Diagram{Type: model.DiagramTypeSequence}
		if shouldEnhanceSourceAnalysis(sequence, Config{SourceEnhancement: boolPtr(true), NeedSourceEnhancement: true}) {
			t.Fatal("expected non-flowchart diagrams to skip source analysis")
		}
	})
}
