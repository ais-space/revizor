package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/license"
	"github.com/ais-platform/ais_products/revizor/internal/store"
)

// Version — текущая версия бинарника. Устанавливается через SetVersion() из main.go.
var Version = "0.1.0"

// SetVersion устанавливает версию бинарника. Вызывается из main.go при старте.
func SetVersion(v string) {
	if v != "" {
		Version = v
	}
}

// TraceUpdateCheckTool проверяет наличие обновлений бинарника Ревизора (v55, Фаза 6).
// Агент вызывает его для проверки и уведомляет пользователя при необходимости.
type TraceUpdateCheckTool struct {
	license *license.License
}

func (t *TraceUpdateCheckTool) Name() string        { return "trace_update_check" }
func (t *TraceUpdateCheckTool) SetLicense(lic *license.License) { t.license = lic }

func (t *TraceUpdateCheckTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_update_check",
		Description: "Check for Revizor binary updates. Compares the current version with the latest available from the license server. Use this to notify the user when an update is available and trigger the upgrade.",
		InputSchema: JSONSchema{
			Type:       "object",
			Properties: map[string]PropSpec{},
			Required:   []string{},
		},
	}
}

type updateCheckResponse struct {
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version"`
	UpdateAvailable bool   `json:"update_available"`
}

func (t *TraceUpdateCheckTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	serverURL := ""
	if cfg != nil {
		serverURL = cfg.Server.LicenseServerURL
	}
	if serverURL == "" {
		return "Update check unavailable: license server URL not configured.", nil
	}

	client := &http.Client{Timeout: 10 * time.Second}
	url := fmt.Sprintf("%s/api/v1/license/update-check?version=%s&product=revizor", serverURL, Version)
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Sprintf("Update check failed: %v", err), nil
	}
	defer resp.Body.Close()

	var result updateCheckResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Sprintf("Update check failed: invalid response: %v", err), nil
	}

	if result.UpdateAvailable {
		return fmt.Sprintf(
			"Update available!\n  Current version: %s\n  Latest version:  %s\n\nTo upgrade, run: curl -s https://ais-platform.dev/dl/revizor | sh",
			result.CurrentVersion, result.LatestVersion,
		), nil
	}

	return fmt.Sprintf("Revizor is up to date (v%s).", result.CurrentVersion), nil
}
