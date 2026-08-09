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

// TraceLicenseRenewTool — ручное подтверждение лицензии агентом.
type TraceLicenseRenewTool struct {
	license       *license.License
	lastHeartbeat *time.Time
}

func (t *TraceLicenseRenewTool) Name() string { return "trace_license_renew" }

func (t *TraceLicenseRenewTool) SetLicense(lic *license.License)     { t.license = lic }
func (t *TraceLicenseRenewTool) SetLastHeartbeat(tm *time.Time)      { t.lastHeartbeat = tm }

func (t *TraceLicenseRenewTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_license_renew",
		Description: "Renew the license heartbeat on the AIS licensing server. Must be called periodically by the AI agent (daily recommended) to keep Pro features active. After 3 missed days, the licensing server may degrade the key to Community mode.",
		InputSchema: JSONSchema{
			Type:       "object",
			Properties: map[string]PropSpec{},
		},
	}
}

func (t *TraceLicenseRenewTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	if t.license == nil {
		return `{"status":"community","message":"Running in Community mode. No license configured.","effective_exp":null}`, nil
	}

	if t.license.Tier == "enterprise" {
		return fmt.Sprintf(`{"status":"enterprise","message":"Enterprise license %s — offline mode, no heartbeat required.","effective_exp":null}`, t.license.KID), nil
	}

	if t.license.IID == "" {
		return fmt.Sprintf(`{"status":"pending_activation","message":"License %s not yet activated (IID missing). The server will retry activation on next restart.","effective_exp":null}`, t.license.KID), nil
	}

	machineID, err := license.MachineID()
	if err != nil {
		return fmt.Sprintf(`{"status":"error","message":"Cannot determine machine_id: %v"}`, err), nil
	}

	serverURL := cfg.Server.LicenseServerURL
	if serverURL == "" {
		serverURL = "https://ais-platform.dev"
	}

	client := license.NewHeartbeatClient(serverURL)
	resp, err := client.SendHeartbeat(ctx, license.HeartbeatRequest{
		KID:       t.license.KID,
		IID:       t.license.IID,
		MachineID: machineID,
		Version:   "0.1.0",
	})
	if err != nil {
		expStr := "none"
		if t.license.Exp > 0 {
			expStr = time.Unix(t.license.Exp, 0).Format(time.RFC3339)
		}
		return fmt.Sprintf(`{"status":"heartbeat_failed","message":"Heartbeat to licensing server failed: %v","effective_exp":%q}`, err, expStr), nil
	}

	// Обновляем время последнего heartbeat
	if t.lastHeartbeat != nil {
		*t.lastHeartbeat = time.Now()
	}

	return fmt.Sprintf(`{"status":"ok","message":"Heartbeat confirmed. License is active.","expires_in_sec":%d}`, resp.ExpiresIn), nil
}
