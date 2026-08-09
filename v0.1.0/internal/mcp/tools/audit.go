package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/store"
)

// TraceAuditTool — проверка Python и TypeScript файлов на соответствие RE033_NO_TRACE.
// Сканирует source_root и проверяет наличие импорта trace (настраивается через конфиг).
type TraceAuditTool struct{}

func (t *TraceAuditTool) Name() string { return "trace_audit" }

func (t *TraceAuditTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_audit",
		Description: "Audit Python and TypeScript modules for RE033_NO_TRACE compliance. Checks that every .py and .ts/.tsx file imports trace (Python: revizor_core_py_0_1_0, TypeScript: @ais-platform/revizor_core_ts_0_1_0).",
		InputSchema: JSONSchema{
			Type: "object",
			Properties: map[string]PropSpec{
				"module_path": stringProp("Specific module directory or name to audit. Default: all modules/"),
			},
		},
	}
}

func (t *TraceAuditTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	var params struct {
		ModulePath string `json:"module_path"`
	}
	if err := parseArgs(args, &params); err != nil {
		return fmt.Sprintf("Invalid argument: %v (check parameter types)", err), nil
	}

	root, err := os.Getwd()
	if err != nil {
		return fmt.Sprintf("Failed to get working directory: %v", err), nil
	}

	if params.ModulePath != "" {
		// Аудит конкретного модуля/директории
		modulePath := resolveModulePath(root, params.ModulePath, cfg)
		if modulePath == "" {
			return fmt.Sprintf("FAIL: path not found — %s", params.ModulePath), nil
		}
		result, err := auditModule(modulePath, cfg)
		if err != nil {
			return fmt.Sprintf("Failed to audit %s: %v", params.ModulePath, err), nil
		}
		return formatSingleAudit(params.ModulePath, result), nil
	}

	// Аудит всех модулей — используем source_root из конфига
	sourceRoot := cfg.Project.SourceRoot
	if sourceRoot == "" {
		sourceRoot = "modules" // обратная совместимость
	}
	scanRoot := filepath.Join(root, sourceRoot)
	if _, err := os.Stat(scanRoot); os.IsNotExist(err) {
		return fmt.Sprintf("FAIL: source_root directory '%s' not found in current working directory", sourceRoot), nil
	}

	results, err := auditAllModules(scanRoot, cfg)
	if err != nil {
		return fmt.Sprintf("Failed to audit modules: %v", err), nil
	}

	if len(results) == 0 {
		return "No files found to audit", nil
	}

	return formatAllAudits(results), nil
}

// --- Доменные типы ---

// AuditResult — результат проверки модуля на RE033_NO_TRACE.
type AuditResult struct {
	OK           bool
	FilesMissing []string
	TotalFiles   int
}

// ModuleAudit — результат аудита одного модуля.
type ModuleAudit struct {
	ModuleName string
	Result     AuditResult
}

// --- Основная логика аудита ---

// auditModule проверяет .py и .ts/.tsx файлы в директории на наличие trace-импорта.
func auditModule(modulePath string, cfg *config.Config) (AuditResult, error) {
	result := AuditResult{OK: true}

	err := filepath.WalkDir(modulePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			dirName := d.Name()
			if isAuditExcludedDir(dirName) {
				return filepath.SkipDir
			}
			return nil
		}

		name := d.Name()
		isPy := strings.HasSuffix(name, ".py")
		isTs := strings.HasSuffix(name, ".ts") || strings.HasSuffix(name, ".tsx")
		if !isPy && !isTs {
			return nil
		}
		if name == "__init__.py" || strings.HasSuffix(name, ".d.ts") {
			return nil
		}

		result.TotalFiles++
		if !hasTraceImport(path, cfg) {
			result.OK = false
			relPath, _ := filepath.Rel(modulePath, path)
			result.FilesMissing = append(result.FilesMissing, relPath)
		}
		return nil
	})

	return result, err
}

// auditAllModules обходит все директории и аудитирует каждый модуль.
func auditAllModules(scanRoot string, cfg *config.Config) ([]ModuleAudit, error) {
	entries, err := os.ReadDir(scanRoot)
	if err != nil {
		return nil, fmt.Errorf("reading directory: %w", err)
	}

	var results []ModuleAudit
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "__") {
			continue
		}
		if isExcludedModule(name, cfg) {
			continue
		}

		modulePath := filepath.Join(scanRoot, name)
		result, err := auditModule(modulePath, cfg)
		if err != nil {
			// Пропускаем модули с ошибками чтения
			continue
		}
		if result.TotalFiles == 0 {
			continue
		}

		results = append(results, ModuleAudit{
			ModuleName: name,
			Result:     result,
		})
	}

	return results, nil
}

