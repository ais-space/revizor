package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/store"
	"github.com/ais-platform/ais_products/revizor/internal/trace"
)

type TraceDisableTool struct{}

func (t *TraceDisableTool) Name() string { return "trace_disable" }

func (t *TraceDisableTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_disable",
		Description: "Disable a trace point.",
		InputSchema: JSONSchema{
			Type: "object",
			Properties: map[string]PropSpec{
				"path":       stringProp("Trace path to disable (e.g., auth.**)"),
				"session_id": stringProp("Session ID (optional)"),
			},
			Required: []string{"path"},
		},
	}
}

func (t *TraceDisableTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
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

	if err := st.DisableTrace(params.Path, sidPtr); err != nil {
		return fmt.Sprintf("\u274c Failed to disable %s: %v", params.Path, err), nil
	}

	trace.InvalidateCache(params.SessionID)
	storeRows, _ := st.GetConfig(sidPtr)
	traceRows := make([]trace.ConfigRow, len(storeRows))
	for i, r := range storeRows {
		traceRows[i] = trace.ConfigRow{TracePath: r.TracePath, Enabled: r.Enabled, Owner: r.Owner}
	}
	trace.CompileConfig(traceRows, sidPtr)

	return fmt.Sprintf("\u2705 Point %s disabled", params.Path), nil
}
