package tools

import (
	"context"
	"encoding/json"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/store"
)

// DebugLogSetter — интерфейс для переключения отладочного логирования MCP-запросов.
type DebugLogSetter interface {
	SetDebugLog(on bool)
	IsDebugLog() bool
}

// TraceDebugLogTool — скрытый инструмент для включения/выключения отладочного логирования.
type TraceDebugLogTool struct {
	debugSetter DebugLogSetter
}

func (t *TraceDebugLogTool) Name() string { return "trace_debug_log" }

func (t *TraceDebugLogTool) SetDebugSetter(ds DebugLogSetter) {
	t.debugSetter = ds
}

func (t *TraceDebugLogTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_debug_log",
		Description: "Internal debug toggle for MCP request/response logging.",
		InputSchema: JSONSchema{
			Type:       "object",
			Properties: map[string]PropSpec{},
		},
	}
}

func (t *TraceDebugLogTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	if t.debugSetter == nil {
		return "Debug log: unavailable (no handler reference).", nil
	}

	if t.debugSetter.IsDebugLog() {
		t.debugSetter.SetDebugLog(false)
		return "Debug log: OFF. No more requests will be written to logs/revizor_mcp_debug.log.", nil
	}

	t.debugSetter.SetDebugLog(true)
	return "Debug log: ON. All MCP requests and responses will be written to logs/revizor_mcp_debug.log.", nil
}
