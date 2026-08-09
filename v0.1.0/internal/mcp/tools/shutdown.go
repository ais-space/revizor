package tools

import (
	"context"
	"encoding/json"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/store"
)

// ShutdownFunc — функция, вызываемая для graceful остановки процесса.
type ShutdownFunc func() error

// TraceShutdownTool — инструмент для graceful остановки Ревизора.
type TraceShutdownTool struct {
	shutdown ShutdownFunc
}

func (t *TraceShutdownTool) Name() string { return "trace_shutdown" }

func (t *TraceShutdownTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_shutdown",
		Description: "Gracefully stop the Revizor process. Flushes all buffers, closes database connections, and exits cleanly. Available via HTTP POST /api/v1/trace/shutdown, MCP tool trace_shutdown, CLI, or prompt adapter.",
		InputSchema: JSONSchema{
			Type:       "object",
			Properties: map[string]PropSpec{},
		},
	}
}

// SetShutdown устанавливает функцию graceful остановки.
func (t *TraceShutdownTool) SetShutdown(fn ShutdownFunc) {
	t.shutdown = fn
}

func (t *TraceShutdownTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	if t.shutdown == nil {
		return "Shutdown is not available in stdio-only mode. Use HTTP server mode (revizor serve) for shutdown.", nil
	}

	if err := t.shutdown(); err != nil {
		return "Failed to initiate shutdown: " + err.Error(), nil
	}

	return "Revizor is shutting down gracefully...", nil
}
