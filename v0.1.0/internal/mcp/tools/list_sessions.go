package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/store"
)

type TraceListSessionsTool struct{}

func (t *TraceListSessionsTool) Name() string { return "trace_list_sessions" }

func (t *TraceListSessionsTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_list_sessions",
		Description: "Show all active trace sessions.",
		InputSchema: JSONSchema{
			Type:       "object",
			Properties: map[string]PropSpec{},
		},
	}
}

func (t *TraceListSessionsTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	sessions, err := st.GetActiveSessions()
	if err != nil {
		return fmt.Sprintf("Failed to list sessions: %v", err), nil
	}
	if len(sessions) == 0 {
		return "No active sessions", nil
	}

	result := fmt.Sprintf("Active sessions: %d\n", len(sessions))
	for _, s := range sessions {
		result += fmt.Sprintf("\n  \U0001f4cb %s\n", s.SessionID)
		result += fmt.Sprintf("     Owner: %s\n", s.Owner)
		if s.Description != nil && *s.Description != "" {
			result += fmt.Sprintf("     Description: %s\n", *s.Description)
		}
		result += fmt.Sprintf("     Created: %s\n", s.CreatedAt)
	}
	return result, nil
}
