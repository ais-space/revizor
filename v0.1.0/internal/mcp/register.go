package mcp

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/license"
	"github.com/ais-platform/ais_products/revizor/internal/mcp/tools"
	"github.com/ais-platform/ais_products/revizor/internal/store"
	"github.com/ais-platform/ais_products/revizor/internal/webhook"
)

// toolAdapter адаптирует tools.Tool → mcp.ToolHandler.
type toolAdapter struct {
	t                 tools.Tool
	license           *license.License
	lastHeartbeatTime *time.Time
}

func (a *toolAdapter) Name() string { return a.t.Name() }

func (a *toolAdapter) Schema() MCPTool {
	s := a.t.Schema()
	props := make(map[string]JSONSchemaProp, len(s.InputSchema.Properties))
	for k, v := range s.InputSchema.Properties {
		props[k] = JSONSchemaProp{Type: v.Type, Description: v.Description}
	}
	return MCPTool{
		Name:        s.Name,
		Description: s.Description,
		InputSchema: JSONSchema{
			Type:       s.InputSchema.Type,
			Properties: props,
			Required:   s.InputSchema.Required,
		},
	}
}

func (a *toolAdapter) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	// Передаём лицензию в инструмент если он поддерживает LicenseSetter
	if setter, ok := a.t.(tools.LicenseSetter); ok {
		setter.SetLicense(a.license)
	}
	// Передаём время последнего heartbeat
	if setter, ok := a.t.(tools.LastHeartbeatSetter); ok {
		setter.SetLastHeartbeat(a.lastHeartbeatTime)
	}
	return a.t.Execute(ctx, args, st, cfg)
}

// registerTools регистрирует все MCP-инструменты в handler.
func (h *MCPHandler) registerTools() {
	var lastHb time.Time
	for _, t := range tools.All() {
		adapter := &toolAdapter{t: t, license: h.license, lastHeartbeatTime: &lastHb}
		h.tools[t.Name()] = adapter

		// Если инструмент ещё не имеет лицензии — передаём
		if setter, ok := t.(tools.LicenseSetter); ok {
			setter.SetLicense(h.license)
		}
		// Debug log setter
		if dlt, ok := t.(*tools.TraceDebugLogTool); ok {
			dlt.SetDebugSetter(h)
		}
	}
}

// SetWebhookDispatcher передаёт диспетчер webhook'ов в соответствующие инструменты (REV-W-001).
func (h *MCPHandler) SetWebhookDispatcher(d *webhook.Dispatcher) {
	for _, t := range tools.All() {
		switch tool := t.(type) {
		case *tools.TraceWebhookListTool:
			tool.SetDispatcher(d)
		case *tools.TraceWebhookTestTool:
			tool.SetDispatcher(d)
		}
	}
}