// --- Проверка импорта ---

// hasTraceImport проверяет наличие импорта trace в файле (паттерны из конфига).
func hasTraceImport(filePath string, cfg *config.Config) bool {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return false
	}
	s := string(content)

	// Определяем какие паттерны использовать
	var pyPatterns, tsPatterns []string
	if cfg != nil {
		pyPatterns = cfg.Project.TraceImports.AuditPythonPatterns
		tsPatterns = cfg.Project.TraceImports.AuditTypeScriptPatterns
	}
	if len(pyPatterns) == 0 {
		pyPatterns = []string{
			"from revizor_sdk import",
			"from modules.revizor_core_py_0_1_0 import",
			"from revizor_core_py_0_1_0 import",
		}
	}
	if len(tsPatterns) == 0 {
		tsPatterns = []string{"@ais-platform/revizor-sdk", "@ais-platform/revizor_core_ts_0_1_0"}
	}

	for _, p := range pyPatterns {
		if strings.Contains(s, p) {
			return true
		}
	}
	for _, p := range tsPatterns {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

// --- Исключения ---

// isExcludedModule возвращает true если модуль исключён из аудита.
func isExcludedModule(name string, cfg *config.Config) bool {
	// AIS-специфичные исключения (обратная совместимость)
	if name == "revizor_core_py_0_1_0" ||
		name == "revizor_core_0_1_0" ||
		name == "revizor_middleware_0_1_0" ||
		name == "revizor_api_0_1_0" ||
		name == "revizor_mcp_0_1_0" ||
		name == "ais_module_template_0_1_0" ||
		name == "ais_prompter_api_0_1_0" ||
		name == "ais_prompter_api_0_2_0" {
		return true
	}
	// Исключения из конфига
	if cfg != nil {
		for _, exc := range cfg.Project.ExcludedDirs {
			if name == exc {
				return true
			}
		}
	}
	return false
}

// isAuditExcludedDir возвращает true для директорий, исключённых из аудита.
func isAuditExcludedDir(dirName string) bool {
	switch dirName {
	case "__pycache__", ".pytest_cache", "node_modules", ".next", "dist", "tests":
		return true
	}
	return false
}

// resolveModulePath преобразует путь в абсолютный путь к директории.
func resolveModulePath(root, moduleOrPath string, cfg *config.Config) string {
	// Если это абсолютный или относительный путь к существующей директории
	if filepath.IsAbs(moduleOrPath) {
		if info, err := os.Stat(moduleOrPath); err == nil && info.IsDir() {
			return moduleOrPath
		}
	}
	relativePath := filepath.Join(root, moduleOrPath)
	if info, err := os.Stat(relativePath); err == nil && info.IsDir() {
		return relativePath
	}
	// Пробуем как имя модуля в source_root
	sourceRoot := cfg.Project.SourceRoot
	if sourceRoot == "" {
		sourceRoot = "modules"
	}
	modulePath := filepath.Join(root, sourceRoot, moduleOrPath)
	if info, err := os.Stat(modulePath); err == nil && info.IsDir() {
		return modulePath
	}
	return ""
}

// --- Форматирование вывода ---

func formatSingleAudit(moduleName string, result AuditResult) string {
	if result.OK {
		return fmt.Sprintf("OK: %s — all %d files have trace import", moduleName, result.TotalFiles)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("RE033_NO_TRACE in %s:\n", moduleName))
	for _, f := range result.FilesMissing {
		sb.WriteString(fmt.Sprintf("  - %s\n", f))
	}
	sb.WriteString(fmt.Sprintf("\n%d of %d files missing trace import", len(result.FilesMissing), result.TotalFiles))
	return sb.String()
}

func formatAllAudits(results []ModuleAudit) string {
	var okCount, failCount int
	var sb strings.Builder

	for _, ma := range results {
		if ma.Result.OK {
			okCount++
		} else {
			failCount++
			sb.WriteString(formatSingleAudit(ma.ModuleName, ma.Result))
			sb.WriteString("\n\n")
		}
	}

	summary := fmt.Sprintf("Audit complete: %d modules OK, %d with RE033_NO_TRACE (total %d)",
		okCount, failCount, len(results))
	return summary + "\n\n" + sb.String()
}
