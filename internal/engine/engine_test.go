package engine_test

import (
	"testing"

	"github.com/CyanAutomation/merm8/internal/engine"
	"github.com/CyanAutomation/merm8/internal/model"
	"github.com/CyanAutomation/merm8/internal/rules"
)

func TestEngine_CleanDiagram(t *testing.T) {
	d := &model.Diagram{
		Nodes: []model.Node{{ID: "A"}, {ID: "B"}},
		Edges: []model.Edge{{From: "A", To: "B"}},
	}
	e := engine.New()
	issues := e.Run(d, rules.Config{})
	if len(issues) != 0 {
		t.Fatalf("expected no issues for clean diagram, got %v", issues)
	}
}

func TestEngine_ReturnsNonNilSlice(t *testing.T) {
	d := &model.Diagram{}
	e := engine.New()
	issues := e.Run(d, rules.Config{})
	if issues == nil {
		t.Fatal("Run should never return a nil slice")
	}
}

func TestEngine_DuplicateAndDisconnected(t *testing.T) {
	d := &model.Diagram{
		Nodes: []model.Node{{ID: "A"}, {ID: "A"}, {ID: "C"}},
		Edges: []model.Edge{{From: "A", To: "B"}},
	}
	e := engine.New()
	issues := e.Run(d, rules.Config{})
	// Expect: 1 duplicate + 1 disconnected (C) = 2 issues minimum
	if len(issues) < 2 {
		t.Fatalf("expected at least 2 issues, got %d: %v", len(issues), issues)
	}
}
