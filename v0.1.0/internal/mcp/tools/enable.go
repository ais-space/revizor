package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/store"
	"github.com/ais-platform/ais_products/revizor/internal/trace"
)

type TraceEnableTool struct{}

func (t *TraceEnableTool) Name() string { return "trace_enable" }

func (t *TraceEnableTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_enable",
		Description: "Enable a single trace point. Supports glob patterns (e.g., auth.**).",
		InputSchema: JSONSchema{
			Type: "object",
			Properties: map[string]PropSpec{
				"path":       stringProp("Trace path to enable (e.g., auth.**)"),
				"session_id": stringProp("Session ID (optional, default: global)"),
			},
			Required: []string{"path"},
		},
	}
}

func (t *TraceEnableTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	var params struct {
		Path      string `json:"path"`
		SessionID string `json:"session_id"`
	}
	if err := parseArgs(args, &params); err != nil {
		return fmt.Sprintf("Invalid argument: %v (check parameter types)", err), nil
	}
	if params.Path == "" {
		return "No path specified — provide the trace path to check.", nil
	}

	var sidPtr *string
	if params.SessionID != "" {
		sidPtr = &params.SessionID
	}

	if err := st.EnableTrace(params.Path, sidPtr, store.EnableOpts{}); err != nil {
		return fmt.Sprintf("\u274c Failed to enable %s: %v", params.Path, err), nil
	}

	trace.InvalidateCache(params.SessionID)
	storeRows, _ := st.GetConfig(sidPtr)
	traceRows := make([]trace.ConfigRow, len(storeRows))
	for i, r := range storeRows {
		traceRows[i] = trace.ConfigRow{TracePath: r.TracePath, Enabled: r.Enabled, Owner: r.Owner}
	}
	trace.CompileConfig(traceRows, sidPtr)

	return fmt.Sprintf("\u2705 Point %s enabled", params.Path), nil
}
