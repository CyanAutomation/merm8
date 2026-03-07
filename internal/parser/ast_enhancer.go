package parser

import (
	"regexp"
	"sort"
	"strings"

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
	// Note: parser normalizes node IDs to lowercase, so we must normalize
	// source-extracted IDs for comparison
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
		// Parser normalizes node IDs to lowercase; normalize source ID for comparison
		normalizedID := strings.ToLower(nodeID)
		if !edgeNodes[normalizedID] {
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
	// Match flowchart node IDs used in explicit node definitions.
	// Keep this aligned with parser normalization support for common IDs like:
	// A[label], service-node[label], node_2(label), etc.
	nodeDefinitionPattern := regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_-]*)\s*(?:\[|\(|{)`)

	seen := make(map[string]bool)
	var result []string

	// Remove comments before processing to avoid false matches in comment text
	// Comments in Mermaid flowcharts start with %% and continue to end of line
	lines := strings.Split(source, "\n")
	var cleanedLines []string
	for _, line := range lines {
		// Remove comment portion from the line
		if idx := strings.Index(line, "%%"); idx >= 0 {
			line = line[:idx]
		}
		cleanedLines = append(cleanedLines, line)
	}
	cleanedSource := strings.Join(cleanedLines, "\n")

	// Extract from explicit node definitions
	for _, match := range nodeDefinitionPattern.FindAllStringSubmatch(cleanedSource, -1) {
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
	// Match node definitions with the same ID format used by extractAllNodeIDsFromSource.
	nodeDefinitionPattern := regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_-]*)\s*(?:\[|\(|{)`)

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
