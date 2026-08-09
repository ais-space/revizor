package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/store"
	"github.com/ais-platform/ais_products/revizor/internal/trace"
)

type TraceStartTool struct{}

func (t *TraceStartTool) Name() string { return "trace_start" }

func (t *TraceStartTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_start",
		Description: "Create a debug session and enable trace points. Pass a preset name or a comma-separated list of trace paths.",
		InputSchema: JSONSchema{
			Type: "object",
			Properties: map[string]PropSpec{
				"paths":       stringProp("Comma-separated trace paths or preset name (from revizor.yaml)"),
				"description": stringProp("Session description (optional)"),
			},
			Required: []string{"paths"},
		},
	}
}

func (t *TraceStartTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	var params struct {
		Paths       string `json:"paths"`
		Description string `json:"description"`
	}
	if err := parseArgs(args, &params); err != nil {
		return fmt.Sprintf("Invalid argument: %v (check parameter types)", err), nil
	}
	if params.Paths == "" {
		return "No paths specified — provide comma-separated trace paths or a preset name.", nil
	}

	// Разрешение пресета: приоритет БД > YAML
	presetName := ""
	var pathList []string

	dbPresets, _ := st.GetPresets()
	foundInDB := false
	for _, p := range dbPresets {
		if p.Name == params.Paths {
			pathList = p.Paths
			presetName = p.Name
			foundInDB = true
			break
		}
	}

	if !foundInDB {
		if preset, ok := cfg.Presets[params.Paths]; ok {
			pathList = preset.Paths
			presetName = params.Paths
		}
	}

	// Если не пресет — парсим как список путей через запятую
	if len(pathList) == 0 {
		for _, p := range strings.Split(params.Paths, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				pathList = append(pathList, p)
			}
		}
	}

	if len(pathList) == 0 {
		return "No valid paths to enable — no paths matched the preset or pattern.", nil
	}

	var invalidPaths []string
	for _, p := range pathList {
		actual := p
		if strings.HasPrefix(p, "!") {
			actual = p[1:]
		}
		if !trace.ValidatePath(actual) {
			invalidPaths = append(invalidPaths, p)
		}
	}
	if len(invalidPaths) > 0 {
		lines := []string{"\u26a0\ufe0f Invalid paths found:", ""}
		for _, p := range invalidPaths {
			lines = append(lines, fmt.Sprintf("  \u274c %s", p))
		}
		lines = append(lines, "", "Format: lowercase [a-z0-9_.], max 120 characters.", "Fix the paths and retry.")
		return strings.Join(lines, "\n"), nil
	}

	sess, err := st.CreateSession("mcp-agent", params.Description)
	if err != nil || sess == nil {
		errMsg := "Failed to create session — the database may be locked or full."
		if err != nil {
			errMsg = fmt.Sprintf("Error: %v", err)
		}
		return errMsg, nil
	}

	sessionID := sess.SessionID
	sidPtr := &sessionID

	enabled := []string{}
	failed := []string{}
	for _, p := range pathList {
		if err := st.EnableTrace(p, sidPtr, store.EnableOpts{}); err != nil {
			failed = append(failed, p)
		} else {
			enabled = append(enabled, p)
		}
	}

	trace.InvalidateCache(sessionID)
	storeRows, _ := st.GetConfig(sidPtr)
	traceRows := make([]trace.ConfigRow, len(storeRows))
	for i, r := range storeRows {
		traceRows[i] = trace.ConfigRow{TracePath: r.TracePath, Enabled: r.Enabled, Owner: r.Owner}
	}
	trace.CompileConfig(traceRows, sidPtr)

	lines := []string{
		fmt.Sprintf("Session created: %s", sessionID),
		"Owner: mcp-agent",
	}
	if presetName != "" {
		lines = append(lines, fmt.Sprintf("Preset: %s", presetName))
	} else {
		lines = append(lines, fmt.Sprintf("Paths: %d", len(pathList)))
	}
	lines = append(lines, fmt.Sprintf("Enabled: %d/%d", len(enabled), len(pathList)))
	if len(enabled) > 0 {
		lines = append(lines, "Points:")
		for _, p := range enabled {
			lines = append(lines, fmt.Sprintf("  \u2705 %s", p))
		}
	}
	if len(failed) > 0 {
		lines = append(lines, "Errors:")
		for _, p := range failed {
			lines = append(lines, fmt.Sprintf("  \u274c %s", p))
		}
	}

	return strings.Join(lines, "\n"), nil
}
