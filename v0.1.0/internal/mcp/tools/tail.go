package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/store"
)

// TailTool требует доступ к курсорам MCPHandler.
// Мы используем интерфейс TailCursorStore для получения/установки last_id.
type TailCursorStore interface {
	TailCursor(sessionID string) int64
	SetTailCursor(sessionID string, lastID int64)
}

// tailContextKey — ключ для передачи TailCursorStore через context.
type tailContextKey struct{}

// WithTailStore сохраняет TailCursorStore в контексте.
func WithTailStore(ctx context.Context, ts TailCursorStore) context.Context {
	return context.WithValue(ctx, tailContextKey{}, ts)
}

type TraceTailTool struct{}

func (t *TraceTailTool) Name() string { return "trace_tail" }

func (t *TraceTailTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_tail",
		Description: "Streaming read: show new log entries since last read (cursor-based).",
		InputSchema: JSONSchema{
			Type: "object",
			Properties: map[string]PropSpec{
				"session_id": stringProp("Session ID (optional)"),
				"lines":      intProp("Max lines to return (default: 50)"),
			},
		},
	}
}

func (t *TraceTailTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	var params struct {
		SessionID string `json:"session_id"`
		Lines     int    `json:"lines"`
	}
	if err := parseArgs(args, &params); err != nil {
		return fmt.Sprintf("Invalid argument: %v (check parameter types)", err), nil
	}
	if params.Lines <= 0 {
		params.Lines = 50
	}

	var sidPtr *string
	if params.SessionID != "" {
		sidPtr = &params.SessionID
	}

	lastID := int64(0)
	if ts, ok := ctx.Value(tailContextKey{}).(TailCursorStore); ok {
		lastID = ts.TailCursor(params.SessionID)
	}

	logs, err := st.ReadTraceLog(sidPtr, 0, params.Lines, nil, nil, nil)
	if err != nil {
		return fmt.Sprintf("Failed to read trace log: %v", err), nil
	}

	newLogs := []store.TraceEntry{}
	for _, entry := range logs {
		if entry.ID > lastID {
			newLogs = append(newLogs, entry)
		}
	}

	if len(newLogs) == 0 {
		return "No new entries", nil
	}

	// Update cursor
	maxID := int64(0)
	for _, entry := range newLogs {
		if entry.ID > maxID {
			maxID = entry.ID
		}
	}
	if ts, ok := ctx.Value(tailContextKey{}).(TailCursorStore); ok {
		ts.SetTailCursor(params.SessionID, maxID)
	}

	result := fmt.Sprintf("New entries: %d\n", len(newLogs))
	for _, entry := range newLogs[:min(len(newLogs), params.Lines)] {
		result += fmt.Sprintf("\n[%s] %s", entry.CreatedAt, entry.TracePath)
		if entry.Data != nil {
			dataJSON, _ := json.Marshal(entry.Data)
			dataStr := string(dataJSON)
			if len(dataStr) > 200 {
				dataStr = dataStr[:200]
			}
			result += fmt.Sprintf("\n  data: %s", dataStr)
		}
	}
	return result, nil
}

// min возвращает минимум двух int.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

