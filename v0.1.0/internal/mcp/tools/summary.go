package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/store"
)

// TraceSessionSummaryTool — инструмент #24: сводка по сессии трассировки.
// Используется оркестраторами (Прораб/Foreman) для получения метрик сессии.
type TraceSessionSummaryTool struct{}

func (t *TraceSessionSummaryTool) Name() string { return "trace_session_summary" }

func (t *TraceSessionSummaryTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_session_summary",
		Description: "Сводка по сессии трассировки: количество событий, уникальных путей, время последнего события, статус сессии. Для интеграции с оркестраторами (Прораб).",
		InputSchema: JSONSchema{
			Type: "object",
			Properties: map[string]PropSpec{
				"session_id": stringProp("ID сессии трассировки (обязательно)"),
			},
			Required: []string{"session_id"},
		},
	}
}

func (t *TraceSessionSummaryTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	var params struct {
		SessionID string `json:"session_id"`
	}
	if err := parseArgs(args, &params); err != nil {
		return fmt.Sprintf("Invalid argument: %v (check parameter types)", err), nil
	}

	if params.SessionID == "" {
		return "Session ID is required — provide the session_id to summarize.", nil
	}

	// Получаем статистику сессии
	stats, err := st.GetTraceStats(&params.SessionID)
	if err != nil {
		return fmt.Sprintf("Error reading session stats: %v", err), nil
	}

	// Проверяем статус сессии: active / expired
	sessions, err := st.GetActiveSessions()
	if err != nil {
		return fmt.Sprintf("Error reading sessions: %v", err), nil
	}

	status := "unknown"
	for _, s := range sessions {
		if s.SessionID == params.SessionID {
			status = "active"
			if s.ExpiresAt != nil {
				status += fmt.Sprintf(" (expires: %s)", *s.ExpiresAt)
			}
			break
		}
	}
	if status == "unknown" {
		status = "expired"
	}

	result := fmt.Sprintf("Session: %s\n", params.SessionID)
	result += fmt.Sprintf("Status: %s\n", status)
	result += fmt.Sprintf("Total events: %d\n", stats.TotalEvents)
	result += fmt.Sprintf("Unique paths: %d\n", stats.UniquePaths)
	if stats.LastEventAt != nil {
		result += fmt.Sprintf("Last event: %s\n", *stats.LastEventAt)
	} else {
		result += "Last event: none\n"
	}

	return result, nil
}
