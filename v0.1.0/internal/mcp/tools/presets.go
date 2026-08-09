package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/store"
)

type TracePresetsListTool struct{}

func (t *TracePresetsListTool) Name() string { return "trace_presets_list" }

func (t *TracePresetsListTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_presets_list",
		Description: "List available presets (from DB, fallback to YAML).",
		InputSchema: JSONSchema{
			Type:       "object",
			Properties: map[string]PropSpec{},
		},
	}
}

func (t *TracePresetsListTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	// Приоритет: БД > YAML
	dbPresets, err := st.GetPresets()
	if err != nil {
		return fmt.Sprintf("Failed to read presets: %v", err), nil
	}

	if len(dbPresets) > 0 {
		// Сортируем по имени
		sort.Slice(dbPresets, func(i, j int) bool { return dbPresets[i].Name < dbPresets[j].Name })

		result := fmt.Sprintf("Available presets: %d\n", len(dbPresets))
		for _, p := range dbPresets {
			result += fmt.Sprintf("\n  \U0001f527 %s (%d paths)", p.Name, len(p.Paths))
			if p.Description != "" && p.Description != p.Name {
				result += fmt.Sprintf("\n     %s", p.Description)
			}
			for _, path := range p.Paths {
				result += fmt.Sprintf("\n     %s", path)
			}
			result += "\n"
		}
		return result, nil
	}

	// Fallback на YAML (если БД пуста)
	if len(cfg.Presets) == 0 {
		return "No presets configured (neither in DB nor in revizor.yaml)", nil
	}

	names := make([]string, 0, len(cfg.Presets))
	for name := range cfg.Presets {
		names = append(names, name)
	}
	sort.Strings(names)

	result := fmt.Sprintf("Available presets: %d (from revizor.yaml)\n", len(cfg.Presets))
	for _, name := range names {
		preset := cfg.Presets[name]
		result += fmt.Sprintf("\n  \U0001f527 %s (%d paths)", name, len(preset.Paths))
		if preset.Description != "" {
			result += fmt.Sprintf("\n     %s", preset.Description)
		}
		for _, p := range preset.Paths {
			result += fmt.Sprintf("\n     %s", p)
		}
		result += "\n"
	}
	return result, nil
}
