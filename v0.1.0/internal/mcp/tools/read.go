package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/store"
)

type TraceReadTool struct{}

func (t *TraceReadTool) Name() string { return "trace_read" }

func (t *TraceReadTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_read",
		Description: "Read the last N lines of a session log. Supports pagination (offset/limit), format modes (json, compact, table), and max_response_chars truncation.",
		InputSchema: JSONSchema{
			Type: "object",
			Properties: map[string]PropSpec{
				"session_id":         stringProp("Session ID (optional)"),
				"lines":              intProp("Number of lines to read (default: 50, deprecated: use limit)"),
				"limit":              intProp("Max events to return (default: 50)"),
				"offset":             intProp("Skip first N events (default: 0)"),
				"max_response_chars": intProp("Truncate output to this many characters (default: 8000)"),
				"format":             stringProp("Output format: 'json' (default), 'compact' (tab-separated one-liner), 'table' (column-aligned)"),
			},
		},
	}
}

func (t *TraceReadTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	var params struct {
		SessionID        string `json:"session_id"`
		Lines            int    `json:"lines"`
		Limit            int    `json:"limit"`
		Offset           int    `json:"offset"`
		MaxResponseChars int    `json:"max_response_chars"`
		Format           string `json:"format"`
	}
	if err := parseArgs(args, &params); err != nil {
		return fmt.Sprintf("Invalid argument: %v (check parameter types)", err), nil
	}

	// Backwards compat: lines -> limit
	if params.Limit <= 0 {
		params.Limit = params.Lines
	}
	if params.Limit <= 0 {
		params.Limit = 50
	}
	if params.MaxResponseChars <= 0 {
		params.MaxResponseChars = 8000
	}
	switch params.Format {
	case "", "json", "compact", "table":
		// valid
	default:
		return fmt.Sprintf("Invalid format: '%s'. Valid formats: json, compact, table", params.Format), nil
	}

	var sidPtr *string
	if params.SessionID != "" {
		sidPtr = &params.SessionID
	}

	logs, err := st.ReadTraceLog(sidPtr, params.Offset, params.Limit, nil, nil, nil)
	if err != nil {
		return fmt.Sprintf("Failed to read trace log: %v", err), nil
	}
	if len(logs) == 0 {
		return "Log is empty", nil
	}

	return formatAndTruncate(logs, params.Format, params.MaxResponseChars), nil
}

// --- Общие хелперы форматирования (используются также search.go) ---

func formatAndTruncate(entries []store.TraceEntry, format string, maxChars int) string {
	var result string
	switch format {
	case "compact":
		result = formatCompact(entries)
	case "table":
		result = formatTraceTable(entries)
	default:
		data, err := json.MarshalIndent(entries, "", "  ")
		if err != nil {
			return fmt.Sprintf("Failed to format results: %v", err)
		}
		result = string(data)
	}

	if maxChars > 0 && len(result) > maxChars {
		truncated := result[:maxChars]
		lines := strings.Count(truncated, "\n")
		if format == "json" {
			lines = strings.Count(truncated, `"id":`)
		}
		return truncated + fmt.Sprintf("\n... (truncated at %d chars, ~%d of %d events shown)", maxChars, lines, len(entries))
	}

	return result
}

func formatCompact(entries []store.TraceEntry) string {
	var sb strings.Builder
	for _, e := range entries {
		sb.WriteString(e.CreatedAt)
		sb.WriteByte('\t')
		sb.WriteString(e.TracePath)
		sb.WriteByte('\t')
		sb.WriteString(flattenData(e.Data))
		sb.WriteByte('\n')
	}
	return sb.String()
}

func formatTraceTable(entries []store.TraceEntry) string {
	var sb strings.Builder
	const timeW = 26
	const pathW = 50

	sb.WriteString(fmt.Sprintf("%-*s | %-*s | DATA\n", timeW, "TIME", pathW, "TRACE_PATH"))
	sb.WriteString(fmt.Sprintf("%s|%s|------------------------------\n",
		strings.Repeat("-", timeW+1), strings.Repeat("-", pathW+1)))

	for _, e := range entries {
		timeStr := padOrTrunc(e.CreatedAt, timeW)
		pathStr := padOrTrunc(e.TracePath, pathW)
		dataStr := flattenData(e.Data)
		sb.WriteString(fmt.Sprintf("%s | %s | %s\n", timeStr, pathStr, dataStr))
	}
	return sb.String()
}

func padOrTrunc(s string, width int) string {
	if len(s) > width {
		return s[:width-3] + "..."
	}
	return s + strings.Repeat(" ", width-len(s))
}

func flattenData(data any) string {
	if data == nil {
		return "-"
	}
	switch m := data.(type) {
	case map[string]any:
		var pairs []string
		for k, v := range m {
			valStr := fmt.Sprintf("%v", v)
			if len(valStr) > 80 {
				valStr = valStr[:80] + "..."
			}
			pairs = append(pairs, k+"="+valStr)
		}
		return strings.Join(pairs, " ")
	case string:
		return m
	default:
		return fmt.Sprintf("%v", data)
	}
}
