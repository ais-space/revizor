package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/store"
	"github.com/ais-platform/ais_products/revizor/internal/trace"
)

type TraceKillTool struct{}

func (t *TraceKillTool) Name() string { return "trace_kill" }

func (t *TraceKillTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_kill",
		Description: "Kill one or all active trace sessions — force expire with cleanup. If session_id is 'all' or empty, kills all sessions.",
		InputSchema: JSONSchema{
			Type: "object",
			Properties: map[string]PropSpec{
				"session_id": stringProp("Session ID to kill, or 'all' to kill all sessions (default: 'all')"),
			},
		},
	}
}

func (t *TraceKillTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	var params struct {
		SessionID string `json:"session_id"`
	}
	if err := parseArgs(args, &params); err != nil {
		return fmt.Sprintf("Invalid argument: %v (check parameter types)", err), nil
	}

	// Если session_id не указан или "all" — убиваем все сессии
	if params.SessionID == "" || params.SessionID == "all" {
		sessions, err := st.GetActiveSessions()
		if err != nil {
			return fmt.Sprintf("❌ Error getting sessions: %v", err), nil
		}
		if len(sessions) == 0 {
			return "No active sessions to kill", nil
		}

		killed := 0
		for _, s := range sessions {
			if err := st.ExpireSession(s.SessionID); err == nil {
				trace.InvalidateCache(s.SessionID)
				killed++
			}
		}

		return fmt.Sprintf("✅ OK: killed %d/%d sessions", killed, len(sessions)), nil
	}

	// Убиваем конкретную сессию
	if err := st.ExpireSession(params.SessionID); err != nil {
		return fmt.Sprintf("❌ Failed to kill session %s: %v", params.SessionID, err), nil
	}

	trace.InvalidateCache(params.SessionID)
	return fmt.Sprintf("✅ OK: session %s killed", params.SessionID), nil
}
