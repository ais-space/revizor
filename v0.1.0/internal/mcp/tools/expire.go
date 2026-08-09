package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/store"
	"github.com/ais-platform/ais_products/revizor/internal/trace"
)

type TraceExpireTool struct{}

func (t *TraceExpireTool) Name() string { return "trace_expire" }

func (t *TraceExpireTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_expire",
		Description: "Force-expire a session: disable all its trace points and set expiration to now.",
		InputSchema: JSONSchema{
			Type: "object",
			Properties: map[string]PropSpec{
				"session_id": stringProp("Session ID to expire"),
			},
			Required: []string{"session_id"},
		},
	}
}

func (t *TraceExpireTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	var params struct {
		SessionID string `json:"session_id"`
	}
	if err := parseArgs(args, &params); err != nil {
		return fmt.Sprintf("Invalid argument: %v (check parameter types)", err), nil
	}
	if params.SessionID == "" {
		return "\u274c No session_id specified", nil
	}

	if err := st.ExpireSession(params.SessionID); err != nil {
		return fmt.Sprintf("\u274c Failed to expire session %s: %v", params.SessionID, err), nil
	}

	trace.InvalidateCache(params.SessionID)

	return fmt.Sprintf("\u2705 Session %s expired. All trace points disabled.", params.SessionID), nil
}
