package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/store"
)

type TraceListPointsTool struct{}

func (t *TraceListPointsTool) Name() string { return "trace_list_points" }

func (t *TraceListPointsTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_list_points",
		Description: "Show all known trace paths in the system, grouped by module.",
		InputSchema: JSONSchema{
			Type: "object",
			Properties: map[string]PropSpec{
				"module_prefix": stringProp("Optional module prefix filter (e.g., auth, elevation)"),
			},
		},
	}
}

func (t *TraceListPointsTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	var params struct {
		ModulePrefix string `json:"module_prefix"`
	}
	if err := parseArgs(args, &params); err != nil {
		return fmt.Sprintf("Invalid argument: %v (check parameter types)", err), nil
	}

	var mpPtr *string
	if params.ModulePrefix != "" {
		mpPtr = &params.ModulePrefix
	}

	paths, err := st.GetDistinctPaths(nil, mpPtr)
	if err != nil {
		return fmt.Sprintf("Failed to retrieve trace paths: %v", err), nil
	}
	if len(paths) == 0 {
		return "No registered trace points", nil
	}

	byModule := make(map[string][]string)
	for _, p := range paths {
		mod := "_root"
		if idx := strings.Index(p, "."); idx != -1 {
			mod = p[:idx]
		}
		byModule[mod] = append(byModule[mod], p)
	}

	// Sort module names
	modNames := make([]string, 0, len(byModule))
	for m := range byModule {
		modNames = append(modNames, m)
	}
	sort.Strings(modNames)

	result := fmt.Sprintf("Total unique paths: %d\n", len(paths))
	for _, mod := range modNames {
		pts := byModule[mod]
		sort.Strings(pts)
		result += fmt.Sprintf("\n[%s] (%d points):", mod, len(pts))
		for _, p := range pts {
			result += fmt.Sprintf("\n  * %s", p)
		}
		result += "\n"
	}
	return result, nil
}
