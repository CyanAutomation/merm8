package parser

import (
	"sort"
	"strings"

	"github.com/CyanAutomation/merm8/internal/model"
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
	allNodeIDs, duplicates := analyzeSourceNodes(sourceCode)

	// Identify disconnected nodes (defined but not referenced in edges).
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

	// Store enhancements on diagram
	diagram.SourceNodeIDs = allNodeIDs
	diagram.DisconnectedNodeIDs = disconnected
	diagram.DuplicateNodeIDs = duplicates
}

// extractAllNodeIDsFromSource finds all node definitions in Mermaid source code.
// Matches explicit node definitions (A[...], B(...), etc.) and node references in edges.
// Returns unique node IDs in order of appearance.
func extractAllNodeIDsFromSource(source string) []string {
	nodeIDs, _ := analyzeSourceNodes(source)
	return nodeIDs
}

// isKeyword checks if a string is a Mermaid keyword to filter out false matches
func isKeyword(s string) bool {
	return mermaidKeywords[strings.ToLower(s)]
}

// findDuplicateNodeIDs identifies node IDs that appear multiple times in source.
// Counts explicit node definitions (A[...], A(...), etc.).
// Returns sorted list of node IDs that are defined more than once.
func findDuplicateNodeIDs(source string) []string {
	_, duplicates := analyzeSourceNodes(source)
	return duplicates
}

func analyzeSourceNodes(source string) ([]string, []string) {
	seen := make(map[string]bool)
	counts := make(map[string]int)
	var nodeIDs []string
	var edgeNodeIDs []string

	for _, rawLine := range strings.Split(source, "\n") {
		line := rawLine
		if idx := strings.Index(line, "%%"); idx >= 0 {
			line = line[:idx]
		}

		explicitDefs := extractExplicitNodeDefinitions(line)
		for _, nodeID := range explicitDefs {
			counts[nodeID]++
			if !seen[nodeID] {
				nodeIDs = append(nodeIDs, nodeID)
				seen[nodeID] = true
			}
		}

		for _, edgeNodeID := range extractEdgeNodeIDs(line) {
			edgeNodeIDs = append(edgeNodeIDs, edgeNodeID)
		}
	}

	for _, edgeNodeID := range edgeNodeIDs {
		if !seen[edgeNodeID] {
			nodeIDs = append(nodeIDs, edgeNodeID)
			seen[edgeNodeID] = true
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
	return nodeIDs, duplicates
}

type tokenSpan struct {
	start int
	end   int
}

func extractEdgeNodeIDs(source string) []string {
	var nodeIDs []string
	idMatches := extractNodeIDSpans(source)
	if len(idMatches) < 2 {
		return nodeIDs
	}

	for _, op := range extractEdgeOperators(source) {
		left := nearestNodeBefore(idMatches, op.start)
		right := nearestNodeAfter(idMatches, op.end)
		if left == nil || right == nil {
			continue
		}

		nodeIDs = append(nodeIDs, source[left.start:left.end], source[right.start:right.end])
	}

	return nodeIDs
}

func nearestNodeBefore(indices []tokenSpan, pos int) *tokenSpan {
	for i := len(indices) - 1; i >= 0; i-- {
		if indices[i].end <= pos {
			return &indices[i]
		}
	}
	return nil
}

func nearestNodeAfter(indices []tokenSpan, pos int) *tokenSpan {
	for i := range indices {
		if indices[i].start >= pos {
			return &indices[i]
		}
	}
	return nil
}

func extractNodeIDSpans(line string) []tokenSpan {
	spans := make([]tokenSpan, 0, 8)
	// Track nesting depth of label brackets/parens/braces to skip identifiers inside them
	var bracketStack []byte
	
	for i := 0; i < len(line); {
		c := line[i]
		
		// Update bracket stack to track if we're inside a label block
		if c == '[' || c == '(' || c == '{' {
			bracketStack = append(bracketStack, c)
			i++
			continue
		}
		if c == ']' || c == ')' || c == '}' {
			if len(bracketStack) > 0 {
				bracketStack = bracketStack[:len(bracketStack)-1]
			}
			i++
			continue
		}
		
		// Skip identifiers that are inside label blocks (bracket/paren/brace content)
		if len(bracketStack) > 0 {
			i++
			continue
		}
		
		if !isIdentifierStart(c) {
			i++
			continue
		}
		start := i
		i = scanNodeIDEnd(line, i)
		token := line[start:i]
		if isKeyword(token) || !isValidNodeIDToken(token) {
			continue
		}
		spans = append(spans, tokenSpan{start: start, end: i})
	}
	return spans
}

func extractExplicitNodeDefinitions(line string) []string {
	definitions := make([]string, 0, 4)
	for i := 0; i < len(line); i++ {
		if line[i] != '[' && line[i] != '(' && line[i] != '{' {
			continue
		}

		j := i - 1
		for j >= 0 && (line[j] == ' ' || line[j] == '\t') {
			j--
		}
		if j < 0 {
			continue
		}

		end := j + 1
		for j >= 0 && (isIdentifierStart(line[j]) || isASCIIDigit(line[j]) || line[j] == '-') {
			j--
		}
		start := j + 1
		if start >= end {
			continue
		}

		token := line[start:end]
		if isKeyword(token) || !isValidNodeIDToken(token) {
			continue
		}
		definitions = append(definitions, token)
	}
	return definitions
}

func extractEdgeOperators(line string) []tokenSpan {
	ops := make([]tokenSpan, 0, 8)
	for i := 0; i < len(line); {
		if !isEdgeOperatorChar(line[i]) {
			i++
			continue
		}
		start := i
		i++
		for i < len(line) && isEdgeOperatorChar(line[i]) {
			i++
		}
		if isEdgeOperatorToken(line[start:i]) {
			ops = append(ops, tokenSpan{start: start, end: i})
		}
	}
	return ops
}

func isIdentifierStart(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '_'
}

func isASCIIDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func isIdentifierCore(c byte) bool {
	return isIdentifierStart(c) || isASCIIDigit(c)
}

func isValidNodeIDToken(token string) bool {
	if token == "" || !isIdentifierStart(token[0]) || strings.HasSuffix(token, "-") {
		return false
	}
	for i := 1; i < len(token); i++ {
		if token[i] == '-' && (token[i-1] == '-' || i+1 == len(token) || !isIdentifierCore(token[i+1])) {
			return false
		}
	}
	return true
}

func scanNodeIDEnd(line string, start int) int {
	i := start + 1
	for i < len(line) {
		if isIdentifierCore(line[i]) {
			i++
			continue
		}
		if line[i] == '-' && i > 0 && i+1 < len(line) && isIdentifierCore(line[i-1]) && isIdentifierCore(line[i+1]) {
			i++
			continue
		}
		break
	}
	return i
}

func isEdgeOperatorChar(c byte) bool {
	switch c {
	case '-', '=', '.', 'o', 'x', '<', '>':
		return true
	default:
		return false
	}
}

func isEdgeOperatorToken(token string) bool {
	hasConnector := false
	connectorCount := 0
	hasRight := false
	for i := 0; i < len(token); i++ {
		switch token[i] {
		case '-', '=', '.':
			hasConnector = true
			connectorCount++
		case '>':
			hasRight = true
		}
	}
	if !hasConnector {
		return false
	}
	return hasRight || connectorCount >= 2
}
