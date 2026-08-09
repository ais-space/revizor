package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/store"
)

// TraceRemoveTool — удаление trace()-вызовов из Python и TypeScript файлов.
// Реверс trace_inject: находит trace(...) вызовы и удаляет их, включая импорт если trace больше не используется.
type TraceRemoveTool struct{}

func (t *TraceRemoveTool) Name() string { return "trace_remove" }

func (t *TraceRemoveTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_remove",
		Description: "Remove trace() calls from a Python or TypeScript file. Also removes the trace import if no trace calls remain. Uses dry_run by default (shows diff).",
		InputSchema: JSONSchema{
			Type: "object",
			Properties: map[string]PropSpec{
				"file_path": stringProp("Path to source file (relative to current working directory)"),
				"language":  stringProp("Language: python or typescript (auto-detected from extension)"),
				"dry_run":   stringProp("If 'true', show diff without writing (default: true)"),
			},
			Required: []string{"file_path"},
		},
	}
}

func (t *TraceRemoveTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	var params struct {
		FilePath string `json:"file_path"`
		Language string `json:"language"`
		DryRun   string `json:"dry_run"`
	}
	if err := parseArgs(args, &params); err != nil {
		return fmt.Sprintf("Invalid argument: %v (check parameter types)", err), nil
	}
	if params.FilePath == "" {
		return "File path is required", nil
	}
	if params.DryRun == "" {
		params.DryRun = "true"
	}

	// Resolve path
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Sprintf("Failed to get working directory: %v", err), nil
	}
	absPath := params.FilePath
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(cwd, absPath)
	}

	lang := detectLanguage(absPath)
	if lang == "" {
		return fmt.Sprintf("Cannot detect language for file: %s. Only .py, .ts, and .tsx files are supported.", params.FilePath), nil
	}

	// Read file
	content, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Sprintf("Failed to read file: %v", err), nil
	}
	original := string(content)

	// Remove trace calls
	var newContent string
	var importRemoved bool
	switch lang {
	case "python":
		newContent, importRemoved = removePythonTraces(original)
	case "typescript":
		newContent, importRemoved = removeTypeScriptTraces(original)
	}

	if newContent == original {
		return "No trace() calls found to remove.", nil
	}

	if params.DryRun == "true" {
		diff := unifiedDiff(original, newContent, absPath)
		summary := fmt.Sprintf("Dry run: %d trace call(s) would be removed.", strings.Count(original, "trace(")-strings.Count(newContent, "trace("))
		if importRemoved {
			summary += " Import would be removed."
		}
		return summary + "\n\n" + diff, nil
	}

	// Write
	if err := atomicWriteWithBackup(absPath, []byte(original), newContent); err != nil {
		return fmt.Sprintf("Failed to write file: %v", err), nil
	}

	summary := fmt.Sprintf("Removed trace calls from %s.", params.FilePath)
	if importRemoved {
		summary += " Import statement removed."
	}
	return summary, nil
}

var pyTraceCallRe = regexp.MustCompile(`(?m)^(\s*)trace\([^)]*\)\s*$`)
var tsTraceCallRe = regexp.MustCompile(`(?m)^(\s*)trace\([^)]*\);\s*$`)
var pyEndCallRe = regexp.MustCompile(`(?m)^(\s*end\([^)]*\)\s*$|^\s*end = trace_start\(.*\)\s*$)`)
var pyImportRe = regexp.MustCompile(`(?m)^(\s*)(from \S+ import .*trace.*|import .*trace.*)\s*$`)
var tsImportRe = regexp.MustCompile(`(?m)^(\s*)(import \{ .*trace.*\} from .*|import .*trace.* from .*)\s*$`)
var tsDeclareRe = regexp.MustCompile(`(?m)^(\s*)declare module\s+"@ais-platform/revizor-sdk"\s*\{[^}]*\}\s*$`)

func removePythonTraces(content string) (string, bool) {
	result := pyTraceCallRe.ReplaceAllString(content, "")
	result = pyEndCallRe.ReplaceAllString(result, "")
	result = pyEndCallRe.ReplaceAllString(result, "")

	// Remove empty lines (collapsed double-newlines)
	for strings.Contains(result, "\n\n\n") {
		result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
	}

	// Check if any trace calls remain
	importRemoved := false
	if !strings.Contains(result, "trace(") && !strings.Contains(result, "trace_start(") {
		result = pyImportRe.ReplaceAllString(result, "")
		result = strings.TrimSpace(result) + "\n"
		importRemoved = true
	}

	return result, importRemoved
}

func removeTypeScriptTraces(content string) (string, bool) {
	result := tsTraceCallRe.ReplaceAllString(content, "")
	result = tsTraceCallRe.ReplaceAllString(result, "")

	for strings.Contains(result, "\n\n\n") {
		result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
	}

	importRemoved := false
	if !strings.Contains(result, "trace(") {
		result = tsImportRe.ReplaceAllString(result, "")
		result = tsDeclareRe.ReplaceAllString(result, "")
		result = strings.TrimSpace(result) + "\n"
		importRemoved = true
	}

	return result, importRemoved
}
