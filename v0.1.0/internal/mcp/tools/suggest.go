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

type TraceSuggestCoverageTool struct{}

func (t *TraceSuggestCoverageTool) Name() string { return "trace_suggest_coverage" }

func (t *TraceSuggestCoverageTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_suggest_coverage",
		Description: "Analyze registered trace paths for missing .success/.failed events AND find traffic from the full event database that has no registered trace paths.",
		InputSchema: JSONSchema{
			Type: "object",
			Properties: map[string]PropSpec{
				"module_path": stringProp("Module prefix to analyze (e.g., auth, elevation)"),
			},
			Required: []string{"module_path"},
		},
	}
}

func (t *TraceSuggestCoverageTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	var params struct {
		ModulePath string `json:"module_path"`
	}
	if err := parseArgs(args, &params); err != nil {
		return fmt.Sprintf("Invalid argument: %v (check parameter types)", err), nil
	}
	if params.ModulePath == "" {
		return "Specify a module prefix (e.g., auth, elevation)", nil
	}

	mp := params.ModulePath
	paths, err := st.GetDistinctPaths(nil, &mp)
	if err != nil {
		return fmt.Sprintf("Failed to retrieve trace paths: %v", err), nil
	}

	var result string

	// Phase 1: Check enter/success/failed completeness from registered paths
	if len(paths) > 0 {
		enters := make(map[string]bool)
		successes := make(map[string]bool)
		failures := make(map[string]bool)

		for _, p := range paths {
			if strings.HasSuffix(p, ".enter") {
				base := p[:len(p)-len(".enter")]
				enters[base] = true
			}
			if strings.HasSuffix(p, ".success") {
				base := p[:len(p)-len(".success")]
				successes[base] = true
			}
			if strings.HasSuffix(p, ".failed") {
				base := p[:len(p)-len(".failed")]
				failures[base] = true
			}
		}

		var missing []string
		for base := range enters {
			if !successes[base] && !failures[base] {
				missing = append(missing, base+".enter → need .success or .failed")
			}
		}
		sort.Strings(missing)

		if len(missing) == 0 {
			result = "All registered functions have complete trace chains (enter → success/failed)."
		} else {
			result = fmt.Sprintf("Incomplete trace chains: %d\n", len(missing))
			for _, m := range missing[:min(len(missing), 100)] {
				result += "\n  " + m
			}
			if len(missing) > 100 {
				result += fmt.Sprintf("\n  ... and %d more", len(missing)-100)
			}
		}
	} else {
		result = fmt.Sprintf("No registered trace points for prefix: %s", mp)
	}

	// Phase 2: Analyze actual traffic from the full event database
	modulePattern := mp + ".%"
	recentEvents, evtErr := st.ReadTraceLog(nil, 0, 5000, &modulePattern, nil, nil)
	if evtErr == nil && len(recentEvents) > 0 {
		registeredSet := make(map[string]bool)
		for _, p := range paths {
			registeredSet[p] = true
		}

		eventPathSet := make(map[string]bool)
		for _, e := range recentEvents {
			if strings.HasPrefix(e.TracePath, mp+".") {
				eventPathSet[e.TracePath] = true
			}
		}

		var uncoveredPaths []string
		for ep := range eventPathSet {
			if !registeredSet[ep] {
				uncoveredPaths = append(uncoveredPaths, ep)
			}
		}
		sort.Strings(uncoveredPaths)

		if len(uncoveredPaths) > 0 {
			result += fmt.Sprintf("\n\nUncovered traffic (%d paths in event log but not registered):\n", len(uncoveredPaths))
			for i, p := range uncoveredPaths {
				if i >= 50 {
					result += fmt.Sprintf("  ... and %d more\n", len(uncoveredPaths)-50)
					break
				}
				result += "  " + p + "\n"
			}
		}
	}

	return result, nil
}
