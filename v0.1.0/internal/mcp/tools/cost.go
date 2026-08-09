package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/store"
)

type TraceCostTool struct{}

func (t *TraceCostTool) Name() string { return "trace_cost" }

func (t *TraceCostTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_cost",
		Description: "Show historical frequency of a trace point (events per minute over a time period).",
		InputSchema: JSONSchema{
			Type: "object",
			Properties: map[string]PropSpec{
				"path":  stringProp("Trace path (e.g., auth.callback.enter)"),
				"hours": intProp("Analysis period in hours (default: 24)"),
			},
			Required: []string{"path"},
		},
	}
}

func (t *TraceCostTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	var params struct {
		Path  string `json:"path"`
		Hours int    `json:"hours"`
	}
	if err := parseArgs(args, &params); err != nil {
		return fmt.Sprintf("Invalid argument: %v (check parameter types)", err), nil
	}
	if params.Path == "" {
		return "Specify the trace path", nil
	}
	if params.Hours <= 0 {
		params.Hours = 24
	}

	freq, err := st.GetPathFrequency(nil, params.Path, params.Hours)
	if err != nil {
		return fmt.Sprintf("Failed to get path frequency: %v", err), nil
	}
	if len(freq) == 0 {
		return fmt.Sprintf("No data for %s in the last %d hours", params.Path, params.Hours), nil
	}

	total := 0
	peak := 0
	for _, b := range freq {
		total += b.Count
		if b.Count > peak {
			peak = b.Count
		}
	}
	avg := float64(total) / float64(len(freq))

	result := fmt.Sprintf("Point: %s\n", params.Path)
	result += fmt.Sprintf("Period: %d hours\n", params.Hours)
	result += fmt.Sprintf("Total events: %d\n", total)
	result += fmt.Sprintf("Avg per minute: %.1f\n", avg)
	result += fmt.Sprintf("Peak per minute: %d\n", peak)
	result += "\nBy minute (first 20):"

	show := freq
	if len(show) > 20 {
		show = show[:20]
	}
	for _, b := range show {
		bar := strings.Repeat("#", min(b.Count, 50))
		result += fmt.Sprintf("\n  %s  %5d %s", b.Bucket, b.Count, bar)
	}
	if len(freq) > 20 {
		result += fmt.Sprintf("\n  ... and %d more intervals", len(freq)-20)
	}
	return result, nil
}
