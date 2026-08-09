package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/store"
	"github.com/ais-platform/ais_products/revizor/internal/trace"
)

type TraceConfigTool struct{}

func (t *TraceConfigTool) Name() string { return "trace_config" }

func (t *TraceConfigTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_config",
		Description: "Show the current compiled config for a session — list of active trace paths.",
		InputSchema: JSONSchema{
			Type: "object",
			Properties: map[string]PropSpec{
				"session_id": stringProp("Session ID (optional, default: global)"),
			},
		},
	}
}

func (t *TraceConfigTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	var params struct {
		SessionID string `json:"session_id"`
	}
	if err := parseArgs(args, &params); err != nil {
		return fmt.Sprintf("Invalid argument: %v (check parameter types)", err), nil
	}

	var sidPtr *string
	if params.SessionID != "" {
		sidPtr = &params.SessionID
	}

	storeRows, err := st.GetConfig(sidPtr)
	if err != nil {
		return fmt.Sprintf("Failed to read trace config: %v", err), nil
	}

	traceRows := make([]trace.ConfigRow, len(storeRows))
	for i, r := range storeRows {
		traceRows[i] = trace.ConfigRow{TracePath: r.TracePath, Enabled: r.Enabled, Owner: r.Owner}
	}
	configMap, err := trace.CompileConfig(traceRows, sidPtr)
	if err != nil {
		return fmt.Sprintf("Failed to compile trace config: %v", err), nil
	}
	if len(configMap) == 0 {
		return "Configuration is empty (no active points)", nil
	}

	// Sort keys
	keys := make([]string, 0, len(configMap))
	for k := range configMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	lines := []string{fmt.Sprintf("Active points: %d", len(configMap)), ""}
	for _, k := range keys {
		if configMap[k] {
			lines = append(lines, fmt.Sprintf("  \u2705 %s", k))
		} else {
			lines = append(lines, fmt.Sprintf("  \u274c %s", k))
		}
	}

	result := ""
	for i, l := range lines {
		if i > 0 {
			result += "\n"
		}
		result += l
	}
	return result, nil
}
