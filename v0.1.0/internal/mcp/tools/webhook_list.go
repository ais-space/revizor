package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/store"
	"github.com/ais-platform/ais_products/revizor/internal/webhook"
)

// TraceWebhookListTool — MCP-инструмент trace_webhook_list (REV-W-001).
type TraceWebhookListTool struct {
	dispatcher *webhook.Dispatcher
}

func (t *TraceWebhookListTool) Name() string { return "trace_webhook_list" }

func (t *TraceWebhookListTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_webhook_list",
		Description: "List registered webhook notifications and their delivery status (REV-W-001). Returns id, url, path_filter, enabled, last_delivery, last_status for each webhook.",
		InputSchema: JSONSchema{
			Type:       "object",
			Properties: map[string]PropSpec{},
			Required:   []string{},
		},
	}
}

func (t *TraceWebhookListTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	if t.dispatcher == nil {
		return "{\"webhooks\": []}", nil
	}

	list := t.dispatcher.List()
	out, err := json.Marshal(map[string]any{"webhooks": list})
	if err != nil {
		return fmt.Sprintf("Failed to marshal webhook list: %v", err), nil
	}
	return string(out), nil
}

// SetDispatcher устанавливает диспетчер webhook'ов.
func (t *TraceWebhookListTool) SetDispatcher(d *webhook.Dispatcher) {
	t.dispatcher = d
}
