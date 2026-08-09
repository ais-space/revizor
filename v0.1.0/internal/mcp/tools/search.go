package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/store"
)

type TraceSearchTool struct{}

func (t *TraceSearchTool) Name() string { return "trace_search" }

func (t *TraceSearchTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_search",
		Description: "Search logs: substring in data + optional path filter + data filter (REV-008) + count-by aggregation (REV-009) + context lines (REV-010). Supports pagination (offset/limit), format modes (json, compact, table), time filters (since/until ISO 8601), and max_response_chars truncation.",
		InputSchema: JSONSchema{
			Type: "object",
			Properties: map[string]PropSpec{
				"search":             stringProp("Substring to search for in trace data"),
				"session_id":         stringProp("Session ID (optional)"),
				"path_filter":        stringProp("Filter by trace path (optional, * = any segment)"),
				"data_filter":        stringProp("Filter by data field: 'key' (exists) or 'key=value' (exact match) (optional, REV-008)"),
				"count_by":           stringProp("Aggregation mode: 'path' to group counts by trace_path (optional, REV-009)"),
				"context_lines":      intProp("Number of context lines around each match, grep -C style (optional, REV-010)"),
				"lines":              intProp("Max results (default: 50, deprecated: use limit)"),
				"limit":              intProp("Max events to return (default: 50)"),
				"offset":             intProp("Skip first N events (default: 0)"),
				"max_response_chars": intProp("Truncate output to this many characters (default: 8000)"),
				"format":             stringProp("Output format: 'json' (default), 'compact' (tab-separated one-liner), 'table' (column-aligned)"),
				"since":              stringProp("Start time in ISO 8601 (e.g., 2026-06-10T00:00:00Z)"),
				"until":              stringProp("End time in ISO 8601 (e.g., 2026-06-10T23:59:59Z)"),
			},
			Required: []string{"search"},
		},
	}
}

func (t *TraceSearchTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	var params struct {
		Search           string `json:"search"`
		SessionID        string `json:"session_id"`
		PathFilter       string `json:"path_filter"`
		DataFilter       string `json:"data_filter"`
		CountBy          string `json:"count_by"`
		ContextLines     int    `json:"context_lines"`
		Lines            int    `json:"lines"`
		Limit            int    `json:"limit"`
		Offset           int    `json:"offset"`
		MaxResponseChars int    `json:"max_response_chars"`
		Format           string `json:"format"`
		Since            string `json:"since"`
		Until            string `json:"until"`
	}
	if err := parseArgs(args, &params); err != nil {
		return fmt.Sprintf("Invalid argument: %v (check parameter types)", err), nil
	}
	if params.Search == "" {
		return "Search string is required — provide a substring to search for.", nil
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
	if params.CountBy != "" && params.CountBy != "path" {
		return fmt.Sprintf("Invalid count_by: '%s'. Valid value: 'path'", params.CountBy), nil
	}

	var sidPtr, pfPtr, dfPtr *string
	if params.SessionID != "" {
		sidPtr = &params.SessionID
	}
	if params.PathFilter != "" {
		pfPtr = &params.PathFilter
	}
	if params.DataFilter != "" {
		dfPtr = &params.DataFilter
	}

	// Parse since/until
	var sinceTime, untilTime *time.Time
	if params.Since != "" {
		t, err := time.Parse(time.RFC3339, params.Since)
		if err != nil {
			return fmt.Sprintf("Invalid argument: 'since' must be ISO 8601 format (e.g., 2026-06-10T00:00:00Z)"), nil
		}
		sinceTime = &t
	}
	if params.Until != "" {
		t, err := time.Parse(time.RFC3339, params.Until)
		if err != nil {
			return fmt.Sprintf("Invalid argument: 'until' must be ISO 8601 format (e.g., 2026-06-10T23:59:59Z)"), nil
		}
		untilTime = &t
	}

	// REV-009: режим агрегации
	if params.CountBy == "path" {
		counts, err := st.CountByPath(params.Search, dfPtr, pfPtr, sidPtr, sinceTime, untilTime)
		if err != nil {
			return fmt.Sprintf("Failed to count by path: %v", err), nil
		}
		if len(counts) == 0 {
			return fmt.Sprintf("Nothing found for: %s (count_by: path)", params.Search), nil
		}
		out, err := json.Marshal(counts)
		if err != nil {
			return fmt.Sprintf("Failed to marshal result: %v", err), nil
		}
		result := string(out)
		if len(result) > params.MaxResponseChars {
			result = result[:params.MaxResponseChars] + "... [truncated]"
		}
		return result, nil
	}

	// REV-010: контекстные строки
	if params.ContextLines > 0 {
		logs, err := st.SearchTraceLogWithContext(params.Search, sidPtr, pfPtr, dfPtr, params.Offset, params.Limit, sinceTime, untilTime, params.ContextLines)
		if err != nil {
			return fmt.Sprintf("Failed to search trace log with context: %v", err), nil
		}
		if len(logs) == 0 {
			msg := fmt.Sprintf("Nothing found for: %s", params.Search)
			if params.DataFilter != "" {
				msg += fmt.Sprintf(" (data_filter: %s)", params.DataFilter)
			}
			if params.ContextLines > 0 {
				msg += fmt.Sprintf(" (context_lines: %d)", params.ContextLines)
			}
			return msg, nil
		}
		return formatAndTruncate(logs, params.Format, params.MaxResponseChars), nil
	}

	// Обычный поиск (с data_filter если указан)
	logs, err := st.SearchTraceLogWithContext(params.Search, sidPtr, pfPtr, dfPtr, params.Offset, params.Limit, sinceTime, untilTime, 0)
	if err != nil {
		// Fallback для обратной совместимости: если реализация не поддерживает новый метод
		if strings.Contains(err.Error(), "not implemented") {
			logs, err = st.SearchTraceLog(params.Search, sidPtr, pfPtr, params.Offset, params.Limit, sinceTime, untilTime)
		}
		if err != nil {
			return fmt.Sprintf("Failed to search trace log: %v", err), nil
		}
	}
	if len(logs) == 0 {
		msg := fmt.Sprintf("Nothing found for: %s", params.Search)
		if params.DataFilter != "" {
			msg += fmt.Sprintf(" (data_filter: %s)", params.DataFilter)
		}
		return msg, nil
	}

	return formatAndTruncate(logs, params.Format, params.MaxResponseChars), nil
}
