package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/store"
)

// TraceTargetsTool — сканер и приоритизатор файлов для ревизоризации.
// Обходит source_root (по умолчанию modules/) и группирует .py/.ts/.tsx файлы по приоритетам P1-P5.
type TraceTargetsTool struct{}

func (t *TraceTargetsTool) Name() string { return "trace_targets" }

func (t *TraceTargetsTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_targets",
		Description: "Scan project filesystem to prioritize files for trace injection. Groups .py/.ts/.tsx files into P1-P5 priority levels and estimates function/trace counts.",
		InputSchema: JSONSchema{
			Type: "object",
			Properties: map[string]PropSpec{
				"module_path": stringProp("Specific module directory or name to scan. Default: all modules/"),
				"priority":    stringProp("Filter to one priority group: P1, P2, P3, P4, P5. Default: all"),
			},
		},
	}
}

func (t *TraceTargetsTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	var params struct {
		ModulePath string `json:"module_path"`
		Priority   string `json:"priority"`
	}
	if err := parseArgs(args, &params); err != nil {
		return fmt.Sprintf("Invalid argument: %v (check parameter types)", err), nil
	}

	root, err := os.Getwd()
	if err != nil {
		return fmt.Sprintf("Failed to get working directory: %v", err), nil
	}

	// Определяем корень сканирования из конфига
	sourceRoot := cfg.Project.SourceRoot
	if sourceRoot == "" {
		sourceRoot = "modules" // обратная совместимость
	}
	scanRoot := filepath.Join(root, sourceRoot)
	if _, err := os.Stat(scanRoot); os.IsNotExist(err) {
		return fmt.Sprintf("FAIL: source_root directory '%s' not found in current working directory", sourceRoot), nil
	}

	targets, err := scanTargets(scanRoot, params.ModulePath, cfg)
	if err != nil {
		return fmt.Sprintf("Failed to scan targets: %v", err), nil
	}

	if len(targets) == 0 {
		return "No files found to scan", nil
	}

	var result strings.Builder
	result.WriteString(formatStats(targets))

	if params.Priority != "" {
		// Фильтр по одной группе
		result.WriteString(formatTable(targets, params.Priority))
	} else {
		// Все группы
		for _, g := range []string{"P1", "P2", "P3", "P4", "P5"} {
			result.WriteString(formatTable(targets, g))
		}
	}

	return result.String(), nil
}

// --- Доменные типы ---

// TargetInfo — информация о файле-цели ревизоризации.
type TargetInfo struct {
	Path            string // относительный путь
	Module          string // имя модуля
	Functions       int    // количество функций (0 для TS)
	EstimatedPoints int    // оценка trace-точек
	Type            string // "product" или "test"
	Language        string // "python" или "typescript"
}

// --- Сканирование ---

// scanTargets обходит директорию и собирает цели.
func scanTargets(scanRoot, modulePath string, cfg *config.Config) (map[string][]TargetInfo, error) {
	targets := map[string][]TargetInfo{
		"P1": {}, "P2": {}, "P3": {}, "P4": {}, "P5": {},
	}

	walkRoot := scanRoot
	if modulePath != "" {
		// Сканируем конкретную поддиректорию
		candidate := filepath.Join(scanRoot, modulePath)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			walkRoot = candidate
		} else if info, err := os.Stat(modulePath); err == nil && info.IsDir() {
			walkRoot = modulePath
		} else {
			return nil, fmt.Errorf("path not found: %s", modulePath)
		}
	}

	err := filepath.WalkDir(walkRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // пропускаем недоступные файлы
		}

		// Пропуск исключённых директорий
		if d.IsDir() {
			if isTargetExcludedDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}

		// Фильтр по расширению
		ext := filepath.Ext(path)
		if ext != ".py" && ext != ".ts" && ext != ".tsx" {
			return nil
		}

		// Проверка исключений
		if shouldExcludeTarget(path, d, cfg) {
			return nil
		}

		priority := classifyTarget(path)
		funcCount := 0
		if ext == ".py" {
			funcCount = countPythonFunctions(path)
		}

		isTest := isTestFile(path)

		lang := "python"
		if ext == ".ts" || ext == ".tsx" {
			lang = "typescript"
		}

		estPoints := estimateTracePoints(funcCount, isTest || lang == "typescript")

		relPath, _ := filepath.Rel(filepath.Dir(scanRoot), path)
		var srcRoot string
		if cfg != nil {
			srcRoot = cfg.Project.SourceRoot
		}
		module := getModuleNameFromTarget(path, srcRoot)

		targets[priority] = append(targets[priority], TargetInfo{
			Path:            relPath,
			Module:          module,
			Functions:       funcCount,
			EstimatedPoints: estPoints,
			Type:            map[bool]string{true: "test", false: "product"}[isTest],
			Language:        lang,
		})
		return nil
	})

	if err != nil {
		return nil, err
	}

	// Сортировка внутри групп по убыванию оценочных точек
	for _, p := range []string{"P1", "P2", "P3", "P4", "P5"} {
		sort.Slice(targets[p], func(i, j int) bool {
			return targets[p][i].EstimatedPoints > targets[p][j].EstimatedPoints
		})
	}

	return targets, nil
}

