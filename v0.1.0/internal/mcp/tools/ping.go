package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/license"
	"github.com/ais-platform/ais_products/revizor/internal/store"
)

// TracePingTool — безопасный тест соединения с информацией о статусе лицензии.
type TracePingTool struct {
	license       *license.License
	lastHeartbeat *time.Time
}

func (t *TracePingTool) Name() string { return "trace_ping" }

func (t *TracePingTool) SetLicense(lic *license.License)     { t.license = lic }
func (t *TracePingTool) SetLastHeartbeat(tm *time.Time)      { t.lastHeartbeat = tm }

func (t *TracePingTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_ping",
		Description: "Safe connectivity test with license status. Returns 'pong' with license tier and heartbeat status. Use as the FIRST call to verify MCP access and check if license renewal is needed.",
		InputSchema: JSONSchema{
			Type:       "object",
			Properties: map[string]PropSpec{},
		},
	}
}

func (t *TracePingTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	verStr := fmt.Sprintf("v%s", Version)
	docsHint := "\nAgent: call trace_get_instructions for the full debugging guide"

	if t.license == nil {
		return fmt.Sprintf("pong (Community mode — no license, %s)%s", verStr, docsHint), nil
	}

	if t.license.Tier == "enterprise" {
		return fmt.Sprintf("pong (Enterprise license %s — offline perpetual, %s)%s", t.license.KID, verStr, docsHint), nil
	}

	if t.license.IID == "" {
		return fmt.Sprintf("pong (Pro license %s — activation pending, %s)%s", t.license.KID, verStr, docsHint), nil
	}

	if t.lastHeartbeat == nil || t.lastHeartbeat.IsZero() {
		expStr := "none"
		if t.license.Exp > 0 {
			expStr = time.Unix(t.license.Exp, 0).Format("2006-01-02")
		}
		return fmt.Sprintf("pong (Pro license %s — HEARTBEAT OVERDUE, run trace_license_renew. Expires: %s, %s)%s", t.license.KID, expStr, verStr, docsHint), nil
	}

	sinceLast := time.Since(*t.lastHeartbeat)
	if sinceLast > 24*time.Hour {
		expStr := "none"
		if t.license.Exp > 0 {
			expStr = time.Unix(t.license.Exp, 0).Format("2006-01-02")
		}
		return fmt.Sprintf("pong (Pro license %s — HEARTBEAT OVERDUE (%v since last), run trace_license_renew. Expires: %s, %s)%s", t.license.KID, sinceLast.Round(time.Hour), expStr, verStr, docsHint), nil
	}

	return fmt.Sprintf("pong (Pro license %s — heartbeat OK, last: %s, %s)%s", t.license.KID, t.lastHeartbeat.Format(time.RFC3339), verStr, docsHint), nil
}
