package parser

import (
	"regexp"
	"sort"

	"github.com/CyanAutomation/merm8/internal/model"
)

// EnhanceASTWithSourceAnalysis augments the diagram with source-level analysis
// to identify nodes that Mermaid's parser may drop during normalization.
// This fixes issues where:
// - Disconnected/orphaned nodes don't appear in the AST
// - Duplicate node definitions get merged before rule processing
func EnhanceASTWithSourceAnalysis(diagram *model.Diagram, sourceCode string) {
	if diagram == nil || sourceCode == "" {
		return
	}

	// Extract all node IDs defined in the source code
	allNodeIDs := extractAllNodeIDsFromSource(sourceCode)

	// Identify disconnected nodes (defined but not referenced in edges)
	edgeNodes := make(map[string]bool)
	for _, edge := range diagram.Edges {
		edgeNodes[edge.From] = true
		edgeNodes[edge.To] = true
	}
	for _, node := range diagram.Nodes {
		edgeNodes[node.ID] = true
	}

	disconnected := []string{}
	for _, nodeID := range allNodeIDs {
		if !edgeNodes[nodeID] {
			disconnected = append(disconnected, nodeID)
		}
	}

	// Identify duplicate node IDs
	duplicates := findDuplicateNodeIDs(sourceCode)

	// Store enhancements on diagram
	diagram.SourceNodeIDs = allNodeIDs
	diagram.DisconnectedNodeIDs = disconnected
	diagram.DuplicateNodeIDs = duplicates
}

// extractAllNodeIDsFromSource finds all node definitions in Mermaid source code.
// Matches explicit node definitions (A[...], B(...), etc.) and node references in edges.
// Returns unique node IDs in order of appearance.
func extractAllNodeIDsFromSource(source string) []string {
	// Match node definitions: identifier followed by node type brackets
	// This captures: A[label], B(label), C{label}, A[[label]], etc.
	nodeDefinitionPattern := regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)\s*(?:\[|\(|{)`)

	seen := make(map[string]bool)
	var result []string

	// Extract from explicit node definitions
	for _, match := range nodeDefinitionPattern.FindAllStringSubmatch(source, -1) {
		nodeID := match[1]
		if !seen[nodeID] && !isKeyword(nodeID) {
			result = append(result, nodeID)
			seen[nodeID] = true
		}
	}

	return result
}

// isKeyword checks if a string is a Mermaid keyword to filter out false matches
func isKeyword(s string) bool {
	keywords := map[string]bool{
		"graph":           true,
		"subgraph":        true,
		"end":             true,
		"direction":       true,
		"TD":              true,
		"LR":              true,
		"DU":              true,
		"BT":              true,
		"TB":              true,
		"RL":              true,
		"sequenceDiagram": true,
		"participant":     true,
		"classDiagram":    true,
		"class":           true,
		"erDiagram":       true,
		"stateDiagram":    true,
		"state":           true,
		"entity":          true,
		"rel":             true,
	}
	return keywords[s]
}

// findDuplicateNodeIDs identifies node IDs that appear multiple times in source.
// Counts explicit node definitions (A[...], A(...), etc.).
// Returns sorted list of node IDs that are defined more than once.
func findDuplicateNodeIDs(source string) []string {
	// Match node definitions
	nodeDefinitionPattern := regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)\s*(?:\[|\(|{)`)

	counts := make(map[string]int)

	// Count occurrences, filtering out keywords
	for _, match := range nodeDefinitionPattern.FindAllStringSubmatch(source, -1) {
		nodeID := match[1]
		if !isKeyword(nodeID) {
			counts[nodeID]++
		}
	}

	var duplicates []string
	for nodeID, count := range counts {
		if count > 1 {
			duplicates = append(duplicates, nodeID)
		}
	}

	// Sort for consistent output
	sort.Strings(duplicates)
	return duplicates
}