// --- Классификация (эвристическая) ---

// p1Keywords — ключевые слова, характерные для критического кода.
// Проверяются как целые слова (через разделители), чтобы "oauth" не матчился на "auth".
var p1Keywords = []string{"auth", "session", "elevation", "permission", "credential", "admin"}

// p2Keywords — ключевые слова, характерные для важного кода.
var p2Keywords = []string{"api", "identity", "link", "linker", "oauth", "callback", "merge"}

func classifyTarget(path string) string {
	ext := filepath.Ext(path)
	lower := strings.ToLower(path)
	tf := isTestFile(path)

	// TypeScript — всё в P4
	if ext == ".ts" || ext == ".tsx" {
		if tf {
			return "P5"
		}
		return "P4"
	}

	// Python: эвристическая классификация по ключевым словам
	// Проверяем как границы слов (через / _ - .), чтобы избежать ложных совпадений
	wordBoundary := func(s, kw string) bool {
		idx := strings.Index(s, kw)
		if idx < 0 {
			return false
		}
		// Проверяем границу слева
		if idx > 0 {
			prev := s[idx-1]
			if prev != '/' && prev != '_' && prev != '-' && prev != '.' {
				return false
			}
		}
		// Проверяем границу справа
		end := idx + len(kw)
		if end < len(s) {
			next := s[end]
			if next != '/' && next != '_' && next != '-' && next != '.' {
				return false
			}
		}
		return true
	}

	for _, kw := range p1Keywords {
		if wordBoundary(lower, kw) {
			if tf {
				return "P5"
			}
			return "P1"
		}
	}

	for _, kw := range p2Keywords {
		if wordBoundary(lower, kw) {
			if tf {
				return "P5"
			}
			return "P2"
		}
	}

	// Оставшиеся: тесты → P5, продуктовый → P3
	if tf {
		return "P5"
	}
	return "P3"
}

// --- Исключения ---

// Модули Ревизора — уже содержат self-tracing
var revizorModules = map[string]bool{
	"revizor_core_0_1_0":       true,
	"revizor_core_py_0_1_0":    true,
	"revizor_middleware_0_1_0": true,
	"revizor_api_0_1_0":        true,
	"revizor_mcp_0_1_0":        true,
}

// Исключённые модули (устаревшие, шаблоны)
var excludedModules = map[string]bool{
	"ais_prompter_api_0_1_0":  true,
	"ais_module_template_0_1_0": true,
}

// TS-файлы, исключённые из ревизоризации (конфиги, декларации типов)
var excludeTSPatterns = []*regexp.Regexp{
	regexp.MustCompile(`next\.config\.ts$`), regexp.MustCompile(`next-env\.d\.ts$`),
	regexp.MustCompile(`\.d\.ts$`),
	regexp.MustCompile(`jest\.config\.`), regexp.MustCompile(`tailwind\.config\.`),
	regexp.MustCompile(`postcss\.config\.`), regexp.MustCompile(`setup\.ts$`),
}

func isTargetExcludedDir(dirName string) bool {
	switch dirName {
	case "__pycache__", "node_modules", ".next", "dist", "alembic", "migrations", ".git":
		return true
	}
	return false
}

func shouldExcludeTarget(path string, d fs.DirEntry, cfg *config.Config) bool {
	// Проверка по компонентам пути
	parts := strings.Split(path, string(filepath.Separator))
	for _, part := range parts {
		if isTargetExcludedDir(part) {
			return true
		}
		if revizorModules[part] || excludedModules[part] {
			return true
		}
		// Исключения из конфига
		if cfg != nil {
			for _, exc := range cfg.Project.ExcludedDirs {
				if part == exc {
					return true
				}
			}
		}
	}

	fileName := d.Name()

	// __init__.py — исключаем только если практически пустой
	if fileName == "__init__.py" {
		content, err := os.ReadFile(path)
		if err == nil && len(strings.TrimSpace(string(content))) < 10 {
			return true
		}
		return false
	}

	// Конфигурационные TypeScript файлы
	ext := filepath.Ext(path)
	if ext == ".ts" || ext == ".tsx" {
		for _, pat := range excludeTSPatterns {
			if pat.MatchString(fileName) {
				return true
			}
		}
	}

	return false
}

// --- Подсчёт функций ---

