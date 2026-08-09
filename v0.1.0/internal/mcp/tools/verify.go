package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/store"
)

type TraceVerifyCoverageTool struct{}

func (t *TraceVerifyCoverageTool) Name() string { return "trace_verify_coverage" }

func (t *TraceVerifyCoverageTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_verify_coverage",
		Description: "Verify chain integrity: each .enter event should have a corresponding .success or .failed.",
		InputSchema: JSONSchema{
			Type: "object",
			Properties: map[string]PropSpec{
				"session_id":  stringProp("Session ID (optional)"),
				"module_name": stringProp("Module name filter (e.g., auth)"),
			},
		},
	}
}

func (t *TraceVerifyCoverageTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	var params struct {
		SessionID  string `json:"session_id"`
		ModuleName string `json:"module_name"`
	}
	if err := parseArgs(args, &params); err != nil {
		return fmt.Sprintf("Invalid argument: %v (check parameter types)", err), nil
	}

	if params.SessionID == "" && params.ModuleName == "" {
		return "Please specify session_id or module_name to check. Without filters, the check is too broad.", nil
	}

	var sidPtr, modPtr *string
	if params.SessionID != "" {
		sidPtr = &params.SessionID
	}
	if params.ModuleName != "" {
		modPtr = &params.ModuleName
	}

	incomplete, err := st.GetEnterChains(sidPtr, modPtr)
	if err != nil {
		return fmt.Sprintf("Failed to verify trace chains: %v", err), nil
	}
	if len(incomplete) == 0 {
		return "All chains complete", nil
	}

	result := fmt.Sprintf("Incomplete chains: %d\n", len(incomplete))
	maxShow := 100
	for i, c := range incomplete {
		if i >= maxShow {
			result += fmt.Sprintf("\n  ... and %d more", len(incomplete)-maxShow)
			break
		}
		result += fmt.Sprintf("\n  %s", c.EnterPath)
		if c.BasePath != "" {
			result += fmt.Sprintf(" (base: %s)", c.BasePath)
		}
	}
	return result, nil
}
