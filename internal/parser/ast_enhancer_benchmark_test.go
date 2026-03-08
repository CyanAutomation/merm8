package parser

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var (
	legacyNodeDefinitionPattern = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*(?:-[A-Za-z0-9_]+)*)\s*(?:\[|\(|{)`)
	legacyNodeIDPattern         = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*(?:-[A-Za-z0-9_]+)*`)
	legacyEdgeOperatorPattern   = regexp.MustCompile(`(?:[ox<]?(?:-{2,}|={2,}|\.{2,})[ox>]?)|(?:[ox<]?(?:-|=|\.)+[ox>]*>+)`)
)

func BenchmarkAnalyzeSourceNodes_LargeDiagram(b *testing.B) {
	source := makeLargeFlowchartSource(2500)
	b.ReportAllocs()
	b.Run("optimized", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			nodes, duplicates := analyzeSourceNodes(source)
			if len(nodes) == 0 || len(duplicates) == 0 {
				b.Fatalf("expected populated analysis results")
			}
		}
	})

	b.Run("legacy-regex", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			nodes, duplicates := legacyAnalyzeSourceNodes(source)
			if len(nodes) == 0 || len(duplicates) == 0 {
				b.Fatalf("expected populated analysis results")
			}
		}
	})
}

func legacyAnalyzeSourceNodes(source string) ([]string, []string) {
	seen := make(map[string]bool)
	counts := make(map[string]int)
	var nodeIDs []string

	cleanedSource := legacyStripMermaidComments(source)
	for _, match := range legacyNodeDefinitionPattern.FindAllStringSubmatch(cleanedSource, -1) {
		nodeID := match[1]
		if isKeyword(nodeID) {
			continue
		}
		counts[nodeID]++
		if !seen[nodeID] {
			nodeIDs = append(nodeIDs, nodeID)
			seen[nodeID] = true
		}
	}

	for _, edgeNodeID := range legacyExtractEdgeNodeIDs(cleanedSource) {
		if !seen[edgeNodeID] && !isKeyword(edgeNodeID) {
			nodeIDs = append(nodeIDs, edgeNodeID)
			seen[edgeNodeID] = true
		}
	}

	duplicates := make([]string, 0)
	for nodeID, count := range counts {
		if count > 1 {
			duplicates = append(duplicates, nodeID)
		}
	}
	return nodeIDs, duplicates
}

func legacyStripMermaidComments(source string) string {
	lines := strings.Split(source, "\n")
	for i, line := range lines {
		if idx := strings.Index(line, "%%"); idx >= 0 {
			lines[i] = line[:idx]
		}
	}
	return strings.Join(lines, "\n")
}

func legacyExtractEdgeNodeIDs(source string) []string {
	var nodeIDs []string
	for _, line := range strings.Split(source, "\n") {
		idMatches := legacyNodeIDPattern.FindAllStringIndex(line, -1)
		if len(idMatches) < 2 {
			continue
		}

		for _, op := range legacyEdgeOperatorPattern.FindAllStringIndex(line, -1) {
			left := legacyNearestNodeBefore(idMatches, op[0])
			right := legacyNearestNodeAfter(idMatches, op[1])
			if left == nil || right == nil {
				continue
			}
			nodeIDs = append(nodeIDs, line[left[0]:left[1]], line[right[0]:right[1]])
		}
	}
	return nodeIDs
}

func legacyNearestNodeBefore(indices [][]int, pos int) []int {
	for i := len(indices) - 1; i >= 0; i-- {
		if indices[i][1] <= pos {
			return indices[i]
		}
	}
	return nil
}

func legacyNearestNodeAfter(indices [][]int, pos int) []int {
	for _, index := range indices {
		if index[0] >= pos {
			return index
		}
	}
	return nil
}

func makeLargeFlowchartSource(size int) string {
	var sb strings.Builder
	sb.Grow(size * 32)
	sb.WriteString("graph TD\n")
	for i := 0; i < size; i++ {
		id := "node_" + strconv.Itoa(i)
		sb.WriteString(id)
		sb.WriteString("[Node ")
		sb.WriteString(strconv.Itoa(i))
		sb.WriteString("]\n")
		if i > 0 {
			sb.WriteString("node_")
			sb.WriteString(strconv.Itoa(i - 1))
			sb.WriteString(" --> ")
			sb.WriteString(id)
			sb.WriteString("\n")
		}
		if i%250 == 0 {
			sb.WriteString(id)
			sb.WriteString("[duplicate]\n")
			sb.WriteString("%% comment line ")
			sb.WriteString(strconv.Itoa(i))
			sb.WriteString("\n")
		}
	}
	return sb.String()
}
