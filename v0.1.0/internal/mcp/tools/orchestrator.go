package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/store"
)

// TraceOrchestratorEventsTool — инструмент #25: таймлайн событий оркестратора.
// Фильтрует trace-события по task_id в orchestrator_meta, возвращает хронологию.
type TraceOrchestratorEventsTool struct{}

func (t *TraceOrchestratorEventsTool) Name() string { return "trace_orchestrator_events" }

func (t *TraceOrchestratorEventsTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_orchestrator_events",
		Description: "Таймлайн trace-событий для конкретной задачи оркестратора (Прораба). Фильтрует по task_id в поле orchestrator_meta.",
		InputSchema: JSONSchema{
			Type: "object",
			Properties: map[string]PropSpec{
				"task_id":    stringProp("ID задачи оркестратора (обязательно, фильтр по orchestrator_meta.task_id)"),
				"window_sec": intProp("Временное окно в секундах (по умолчанию 300 = 5 минут)"),
			},
			Required: []string{"task_id"},
		},
	}
}

// orchestratorEvent — событие в таймлайне оркестратора (алиас для store.OrchestratorEvent).
type orchestratorEvent = store.OrchestratorEvent

func (t *TraceOrchestratorEventsTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	var params struct {
		TaskID    string `json:"task_id"`
		WindowSec int    `json:"window_sec"`
	}
	if err := parseArgs(args, &params); err != nil {
		return fmt.Sprintf("Invalid argument: %v (check parameter types)", err), nil
	}

	if params.TaskID == "" {
		return "Task ID is required — provide the orchestrator task_id to filter events.", nil
	}

	if params.WindowSec <= 0 {
		params.WindowSec = 300
	}

	// Используем GetOrchestratorEvents — специфичный метод для SQLiteStore.
	events, err := getOrchestratorEvents(st, params.TaskID, params.WindowSec)
	if err != nil {
		return fmt.Sprintf("Failed to read orchestrator events: %v", err), nil
	}

	if len(events) == 0 {
		windowStart := time.Now().UTC().Add(-time.Duration(params.WindowSec) * time.Second)
		return fmt.Sprintf("No orchestrator events found for task '%s' in the last %ds (since %s).\n"+
			"Make sure the agent is writing to orchestrator_meta with task_id=%s.",
			params.TaskID, params.WindowSec,
			windowStart.Format("2006-01-02T15:04:05Z"),
			params.TaskID), nil
	}

	result := fmt.Sprintf("Orchestrator timeline for task '%s':\n", params.TaskID)
	result += fmt.Sprintf("Events: %d\n", len(events))
	result += fmt.Sprintf("Window: %ds\n\n", params.WindowSec)

	for _, ev := range events {
		result += fmt.Sprintf("[%s] %s | %s",
			ev.CreatedAt, formatEventType(ev.EventType), ev.TracePath)
		if ev.Data != "" && ev.Data != "{}" {
			result += fmt.Sprintf(" | %s", ev.Data)
		}
		result += "\n"
	}

	return result, nil
}

// getOrchestratorEvents выполняет запрос событий оркестратора.
func getOrchestratorEvents(st store.TraceStore, taskID string, windowSec int) ([]orchestratorEvent, error) {
	// Прямой доступ через SQLiteStore.QueryOrchestratorEvents
	type sqliteAccessor interface {
		QueryOrchestratorEvents(taskID string, windowSec int) ([]store.OrchestratorEvent, error)
	}

	if accessor, ok := st.(sqliteAccessor); ok {
		return accessor.QueryOrchestratorEvents(taskID, windowSec)
	}

	// Fallback: ручная фильтрация через ReadTraceLog
	sid := ""
	entries, err := st.ReadTraceLog(&sid, 0, 1000, nil, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("read trace log: %w", err)
	}

	cutoff := time.Now().UTC().Add(-time.Duration(windowSec) * time.Second)
	var events []orchestratorEvent
	for _, e := range entries {
		if e.OrchestratorMeta == nil {
			continue
		}
		tid, ok := e.OrchestratorMeta["task_id"].(string)
		if !ok || tid != taskID {
			continue
		}
		createdAt, errParse := time.Parse("2006-01-02T15:04:05Z", e.CreatedAt)
		if errParse != nil {
			createdAt, errParse = time.Parse("2006-01-02 15:04:05", e.CreatedAt)
			if errParse != nil {
				continue
			}
		}
		if createdAt.Before(cutoff) {
			continue
		}
		eventType := extractOrchestratorEventType(e.OrchestratorMeta)
		dataStr := ""
		if e.Data != nil {
			if b, errM := json.Marshal(e.Data); errM == nil && string(b) != "null" {
				dataStr = string(b)
			}
		}
		events = append(events, orchestratorEvent{
			CreatedAt: e.CreatedAt,
			TracePath: e.TracePath,
			EventType: eventType,
			Data:      dataStr,
		})
	}
	return events, nil
}

// extractOrchestratorEventType извлекает тип события из метаданных оркестратора.
func extractOrchestratorEventType(meta map[string]any) string {
	if meta == nil {
		return "unknown"
	}
	if et, ok := meta["event_type"].(string); ok {
		return et
	}
	if _, ok := meta["step"]; ok {
		return "step"
	}
	if _, ok := meta["retry"]; ok {
		return "retry"
	}
	return "unknown"
}

// formatEventType форматирует тип события для вывода.
func formatEventType(eventType string) string {
	switch eventType {
	case "step.enter":
		return "▶ ENTER"
	case "step.success":
		return "✅ SUCCESS"
	case "step.failed":
		return "❌ FAILED"
	case "agent.heartbeat":
		return "💓 HEARTBEAT"
	case "task.blocked":
		return "🚫 BLOCKED"
	case "retry.attempt":
		return "🔄 RETRY"
	default:
		return "  " + strings.ToUpper(eventType)
	}
}

// extractOrchestratorEventType извлекает тип события из метаданных оркестратора.
