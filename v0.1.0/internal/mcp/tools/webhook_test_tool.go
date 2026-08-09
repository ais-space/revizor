package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/store"
	"github.com/ais-platform/ais_products/revizor/internal/webhook"
)

// TraceWebhookTestTool — MCP-инструмент trace_webhook_test (REV-W-001).
type TraceWebhookTestTool struct {
	dispatcher *webhook.Dispatcher
}

func (t *TraceWebhookTestTool) Name() string { return "trace_webhook_test" }

func (t *TraceWebhookTestTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_webhook_test",
		Description: "Send a test ping to a specific webhook (REV-W-001). Uses trace_path: 'revizor.webhook.test' with data: {test: true}.",
		InputSchema: JSONSchema{
			Type: "object",
			Properties: map[string]PropSpec{
				"webhook_id": stringProp("Webhook ID to test (required)"),
			},
			Required: []string{"webhook_id"},
		},
	}
}

func (t *TraceWebhookTestTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	var params struct {
		WebhookID string `json:"webhook_id"`
	}
	if err := parseArgs(args, &params); err != nil {
		return fmt.Sprintf("Invalid argument: %v (check parameter types)", err), nil
	}
	if params.WebhookID == "" {
		return "webhook_id is required", nil
	}
	if t.dispatcher == nil {
		return "No webhooks configured. Add webhooks section to revizor.yaml.", nil
	}

	result := t.dispatcher.Test(params.WebhookID)
	out, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprintf("Failed to marshal test result: %v", err), nil
	}
	return string(out), nil
}

// SetDispatcher устанавливает диспетчер webhook'ов.
func (t *TraceWebhookTestTool) SetDispatcher(d *webhook.Dispatcher) {
	t.dispatcher = d
}
