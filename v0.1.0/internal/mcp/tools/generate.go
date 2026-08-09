package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/store"
)

type TraceGenerateTestTool struct{}

func (t *TraceGenerateTestTool) Name() string { return "trace_generate_test" }

func (t *TraceGenerateTestTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_generate_test",
		Description: "Generate a pytest test from the event chain of a single request_id.",
		InputSchema: JSONSchema{
			Type: "object",
			Properties: map[string]PropSpec{
				"request_id": stringProp("Request ID to reproduce"),
				"session_id": stringProp("Session ID (optional)"),
			},
			Required: []string{"request_id"},
		},
	}
}

func (t *TraceGenerateTestTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	var params struct {
		RequestID string `json:"request_id"`
		SessionID string `json:"session_id"`
	}
	if err := parseArgs(args, &params); err != nil {
		return fmt.Sprintf("Invalid argument: %v (check parameter types)", err), nil
	}
	if params.RequestID == "" {
		return "Specify the request_id", nil
	}

	var sidPtr *string
	if params.SessionID != "" {
		sidPtr = &params.SessionID
	}

	chain, err := st.GetRequestChain(params.RequestID, sidPtr)
	if err != nil {
		return fmt.Sprintf("Failed to get request chain: %v", err), nil
	}
	if len(chain) == 0 {
		return fmt.Sprintf("No events found for request_id=%s", params.RequestID), nil
	}

	ridShort := params.RequestID
	if len(ridShort) > 8 {
		ridShort = ridShort[:8]
	}

	result := fmt.Sprintf("\"\"\"\nAuto-generated test for request_id=%s\nEvents in chain: %d\n\"\"\"\n", params.RequestID, len(chain))
	result += "import pytest\n"
	result += "from unittest.mock import MagicMock, patch\n\n\n"
	result += fmt.Sprintf("class TestReproduce_%s:\n", ridShort)
	result += fmt.Sprintf("    \"\"\"Reproduce chain %s.\"\"\"\n\n", ridShort)
	result += fmt.Sprintf("    def test_reproduce_chain(self):\n")
	result += fmt.Sprintf("        \"\"\"Chain of %d events.\"\"\"\n", len(chain))

	for i, event := range chain {
		dataStr := ""
		if event.Data != nil {
			dataBytes, _ := json.Marshal(event.Data)
			dataStr = string(dataBytes)
			if len(dataStr) > 120 {
				dataStr = dataStr[:120]
			}
		}
		result += fmt.Sprintf("        # Event %d: %s\n", i+1, event.TracePath)
		result += fmt.Sprintf("        # data: %s\n", dataStr)
		result += "        # TODO: add manual mock refinement\n"
	}

	result += "\n        # TODO: Add assertions based on the data above\n"
	result += "        assert True  # stub\n"
	return result, nil
}
