package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/store"
)

//go:embed agent_instructions.sil
var agentInstructions string

// TraceGetInstructionsTool — возвращает встроенную инструкцию для AI-агента.
type TraceGetInstructionsTool struct{}

func (t *TraceGetInstructionsTool) Name() string { return "trace_get_instructions" }

func (t *TraceGetInstructionsTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_get_instructions",
		Description: "Return the AI agent instructions embedded in the binary. Use this to get the full debugging guide when you don't have access to the README file on disk. No file access required.",
		InputSchema: JSONSchema{
			Type:       "object",
			Properties: map[string]PropSpec{},
		},
	}
}

func (t *TraceGetInstructionsTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	if agentInstructions == "" {
		return fmt.Sprintf("Instructions not embedded in this build. See DEV_README.md for the full list of %d tools.", len(All())), nil
	}
	return agentInstructions, nil
}
