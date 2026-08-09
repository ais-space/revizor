package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/store"
	"github.com/ais-platform/ais_products/revizor/internal/trace"
)

type TraceWhyTool struct{}

func (t *TraceWhyTool) Name() string { return "trace_why" }

func (t *TraceWhyTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_why",
		Description: "Explain why a trace point is enabled or disabled — checks all glob rules.",
		InputSchema: JSONSchema{
			Type: "object",
			Properties: map[string]PropSpec{
				"path": stringProp("Trace path to check (e.g., elevation.callback.same_provider)"),
			},
			Required: []string{"path"},
		},
	}
}

func (t *TraceWhyTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	var params struct {
		Path string `json:"path"`
	}
	if err := parseArgs(args, &params); err != nil {
		return fmt.Sprintf("Invalid argument: %v (check parameter types)", err), nil
	}
	if params.Path == "" {
		return "No path specified — provide the trace path to check.", nil
	}

	if !trace.ValidatePath(params.Path) {
		return fmt.Sprintf("\u274c Invalid path: %s", params.Path), nil
	}

	result := trace.ShouldTrace(params.Path, "")

	status := "\u274c DISABLED"
	if result {
		status = "\u2705 ENABLED"
	}

	lines := []string{
		fmt.Sprintf("Path: %s", params.Path),
		fmt.Sprintf("Status: %s", status),
		"",
	}
	if result {
		lines = append(lines, "The point is enabled — a matching rule was found in the configuration.")
		lines = append(lines, "To disable, use trace_disable().")
	} else {
		lines = append(lines, "The point is disabled — no active rule in the configuration.")
		lines = append(lines, "To enable, use trace_enable() or trace_start().")
	}

	resultStr := ""
	for i, l := range lines {
		if i > 0 {
			resultStr += "\n"
		}
		resultStr += l
	}
	return resultStr, nil
}
