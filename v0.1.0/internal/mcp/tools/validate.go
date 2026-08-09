package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/store"
	"github.com/ais-platform/ais_products/revizor/internal/trace"
)

type TraceValidatePathTool struct{}

func (t *TraceValidatePathTool) Name() string { return "trace_validate_path" }

func (t *TraceValidatePathTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_validate_path",
		Description: "Check if a trace path format is valid.",
		InputSchema: JSONSchema{
			Type: "object",
			Properties: map[string]PropSpec{
				"path": stringProp("Trace path to validate (e.g., elevation.callback.enter)"),
			},
			Required: []string{"path"},
		},
	}
}

func (t *TraceValidatePathTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	var params struct {
		Path string `json:"path"`
	}
	if err := parseArgs(args, &params); err != nil {
		return fmt.Sprintf("Invalid argument: %v (check parameter types)", err), nil
	}
	if params.Path == "" {
		return "No path specified — provide the trace path to check.", nil
	}

	if trace.ValidatePath(params.Path) {
		return fmt.Sprintf("\u2705 Valid path: %s", params.Path), nil
	}
	return fmt.Sprintf("\u274c Invalid path: %s\nFormat: lowercase [a-z0-9_.], max %d characters.\nExample: elevation.callback.enter", params.Path, trace.MaxPathLength), nil
}
