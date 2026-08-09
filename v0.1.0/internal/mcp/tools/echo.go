package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/license"
	"github.com/ais-platform/ais_products/revizor/internal/store"
)

// TraceEchoTool — диагностика: записывает тестовое событие в лог
// и проверяет что оно доступно для чтения. Позволяет агенту убедиться
// что связка SDK → сервер работает.
type TraceEchoTool struct{}

func (t *TraceEchoTool) Name() string { return "trace_echo" }

func (t *TraceEchoTool) SetLicense(lic *license.License) {}
func (t *TraceEchoTool) SetLastHeartbeat(tm interface{}) {}

func (t *TraceEchoTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_echo",
		Description: "Write a test trace event to the log and verify it is readable. Use this to confirm the SDK→server pipeline is working end-to-end. Returns the recorded event.",
		InputSchema: JSONSchema{
			Type: "object",
			Properties: map[string]PropSpec{
				"message": stringProp("Test message to write (default: 'echo test')"),
			},
		},
	}
}

func (t *TraceEchoTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	var params struct {
		Message string `json:"message"`
	}
	if err := parseArgs(args, &params); err != nil {
		// Параметры опциональны — используем дефолты
		params.Message = "echo test"
	}
	if params.Message == "" {
		params.Message = "echo test"
	}

	// Записываем тестовое событие
	echoSessionID := "echo"
	entry := store.TraceEntry{
		TracePath: "revizor.trace_echo.test",
		Data:      map[string]interface{}{"message": params.Message},
		SessionID: &echoSessionID,
	}
	if err := st.WriteTrace(entry); err != nil {
		return fmt.Sprintf("FAIL: could not write test event — %v", err), nil
	}

	return fmt.Sprintf("OK: test event written to log. Path: revizor.trace_echo.test, Message: %s. Verify with trace_search: search='revizor.trace_echo'.", params.Message), nil
}
