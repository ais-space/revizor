package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/store"
)

type TracePresetSetTool struct{}

func (t *TracePresetSetTool) Name() string { return "trace_preset_set" }

func (t *TracePresetSetTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_preset_set",
		Description: "Create or update a preset in the database. Pass comma-separated paths. Use '!' prefix for exclude paths. Changes persist across restarts.",
		InputSchema: JSONSchema{
			Type: "object",
			Properties: map[string]PropSpec{
				"name":        stringProp("Preset name (e.g., debug_my_module)"),
				"paths":       stringProp("Comma-separated trace paths (e.g., mymodule.**, !mymodule.verbose)"),
				"description": stringProp("Preset description (optional)"),
			},
			Required: []string{"name", "paths"},
		},
	}
}

func (t *TracePresetSetTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	var params struct {
		Name        string `json:"name"`
		Paths       string `json:"paths"`
		Description string `json:"description"`
	}
	if err := parseArgs(args, &params); err != nil {
		return fmt.Sprintf("Invalid argument: %v (check parameter types)", err), nil
	}
	if params.Name == "" {
		return "Preset name is required — provide a name for the preset.", nil
	}
	if params.Paths == "" {
		return "Paths are required — provide comma-separated paths for the preset.", nil
	}

	var pathList []string
	for _, p := range strings.Split(params.Paths, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			pathList = append(pathList, p)
		}
	}
	if len(pathList) == 0 {
		return "No valid paths specified — check path format (lowercase, dots, max 120 chars).", nil
	}

	if err := st.SetPreset(params.Name, params.Description, pathList); err != nil {
		return fmt.Sprintf("\u274c Failed to save preset '%s': %v", params.Name, err), nil
	}

	result := fmt.Sprintf("\u2705 Preset '%s' saved (%d paths)", params.Name, len(pathList))
	if params.Description != "" {
		result += fmt.Sprintf("\nDescription: %s", params.Description)
	}
	result += "\nPaths:"
	for _, p := range pathList {
		result += fmt.Sprintf("\n  %s", p)
	}
	result += "\n\nUse trace_start with preset name '%s' to activate."
	result = fmt.Sprintf(result, params.Name)
	return result, nil
}
