package parser

import (
	"regexp"
	"sort"
	"strings"

	"github.com/CyanAutomation/merm8/internal/model"
)

var (
	nodeDefinitionPattern = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*(?:-[A-Za-z0-9_]+)*)\s*(?:\[|\(|{)`)
	nodeIDPattern         = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*(?:-[A-Za-z0-9_]+)*`)
	edgeOperatorPattern   = regexp.MustCompile(`(?:[ox<]?(?:-{2,}|={2,}|\.{2,})[ox>]?)|(?:[ox<]?(?:-|=|\.)+[ox>]*>+)`)
)

var mermaidKeywords = map[string]bool{
	"graph":           true,
	"subgraph":        true,
	"end":             true,
	"direction":       true,
	"td":              true,
	"lr":              true,
	"du":              true,
	"bt":              true,
	"tb":              true,
	"rl":              true,
	"sequencediagram": true,
	"participant":     true,
	"classdiagram":    true,
	"class":           true,
	"erdiagram":       true,
	"statediagram":    true,
	"state":           true,
	"entity":          true,
	"rel":             true,
}

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
		edgeNodes[strings.ToLower(edge.From)] = true
		edgeNodes[strings.ToLower(edge.To)] = true
	}
	for _, node := range diagram.Nodes {
		edgeNodes[strings.ToLower(node.ID)] = true
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
	seen := make(map[string]bool)
	var result []string

	cleanedSource := stripMermaidComments(source)

	// Extract from explicit node definitions
	for _, match := range nodeDefinitionPattern.FindAllStringSubmatch(cleanedSource, -1) {
		nodeID := match[1]
		if !seen[nodeID] && !isKeyword(nodeID) {
			result = append(result, nodeID)
			seen[nodeID] = true
		}
	}

	// Also capture node IDs used in edge endpoints for implicit node definitions
	for _, edgeNodeID := range extractEdgeNodeIDs(cleanedSource) {
		if !seen[edgeNodeID] && !isKeyword(edgeNodeID) {
			result = append(result, edgeNodeID)
			seen[edgeNodeID] = true
		}
	}

	return result
}

// isKeyword checks if a string is a Mermaid keyword to filter out false matches
func isKeyword(s string) bool {
	return mermaidKeywords[strings.ToLower(s)]
}

// findDuplicateNodeIDs identifies node IDs that appear multiple times in source.
// Counts explicit node definitions (A[...], A(...), etc.).
// Returns sorted list of node IDs that are defined more than once.
func findDuplicateNodeIDs(source string) []string {
	counts := make(map[string]int)

	cleanedSource := stripMermaidComments(source)

	// Count occurrences, filtering out keywords
	for _, match := range nodeDefinitionPattern.FindAllStringSubmatch(cleanedSource, -1) {
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

func stripMermaidComments(source string) string {
	lines := strings.Split(source, "\n")
	for i, line := range lines {
		if idx := strings.Index(line, "%%"); idx >= 0 {
			lines[i] = line[:idx]
		}
	}
	return strings.Join(lines, "\n")
}

func extractEdgeNodeIDs(source string) []string {
	var nodeIDs []string
	for _, line := range strings.Split(source, "\n") {
		idMatches := nodeIDPattern.FindAllStringIndex(line, -1)
		if len(idMatches) < 2 {
			continue
		}

		for _, op := range edgeOperatorPattern.FindAllStringIndex(line, -1) {
			left := nearestNodeBefore(idMatches, op[0])
			right := nearestNodeAfter(idMatches, op[1])
			if left == nil || right == nil {
				continue
			}

			nodeIDs = append(nodeIDs, line[left[0]:left[1]], line[right[0]:right[1]])
		}
	}

	return nodeIDs
}

func nearestNodeBefore(indices [][]int, pos int) []int {
	for i := len(indices) - 1; i >= 0; i-- {
		if indices[i][1] <= pos {
			return indices[i]
		}
	}
	return nil
}

func nearestNodeAfter(indices [][]int, pos int) []int {
	for _, index := range indices {
		if index[0] >= pos {
			return index
		}
	}
	return nil
}