var pyFuncPattern = regexp.MustCompile(`(?m)^\s*(?:async\s+)?def\s+(\w+)\s*\(`)

func countPythonFunctions(filePath string) int {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return 0
	}

	count := 0
	for _, match := range pyFuncPattern.FindAllStringSubmatch(string(content), -1) {
		funcName := match[1]
		if strings.HasPrefix(funcName, "_") && funcName != "__init__" {
			continue
		}
		if funcName == "__init__" || funcName == "__new__" || funcName == "__del__" {
			continue
		}
		count++
	}
	return count
}

// --- Оценка точек ---

func estimateTracePoints(funcCount int, isTest bool) int {
	if isTest {
		if funcCount > 0 {
			return funcCount * 2 // .enter + .success для тестов
		}
		return 3 // оценка для TypeScript: ~3 точки на файл
	}
	return funcCount * 3 // .enter + .success + .failed минимум
}

// --- Извлечение имени модуля ---

var moduleVerRe = regexp.MustCompile(`_\d+_\d+_\d+$`)

func getModuleNameFromTarget(path string, sourceRoot string) string {
	sr := sourceRoot
	if sr == "" {
		sr = "modules"
	}
	parts := strings.Split(path, string(filepath.Separator))
	for i, part := range parts {
		if part == sr && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	// Fallback: имя родительской директории
	dir := filepath.Dir(path)
	return filepath.Base(dir)
}

// --- Форматирование вывода ---

func formatStats(targets map[string][]TargetInfo) string {
	var allItems []TargetInfo
	for _, items := range targets {
		allItems = append(allItems, items...)
	}

	var pyProduct, pyTest, tsProduct, tsTest []TargetInfo
	for _, t := range allItems {
		if t.Language == "python" {
			if t.Type == "test" {
				pyTest = append(pyTest, t)
			} else {
				pyProduct = append(pyProduct, t)
			}
		} else {
			if t.Type == "test" {
				tsTest = append(tsTest, t)
			} else {
				tsProduct = append(tsProduct, t)
			}
		}
	}

	sumFuncs := func(items []TargetInfo) int {
		s := 0
		for _, t := range items {
			s += t.Functions
		}
		return s
	}
	sumPoints := func(items []TargetInfo) int {
		s := 0
		for _, t := range items {
			s += t.EstimatedPoints
		}
		return s
	}

	var sb strings.Builder
	sb.WriteString("\n============================================================\n")
	sb.WriteString("  REVIZORIZATION STATISTICS\n")
	sb.WriteString("============================================================\n")
	sb.WriteString(fmt.Sprintf("  Python product:      %4d files, ~%4d functions, ~%5d points\n",
		len(pyProduct), sumFuncs(pyProduct), sumPoints(pyProduct)))
	sb.WriteString(fmt.Sprintf("  Python test:         %4d files, ~%4d functions, ~%5d points\n",
		len(pyTest), sumFuncs(pyTest), sumPoints(pyTest)))
	sb.WriteString(fmt.Sprintf("  TypeScript product:  %4d files, ~%5d points\n",
		len(tsProduct), sumPoints(tsProduct)))
	sb.WriteString(fmt.Sprintf("  TypeScript test:     %4d files, ~%5d points\n",
		len(tsTest), sumPoints(tsTest)))
	sb.WriteString("  ----------------------------------------------------------\n")
	sb.WriteString(fmt.Sprintf("  TOTAL:               %4d files, ~%5d points\n",
		len(allItems), sumPoints(allItems)))
	sb.WriteString("\n")
	return sb.String()
}

func formatTable(targets map[string][]TargetInfo, group string) string {
	items := targets[group]
	if len(items) == 0 {
		return ""
	}

	totalFuncs := 0
	totalPoints := 0
	for _, t := range items {
		totalFuncs += t.Functions
		totalPoints += t.EstimatedPoints
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("============================================================\n"))
	sb.WriteString(fmt.Sprintf("  %s: %d files, ~%d functions, ~%d points\n",
		group, len(items), totalFuncs, totalPoints))
	sb.WriteString(fmt.Sprintf("============================================================\n"))

	for _, t := range items {
		lang := "[PY]"
		if t.Language == "typescript" {
			lang = "[TS]"
		}
		typeMark := "[=]"
		if t.Type == "test" {
			typeMark = "[T]"
		}
		sb.WriteString(fmt.Sprintf("  %s %s %s\n", typeMark, lang, t.Path))
		if t.Functions > 0 {
			sb.WriteString(fmt.Sprintf("         functions: %d, ~%d points\n", t.Functions, t.EstimatedPoints))
		} else if t.Language == "typescript" {
			sb.WriteString(fmt.Sprintf("         ~%d points\n", t.EstimatedPoints))
		}
	}
	sb.WriteString("\n")
	return sb.String()
}
