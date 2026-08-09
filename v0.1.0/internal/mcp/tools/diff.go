package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/store"
)

type TraceDiffTool struct{}

func (t *TraceDiffTool) Name() string { return "trace_diff" }

func (t *TraceDiffTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_diff",
		Description: "Compare two sessions: which trace paths differ.",
		InputSchema: JSONSchema{
			Type: "object",
			Properties: map[string]PropSpec{
				"session_a": stringProp("First session ID"),
				"session_b": stringProp("Second session ID"),
			},
			Required: []string{"session_a", "session_b"},
		},
	}
}

func (t *TraceDiffTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	var params struct {
		SessionA string `json:"session_a"`
		SessionB string `json:"session_b"`
	}
	if err := parseArgs(args, &params); err != nil {
		return fmt.Sprintf("Invalid argument: %v (check parameter types)", err), nil
	}
	if params.SessionA == "" || params.SessionB == "" {
		return "Specify both session_a and session_b", nil
	}

	sidA := &params.SessionA
	sidB := &params.SessionB

	logsA, err := st.ReadTraceLog(sidA, 0, 10000, nil, nil, nil)
	if err != nil {
		return fmt.Sprintf("Error reading session A: %v", err), nil
	}
	logsB, err := st.ReadTraceLog(sidB, 0, 10000, nil, nil, nil)
	if err != nil {
		return fmt.Sprintf("Error reading session B: %v", err), nil
	}

	pathsA := uniquePaths(logsA)
	pathsB := uniquePaths(logsB)

	onlyA := diffPaths(pathsA, pathsB)
	onlyB := diffPaths(pathsB, pathsA)
	common := intersectPaths(pathsA, pathsB)

	result := fmt.Sprintf("Session A: %s (%d events, %d paths)\n", params.SessionA, len(logsA), len(pathsA))
	result += fmt.Sprintf("Session B: %s (%d events, %d paths)\n", params.SessionB, len(logsB), len(pathsB))
	result += fmt.Sprintf("Common paths: %d\n", len(common))
	result += fmt.Sprintf("Only in A: %d\n", len(onlyA))
	result += fmt.Sprintf("Only in B: %d\n", len(onlyB))

	if len(onlyA) > 0 {
		result += "\nOnly in A:"
		for _, p := range onlyA[:min(len(onlyA), 20)] {
			result += fmt.Sprintf("\n  - %s", p)
		}
	}
	if len(onlyB) > 0 {
		result += "\nOnly in B:"
		for _, p := range onlyB[:min(len(onlyB), 20)] {
			result += fmt.Sprintf("\n  + %s", p)
		}
	}
	return result, nil
}

func uniquePaths(logs []store.TraceEntry) []string {
	seen := make(map[string]bool)
	var result []string
	for _, e := range logs {
		if !seen[e.TracePath] {
			seen[e.TracePath] = true
			result = append(result, e.TracePath)
		}
	}
	sort.Strings(result)
	return result
}

func diffPaths(a, b []string) []string {
	bSet := make(map[string]bool, len(b))
	for _, p := range b {
		bSet[p] = true
	}
	var result []string
	for _, p := range a {
		if !bSet[p] {
			result = append(result, p)
		}
	}
	return result
}

func intersectPaths(a, b []string) []string {
	bSet := make(map[string]bool, len(b))
	for _, p := range b {
		bSet[p] = true
	}
	var result []string
	for _, p := range a {
		if bSet[p] {
			result = append(result, p)
		}
	}
	return result
}
