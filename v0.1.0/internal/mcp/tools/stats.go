package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/store"
)

type TraceStatsTool struct{}

func (t *TraceStatsTool) Name() string { return "trace_stats" }

func (t *TraceStatsTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_stats",
		Description: "Show session metrics: total events, unique paths, last event timestamp.",
		InputSchema: JSONSchema{
			Type: "object",
			Properties: map[string]PropSpec{
				"session_id": stringProp("Session ID (optional)"),
			},
		},
	}
}

func (t *TraceStatsTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
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

	stats, err := st.GetTraceStats(sidPtr)
	if err != nil {
		return fmt.Sprintf("Failed to read statistics: %v", err), nil
	}

	result := ""
	if params.SessionID != "" {
		result += fmt.Sprintf("Session: %s\n", params.SessionID)
	}
	result += fmt.Sprintf("Total events: %d\n", stats.TotalEvents)
	result += fmt.Sprintf("Unique paths: %d\n", stats.UniquePaths)
	if stats.LastEventAt != nil {
		result += fmt.Sprintf("Last event: %s", *stats.LastEventAt)
	}
	return result, nil
}
