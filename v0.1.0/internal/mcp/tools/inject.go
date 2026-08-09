package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/license"
	"github.com/ais-platform/ais_products/revizor/internal/store"
)

// TraceInjectTool — массовая вставка trace()-вызовов в Python и TypeScript файлы.
// Поддерживает dry-run режим (unified diff) и атомарную запись с .bak.
// В Community-режиме (без лицензии) всегда работает в dry_run=true.
type TraceInjectTool struct {
	license *license.License
}

func (t *TraceInjectTool) Name() string { return "trace_inject" }

func (t *TraceInjectTool) SetLicense(lic *license.License) {
	t.license = lic
}

func (t *TraceInjectTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        "trace_inject",
		Description: "Mass insertion of trace() calls into Python or TypeScript source files. Supports dry-run mode (diff only) and automatic module name detection from path.",
		InputSchema: JSONSchema{
			Type: "object",
			Properties: map[string]PropSpec{
				"file_path":   stringProp("Path to source file (relative to project root)"),
				"language":    stringProp("Source language: 'python' or 'typescript'. Auto-detected from extension if omitted"),
				"module_name": stringProp("Module name for trace paths (e.g., 'auth'). Auto-detected from path if omitted"),
				"dry_run":     stringProp("Preview mode: return unified diff without modifying file (true/false). Default: true"),
			},
			Required: []string{"file_path"},
		},
	}
}

func (t *TraceInjectTool) Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error) {
	// Разбираем dry_run отдельно — он может быть bool или string ("true"/"false")
	var rawParams struct {
		FilePath   string          `json:"file_path"`
		Language   string          `json:"language"`
		ModuleName string          `json:"module_name"`
		DryRun     json.RawMessage `json:"dry_run"`
	}
	if err := parseArgs(args, &rawParams); err != nil {
		return fmt.Sprintf("Invalid argument: %v (check parameter types)", err), nil
	}

	// Преобразуем dry_run в строку: true/"true" → "true", false/"false" → "false", пусто → "true"
	dryRunStr := "true"
	if len(rawParams.DryRun) > 0 {
		var boolVal bool
		if err := json.Unmarshal(rawParams.DryRun, &boolVal); err == nil {
			if boolVal {
				dryRunStr = "true"
			} else {
				dryRunStr = "false"
			}
		} else {
			// Не bool — пробуем string
			var strVal string
			if err := json.Unmarshal(rawParams.DryRun, &strVal); err == nil {
				dryRunStr = strVal
			}
		}
	}

	params := struct {
		FilePath   string
		Language   string
		ModuleName string
		DryRun     string
	}{rawParams.FilePath, rawParams.Language, rawParams.ModuleName, dryRunStr}

	root, err := os.Getwd()
	if err != nil {
		return fmt.Sprintf("Failed to get working directory: %v", err), nil
	}

	filePath := params.FilePath
	if filepath.IsAbs(filePath) {
		// Абсолютный путь — используем как есть (уже содержит пробелы если нужно)
	} else {
		filePath = filepath.Join(root, filePath)
	}

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return fmt.Sprintf("FAIL: file not found — %s (resolved to %s)", params.FilePath, filePath), nil
	}

	// Определение языка
	lang := params.Language
	if lang == "" {
		lang = detectLanguage(filePath)
	}
	if lang != "python" && lang != "typescript" {
		return fmt.Sprintf("FAIL: unsupported language '%s'. Use 'python' or 'typescript'", lang), nil
	}

	// Определение имени модуля
	moduleName := params.ModuleName
	if moduleName == "" {
		moduleName = getModuleNameFromFilePath(filePath, cfg.Project.SourceRoot)
	}
	if moduleName == "" {
		return "FAIL: could not determine module name. Specify --module_name explicitly.", nil
	}

	// Dry-run: по умолчанию true для безопасности
	dryRun := true
	if params.DryRun == "false" {
		if t.license != nil && license.HasFeature(t.license, "inject_apply") {
			dryRun = false
		}
	}

	// Чтение оригинала
	original, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Sprintf("Failed to read file: %v", err), nil
	}

	var newContent string
	var count int

	if lang == "python" {
		isTest := isTestFile(filePath)
		newContent, count, err = injectPythonTraces(filePath, string(original), moduleName, isTest, cfg)
	} else {
		newContent, count, err = injectTypeScriptTraces(filePath, string(original), moduleName, cfg)
	}

	if err != nil {
		return fmt.Sprintf("Failed to inject trace points: %v", err), nil
	}

	if dryRun {
		if count > 0 {
			diff := unifiedDiff(string(original), newContent, params.FilePath)
			result := fmt.Sprintf("%s\n\nWould insert %d trace points", diff, count)
			// Предупреждение если запрошен apply но нет лицензии
			if params.DryRun == "false" && (t.license == nil || !license.HasFeature(t.license, "inject_apply")) {
				result += "\n⚠️  inject_apply requires a license with mcp_full feature. Only dry-run is available."
				result += "\n   Get a license at https://ais-platform.dev/revizor"
			}
			return result, nil
		}
		return fmt.Sprintf("No trace points to insert in %s (module: %s)", params.FilePath, moduleName), nil
	}

	// Запись (только с лицензией inject_apply)
	if count > 0 {
		if err := atomicWriteWithBackup(filePath, original, newContent); err != nil {
			return fmt.Sprintf("Failed to write file: %v", err), nil
		}
		return fmt.Sprintf("OK: inserted %d trace points in %s (module: %s, backup: %s.bak)\n\n⚠️  If the code runs in Docker or a virtual environment — clear __pycache__\n   and restart the process. Python may continue using old .pyc files.", count, params.FilePath, moduleName, filePath), nil
	}
	return fmt.Sprintf("No trace points to insert in %s (module: %s)", params.FilePath, moduleName), nil
}

// =========================================================================
// Константы
// =========================================================================

// ---- Импорты (настраиваемые через конфиг) ----

// Defaults (обратная совместимость)
const defaultPyImportStmt = "from revizor_sdk import trace, trace_start\n"
const defaultPyImportPattern = "from revizor_sdk import trace"
const defaultTsImportStmt = `import { trace } from "@ais-platform/revizor-sdk";` + "\n"

func getPythonImportStmt(cfg *config.Config) string {
	if cfg != nil && cfg.Project.TraceImports.PythonImportStmt != "" {
		return cfg.Project.TraceImports.PythonImportStmt
	}
	return defaultPyImportStmt
}

func getPythonImportPattern(cfg *config.Config) string {
	if cfg != nil && cfg.Project.TraceImports.PythonImportPattern != "" {
		return cfg.Project.TraceImports.PythonImportPattern
	}
	return defaultPyImportPattern
}

func getTypeScriptImportStmt(cfg *config.Config) string {
	if cfg != nil && cfg.Project.TraceImports.TypeScriptImportStmt != "" {
		return cfg.Project.TraceImports.TypeScriptImportStmt
	}
	return defaultTsImportStmt
}

// Ключевые слова для обнаружения критических решений
var decisionKeywords = []string{
	"provide", "link_mode", "same_provider", "access_denied",
	"elevation", "permission", "role", "is_admin",
}

// Методы, которые всегда пропускаются
var skipMethods = map[string]bool{
	"__init__": true, "__new__": true, "__del__": true, "__repr__": true, "__str__": true,
	"__eq__": true, "__hash__": true, "__len__": true, "__iter__": true, "__next__": true,
	"__enter__": true, "__exit__": true, "__getattr__": true, "__setattr__": true,
}

// Чувствительные параметры — не логируются
var sensitiveArgNames = map[string]bool{
	"password": true, "secret": true, "token": true, "api_key": true, "credential": true,
	"private_key": true, "access_key": true, "auth_token": true, "bearer": true, "config": true,
}

// =========================================================================
// Определение языка и модуля
// =========================================================================

func detectLanguage(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".py":
		return "python"
	case ".ts", ".tsx":
		return "typescript"
	}
	return ""
}

func isTestFile(filePath string) bool {
	base := filepath.Base(filePath)
	// test_*.py (Python convention)
	if strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py") {
		return true
	}
	// *.test.ts, *.test.tsx, *.spec.ts, *.spec.tsx (JS/TS convention)
	if strings.HasSuffix(base, ".test.ts") || strings.HasSuffix(base, ".test.tsx") ||
		strings.HasSuffix(base, ".spec.ts") || strings.HasSuffix(base, ".spec.tsx") {
		return true
	}
	// tests/ директория в пути
	if strings.Contains(filePath, string(filepath.Separator)+"tests"+string(filepath.Separator)) {
		return true
	}
	// __tests__/ директория (Jest convention)
	if strings.Contains(filePath, string(filepath.Separator)+"__tests__"+string(filepath.Separator)) {
		return true
	}
	return false
}

var moduleVersionRe = regexp.MustCompile(`_\d+_\d+_\d+$`)

func getModuleNameFromFilePath(filePath string, sourceRoot string) string {
	// 1. Ищем директорию после sourceRoot (или "modules" для обратной совместимости)
	sr := sourceRoot
	if sr == "" {
		sr = "modules"
	}
	parts := strings.Split(filePath, string(filepath.Separator))
	for i, part := range parts {
		if part == sr && i+1 < len(parts) {
			return moduleVersionRe.ReplaceAllString(parts[i+1], "")
		}
	}
	// 2. Fallback: родительская директория файла
	dir := filepath.Dir(filePath)
	parent := filepath.Base(dir)
	if parent != "" && parent != "." {
		return moduleVersionRe.ReplaceAllString(parent, "")
	}
	// 3. Последний fallback: имя файла без расширения
	base := filepath.Base(filePath)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	return moduleVersionRe.ReplaceAllString(stem, "")
}

// =========================================================================
// Python Regex-парсер (замена AST)
// =========================================================================

var (
	pyFuncDefRe   = regexp.MustCompile(`(?m)^(\s*)(?:async\s+)?def\s+(\w+)\s*\(`)
	pyRaiseRe     = regexp.MustCompile(`(?m)^(\s*)raise\s+(\w*)`)
	pyDecoratorRe = regexp.MustCompile(`@(property|cached_property)\b`)
)

// insertion — запланированная вставка строки.
type insertion struct {
	lineIdx float64 // дробная часть для упорядочивания вставок на одной строке
	text    string
}

// replacement — замена одной строки на несколько новых (REV-013: разбиение однострочных функций).
type replacement struct {
	lineIdx int
	lines   []string
}

// injectPythonTraces вставляет trace-вызовы в Python-файл через regex.
func injectPythonTraces(filePath, original, moduleName string, isTest bool, cfg *config.Config) (string, int, error) {
	lines := strings.SplitAfter(original, "\n")
	// Убираем фиктивную пустую строку в конце если original не заканчивается на \n
	if original != "" && !strings.HasSuffix(original, "\n") {
		// Восстановим оригинальное поведение: последняя строка без \n
		lastIdx := len(lines) - 1
		if lastIdx >= 0 && lines[lastIdx] == "" {
			lines = lines[:lastIdx]
		}
	} else if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	var insertions []insertion
	var replacements []replacement

	// ---- Шаг 1: Импорт ----
	pyImportStmt := getPythonImportStmt(cfg)
	pyImportPattern := getPythonImportPattern(cfg)
	if !strings.Contains(original, pyImportPattern) {
		lastImportIdx := findLastModuleImport(lines, isTest)
		if lastImportIdx >= 0 {
			insertions = append(insertions, insertion{float64(lastImportIdx) + 1.0, pyImportStmt + "\n"})
		} else {
			// Нет импортов — вставить после docstring модуля
			insertAt := 0
			for i, line := range lines {
				stripped := strings.TrimSpace(line)
				if strings.HasPrefix(stripped, `"""`) || strings.HasPrefix(stripped, "'''") {
					quote := stripped[:3]
					if strings.Count(stripped, quote) >= 2 && len(stripped) > 5 {
						insertAt = i + 1
						continue
					}
					j := i + 1
					for j < len(lines) && !strings.Contains(lines[j], quote) {
						j++
					}
					insertAt = j + 1
					continue
				}
				if stripped != "" && !strings.HasPrefix(stripped, "#") {
					break
				}
				insertAt = i + 1
			}
			insertions = append(insertions, insertion{float64(insertAt), pyImportStmt + "\n"})
		}
	}

	// ---- Шаг 2: Первый проход — только replacements (однострочники) ----
	// Разбиваем однострочные функции на многострочные, чтобы индексы строк
	// для последующих insertions считались на уже изменённом коде.
	funcInfos := findPythonFunctions(original, lines, isTest)
	for _, fi := range funcInfos {
		if fi.isOneLiner {
			processPythonFunction(fi, lines, moduleName, isTest, &insertions, &replacements)
		}
	}
	if len(replacements) > 0 {
		lines = applyReplacements(lines, replacements)
	}
	// Сохраняем импорт из первого прохода (индекс правильный — он был
	// добавлен до replacements и не зависит от изменений строк)
	var importInsertion *insertion
	for i := range insertions {
		if strings.Contains(insertions[i].text, "import trace") ||
			strings.Contains(insertions[i].text, "revizor_sdk") ||
			strings.Contains(insertions[i].text, "revizor-sdk") {
			importInsertion = &insertions[i]
			break
		}
	}
	// Сбрасываем insertions первого прохода (кроме импорта) —
	// их индексы указывают на старые lines до replacements
	insertions = nil
	if importInsertion != nil {
		insertions = append(insertions, *importInsertion)
	}
	replacements = nil

	// ---- Шаг 3: Второй проход — insertions на изменённых линиях ----
	funcInfos = findPythonFunctions(strings.Join(lines, ""), lines, isTest)
	for _, fi := range funcInfos {
		processPythonFunction(fi, lines, moduleName, isTest, &insertions, &replacements)
	}
	// Replacements от второго прохода (новых однострочников быть не должно,
	// но processPythonFunction может добавить для вложенных функций)
	if len(replacements) > 0 {
		lines = applyReplacements(lines, replacements)
	}

	// ---- Шаг 4: Дедубликация и сортировка ----
	insertions = dedupAndSort(insertions)

	// ---- Шаг 5: Применить вставки ----
	newLines := applyInsertions(lines, insertions)
	newContent := strings.Join(newLines, "")

	// Валидация: проверяем что результат — валидный Python
	if countInsertions(insertions) > 0 {
		if !basicPyValidation(newContent) {
			return original, 0, fmt.Errorf("result would contain syntax errors — aborting")
		}
	}

	return newContent, countInsertions(insertions), nil
}

// pythonFuncInfo — информация о функции, извлечённая через regex.
type pythonFuncInfo struct {
	name         string
	defLineIdx   int    // 0-based индекс строки с def
	indent       string // отступ def
	bodyStartIdx int    // 0-based индекс начала тела
	bodyEndIdx   int    // 0-based индекс конца тела (приблизительно)
	isOneLiner   bool   // REV-013: тело на той же строке что и def
}

// findPythonFunctions находит все функции в Python-коде через regex.
func findPythonFunctions(original string, lines []string, isTest bool) []pythonFuncInfo {
	var funcs []pythonFuncInfo
	allMatches := pyFuncDefRe.FindAllStringSubmatchIndex(original, -1)

	for _, match := range allMatches {
		indent := strings.TrimLeft(original[match[2]:match[3]], "\n\r") // группа отступа без пустых строк
		funcName := original[match[4]:match[5]]

		if !isTest {
			if strings.HasPrefix(funcName, "_") {
				continue
			}
			if skipMethods[funcName] {
				continue
			}
		}

		// Найти номер строки
		defLineIdx := strings.Count(original[:match[0]], "\n")
		// REV-013: regex может захватить \n перед def в группу (\s*).
		// Тогда defLineIdx указывает на пустую строку — сдвигаемся.
		for defLineIdx < len(lines) && strings.TrimSpace(lines[defLineIdx]) == "" {
			defLineIdx++
		}

		// Проверка @property/@cached_property над функцией
		if !isTest && hasPropertyDecorator(lines, defLineIdx) {
			continue
		}

		// Проверка пустого тела (pass или ...)
		if !isTest && hasEmptyBody(lines, defLineIdx) {
			continue
		}

		// Найти начало тела (после docstring)
		bodyStart := getPyBodyStart(lines, defLineIdx)

		// Найти конец тела (приблизительно — следующий блок с тем же или меньшим отступом)
		bodyEnd := getPyBodyEnd(lines, defLineIdx, indent)

		funcs = append(funcs, pythonFuncInfo{
			name:         funcName,
			defLineIdx:   defLineIdx,
			indent:       indent,
			bodyStartIdx: bodyStart,
			bodyEndIdx:   bodyEnd,
			isOneLiner:   isOneLineFunc(lines[defLineIdx]),
		})
	}

	// Корректируем bodyEnd для ВСЕХ функций уровня модуля: конец = начало следующей
	for i := 0; i < len(funcs)-1; i++ {
		if funcs[i].bodyEndIdx > funcs[i+1].defLineIdx {
			funcs[i].bodyEndIdx = funcs[i+1].defLineIdx
		}
	}

	return funcs
}

// processPythonFunction обрабатывает одну функцию: добавляет .enter, .success, .failed, .decision.
func processPythonFunction(fi pythonFuncInfo, lines []string, moduleName string, isTest bool, insertions *[]insertion, replacements *[]replacement) {
	var traceBase string
	if isTest {
		traceBase = moduleName + ".test." + fi.name
	} else {
		traceBase = moduleName + "." + fi.name
	}

	bodyIndent := fi.indent + "    " // 4 пробела для тела функции

	// ---- .enter ----
	if fi.isOneLiner {
		// REV-013: разбиваем однострочную функцию на многострочную и вставляем .enter.
		// REV-026: сохраняем исходный отступ def-строки при разбиении.
		if fi.defLineIdx < len(lines) && !hasExistingEnter(lines, fi.defLineIdx, fi.bodyEndIdx) {
			defLine := lines[fi.defLineIdx]
			stripped := strings.TrimSpace(defLine)
			colonIdx := strings.LastIndex(stripped, ":")
			if colonIdx >= 0 {
				beforeColon := stripped[:colonIdx+1]
				afterColon := strings.TrimSpace(stripped[colonIdx+1:])
				if afterColon == ")" {
					afterColon = ""
				}
				if afterColon != "" && !strings.HasPrefix(afterColon, "#") {
					defIndent := computeLineIndent(defLine)
					newLines := []string{
						defIndent + beforeColon + "\n",
						defIndent + "    " + `trace("` + traceBase + `.enter")` + "\n",
						defIndent + "    " + afterColon + "\n",
					}
					*replacements = append(*replacements, replacement{fi.defLineIdx, newLines})
				}
			}
		}
	} else {
		if fi.bodyStartIdx < len(lines) && !hasExistingEnter(lines, fi.defLineIdx, fi.bodyEndIdx) {
			actualIndent := computeLineIndent(lines[fi.bodyStartIdx])
			if actualIndent == "" {
				actualIndent = bodyIndent
			}
			enterLine := actualIndent + `trace("` + traceBase + `.enter")` + "\n"
			*insertions = append(*insertions, insertion{float64(fi.bodyStartIdx), enterLine})
		}
	}

	// ---- .success перед return (не None) ----
	// REV-013: для однострочных .success не вставляем — нет места перед return.
	if !fi.isOneLiner {
		seenLines := make(map[int]bool)
		for i := fi.defLineIdx + 1; i < fi.bodyEndIdx && i < len(lines); i++ {
		line := lines[i]
		if !strings.Contains(line, "return ") {
			continue
		}
		// Пропуск return None
		if matched, _ := regexp.MatchString(`return\s+None\s*$`, strings.TrimSpace(line)); matched {
			continue
		}
		if seenLines[i] {
			continue
		}
		seenLines[i] = true

		// Проверка на уже существующий .success/.failed в функции
		if hasExistingSuccessOrFailed(lines, fi.defLineIdx, i) {
			continue
		}

		retIndent := computeLineIndent(line)
		successLine := retIndent + `trace("` + traceBase + `.success")` + "\n"
			*insertions = append(*insertions, insertion{float64(i) + 0.1, successLine})
		}
	} // fi.isOneLiner

	// ---- .failed перед raise ----
	seenRaiseLines := make(map[int]bool)
	for i := fi.defLineIdx + 1; i < fi.bodyEndIdx && i < len(lines); i++ {
		line := lines[i]
		if matched := pyRaiseRe.MatchString(line); !matched {
			continue
		}
		if seenRaiseLines[i] {
			continue
		}
		seenRaiseLines[i] = true

		// Проверка на уже существующий .failed
		if hasExistingFailed(lines, fi.defLineIdx, i) {
			continue
		}

		raiseIndent := computeLineIndent(line)
		failedLine := raiseIndent + `trace("` + traceBase + `.failed")` + "\n"
		*insertions = append(*insertions, insertion{float64(i) + 0.1, failedLine})
	}

	// ---- .decision для критических if ----
	seenDecisionLines := make(map[int]bool)
	for i := fi.defLineIdx + 1; i < fi.bodyEndIdx && i < len(lines); i++ {
		line := lines[i]
		if !strings.Contains(line, "if ") && !strings.Contains(line, "elif ") {
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(line), "if ") && !strings.HasPrefix(strings.TrimSpace(line), "elif ") {
			continue
		}
		if seenDecisionLines[i] {
			continue
		}
		seenDecisionLines[i] = true

		conditionText := getConditionText(line)
		if !isCriticalDecision(conditionText) {
			continue
		}

		condEnd := findConditionEnd(lines, i)
		if condEnd < len(lines) {
			// BUG-GO-11: проверка на уже существующий .decision
			if hasExistingDecision(lines, fi.defLineIdx, condEnd) {
				continue
			}
			decName := decisionName(conditionText)
			decIndent := computeLineIndent(line) + "    "
			decisionLine := fmt.Sprintf(`%strace("%s.decision.%s", {"condition": %q})`+"\n",
				decIndent, traceBase, decName, conditionText)
			*insertions = append(*insertions, insertion{float64(condEnd) + 0.2, decisionLine})
		}
	}
}

// =========================================================================
// Утилиты для Python-парсинга
// =========================================================================

func computeLineIndent(line string) string {
	stripped := strings.TrimLeft(line, " \t")
	return line[:len(line)-len(stripped)]
}

// isOneLineFunc проверяет, содержит ли строка с определением функции тело на той же строке.
func isOneLineFunc(defLine string) bool {
	stripped := strings.TrimSpace(defLine)
	if !(strings.HasPrefix(stripped, "def ") || strings.HasPrefix(stripped, "async def ")) {
		return false
	}
	if !strings.Contains(stripped, "):") && !strings.Contains(stripped, ") ->") {
		return false
	}
	colonIdx := strings.LastIndex(stripped, ":")
	if colonIdx < 0 {
		return false
	}
	afterColon := strings.TrimSpace(stripped[colonIdx+1:])
	if afterColon == ")" {
		afterColon = ""
	}
	return afterColon != "" && !strings.HasPrefix(afterColon, "#")
}

func getPyBodyStart(lines []string, defLineIdx int) int {
	// Ищем конец сигнатуры — строку, где parenDepth <= 0 и есть ':'
	// (конец сигнатуры, а не аннотация типа внутри скобок).
	// Обрабатываем оба варианта: '):' (обычный) и ') ->' (с аннотацией возврата).
	sigEnd := defLineIdx
	parenDepth := 0
	for sigEnd < len(lines) {
		stripped := strings.TrimSpace(lines[sigEnd])
		parenDepth += strings.Count(stripped, "(") - strings.Count(stripped, ")")
		if parenDepth <= 0 {
			// Проверяем конец сигнатуры.
			// Два варианта:
			// 1. '):' — скобка+двоеточие без пробела. colonIdx = позиция ')'.
			//    afterColon = stripped[colonIdx+2:] (пропускаем оба символа).
			// 2. ') -> ... :' — скобка, стрелка, двоеточие (аннотация возврата).
			//    colonIdx = позиция ':'. afterColon = stripped[colonIdx+1:].
			colonIdx := strings.LastIndex(stripped, "):")
			afterColon := ""
			if colonIdx >= 0 {
				// Вариант 1: '):' — afterColon после двух символов
				afterColon = strings.TrimSpace(stripped[colonIdx+2:])
			} else {
				// Вариант 2: ') ->' — ищем двоеточие после стрелки
				parenIdx := strings.LastIndex(stripped, ") ->")
				if parenIdx >= 0 {
					rest := stripped[parenIdx+2:] // после ') '
					if arrowColon := strings.LastIndex(rest, ":"); arrowColon >= 0 {
						colonIdx = parenIdx + 2 + arrowColon
						afterColon = strings.TrimSpace(stripped[colonIdx+1:])
					}
				}
			}
			if colonIdx >= 0 {
				if afterColon == ")" {
					afterColon = ""
				}
				// Однострочник: после ':' есть код (не комментарий)
				isOneLiner := afterColon != "" &&
					!strings.HasPrefix(afterColon, "#")
				if isOneLiner {
					return sigEnd + 1
				}
				sigEnd++
				break
			}
		}
		sigEnd++
	}
	// Пропускаем docstring и комментарии после сигнатуры
	for sigEnd < len(lines) {
		stripped := strings.TrimSpace(lines[sigEnd])
		if strings.HasPrefix(stripped, `"""`) || strings.HasPrefix(stripped, "'''") {
			quote := stripped[:3]
			if strings.Count(stripped, quote) >= 2 {
				return sigEnd + 1
			}
			sigEnd++
			for sigEnd < len(lines) {
				if strings.Contains(lines[sigEnd], quote) {
					return sigEnd + 1
				}
				sigEnd++
			}
			return sigEnd
		}
		if strings.HasPrefix(stripped, "#") || stripped == "" {
			sigEnd++
			continue
		}
		return sigEnd
	}
	return sigEnd
}

func getPyBodyEnd(lines []string, defLineIdx int, defIndent string) int {
	for i := defLineIdx + 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}
		lineIndent := computeLineIndent(line)
		if len(lineIndent) <= len(defIndent) && lineIndent != "" {
			// Следующий блок на том же уровне или внешний — конец функции
			return i
		}
	}
	return len(lines)
}

func hasPropertyDecorator(lines []string, defLineIdx int) bool {
	if defLineIdx > 0 {
		prev := strings.TrimSpace(lines[defLineIdx-1])
		if pyDecoratorRe.MatchString(prev) {
			return true
		}
	}
	return false
}

func hasEmptyBody(lines []string, defLineIdx int) bool {
	bodyStart := getPyBodyStart(lines, defLineIdx)
	if bodyStart < len(lines) {
		stripped := strings.TrimSpace(lines[bodyStart])
		if stripped == "pass" || stripped == "..." {
			return true
		}
	}
	return false
}

func hasExistingEnter(lines []string, defLineIdx, bodyEnd int) bool {
	for i := defLineIdx + 1; i <= bodyEnd && i < len(lines); i++ {
		if strings.Contains(lines[i], `trace("`) && strings.Contains(lines[i], ".enter") {
			return true
		}
	}
	return false
}

func hasExistingSuccessOrFailed(lines []string, defLineIdx, retLineIdx int) bool {
	for i := defLineIdx + 1; i <= retLineIdx && i < len(lines); i++ {
		line := lines[i]
		if strings.Contains(line, `trace("`) && (strings.Contains(line, ".success") || strings.Contains(line, ".failed")) {
			return true
		}
	}
	return false
}

func hasExistingFailed(lines []string, defLineIdx, raiseLineIdx int) bool {
	for i := defLineIdx + 1; i <= raiseLineIdx && i < len(lines); i++ {
		if strings.Contains(lines[i], `trace("`) && strings.Contains(lines[i], ".failed") {
			return true
		}
	}
	return false
}

// Проверка на уже существующий .decision перед вставкой.
// BUG-GO-11 (исправлено): condEndLine указывает на первую строку тела условия,
// поэтому проверяем i <= condEndLine чтобы захватить trace на первой строке тела.
func hasExistingDecision(lines []string, defLineIdx, condEndLine int) bool {
	for i := defLineIdx + 1; i <= condEndLine && i < len(lines); i++ {
		if strings.Contains(lines[i], `trace("`) && strings.Contains(lines[i], ".decision.") {
			return true
		}
	}
	return false
}

func getConditionText(line string) string {
	stripped := strings.TrimSpace(line)
	if strings.HasPrefix(stripped, "if ") {
		return strings.TrimSuffix(strings.TrimPrefix(stripped, "if "), ":")
	}
	if strings.HasPrefix(stripped, "elif ") {
		return strings.TrimSuffix(strings.TrimPrefix(stripped, "elif "), ":")
	}
	return ""
}

func isCriticalDecision(condition string) bool {
	lower := strings.ToLower(condition)
	for _, kw := range decisionKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func decisionName(condition string) string {
	name := strings.ToLower(condition)
	re := regexp.MustCompile(`[^a-z0-9_]+`)
	name = re.ReplaceAllString(name, "_")
	name = regexp.MustCompile(`_+`).ReplaceAllString(name, "_")
	name = strings.Trim(name, "_")
	if len(name) > 40 {
		name = name[:40]
	}
	return name
}

func findConditionEnd(lines []string, ifLine int) int {
	stripped := strings.TrimSpace(lines[ifLine])
	if strings.HasSuffix(strings.TrimSpace(stripped), `\`) {
		j := ifLine
		for j < len(lines) && strings.HasSuffix(strings.TrimSpace(lines[j]), `\`) {
			j++
		}
		return j + 1
	}
	depth := strings.Count(stripped, "(") - strings.Count(stripped, ")")
	if depth <= 0 {
		return ifLine + 1
	}
	for j := ifLine + 1; j < len(lines); j++ {
		s := strings.TrimSpace(lines[j])
		if strings.HasPrefix(s, "#") {
			continue
		}
		depth += strings.Count(s, "(") - strings.Count(s, ")")
		if depth <= 0 {
			return j + 1
		}
	}
	return ifLine + 1
}

// findLastModuleImport ищет последний импорт на уровне модуля (без отступа).
func findLastModuleImport(lines []string, isTest bool) int {
	if isTest {
		return findLastSysPathOrImport(lines)
	}

	last := -1
	for i, line := range lines {
		stripped := strings.TrimSpace(line)
		if stripped == "" {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			continue
		}
		if strings.HasPrefix(stripped, "import ") || strings.HasPrefix(stripped, "from ") {
			last = i
			if strings.Contains(stripped, "(") && !strings.Contains(stripped, ")") {
				last = findClosingParen(lines, i)
			}
		} else if strings.HasPrefix(stripped, "#") || strings.HasPrefix(stripped, `"""`) || strings.HasPrefix(stripped, "'''") {
			continue
		} else {
			if last >= 0 {
				return last
			}
		}
	}
	return last
}

func findLastSysPathOrImport(lines []string) int {
	for i, line := range lines {
		stripped := strings.TrimSpace(line)
		if strings.Contains(stripped, "sys.path") && (line[0] != ' ' && line[0] != '\t') {
			return i + 1
		}
	}
	return findLastModuleImport(lines, false)
}

// pascalToSnake конвертирует PascalCase/camelCase в snake_case.
// Используется для имён React-компонентов и функций TypeScript,
// чтобы trace-пути соответствовали RE-033: [a-z0-9_.]+.
func pascalToSnake(s string) string {
	re := regexp.MustCompile(`([a-z0-9])([A-Z])`)
	result := re.ReplaceAllString(s, "${1}_${2}")
	re2 := regexp.MustCompile(`([A-Z]+)([A-Z][a-z])`)
	result = re2.ReplaceAllString(result, "${1}_${2}")
	return strings.ToLower(result)
}

func findClosingParen(lines []string, startIdx int) int {
	depth := 0
	for i := startIdx; i < len(lines); i++ {
		stripped := strings.TrimSpace(lines[i])
		if strings.HasPrefix(stripped, "#") {
			continue
		}
		depth += strings.Count(stripped, "(") - strings.Count(stripped, ")")
		if depth <= 0 {
			return i
		}
	}
	return startIdx
}

// basicPyValidation — базовая проверка что скобки сбалансированы.
func basicPyValidation(code string) bool {
	parens := 0
	braces := 0
	brackets := 0
	inString := false
	stringChar := byte(0)

	for i := 0; i < len(code); i++ {
		ch := code[i]
		if inString {
			if ch == '\\' {
				i++ // skip escaped
				continue
			}
			if ch == stringChar {
				inString = false
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			inString = true
			stringChar = ch
			continue
		}
		switch ch {
		case '(':
			parens++
		case ')':
			parens--
		case '{':
			braces++
		case '}':
			braces--
		case '[':
			brackets++
		case ']':
			brackets--
		}
	}
	return parens == 0 && braces == 0 && brackets == 0
}

// =========================================================================
// TypeScript Regex-инжектор
// =========================================================================

var (
	tsFuncRe  = regexp.MustCompile(`(?m)^(\s*)(?:export\s+)?(?:async\s+)?function\s+(\w+)`)
	tsArrowRe = regexp.MustCompile(`(?m)^(\s*)(?:export\s+)?(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s*)?\([^)]*\)\s*(?::\s*\w+)?\s*=>`)
)

func injectTypeScriptTraces(filePath, original, moduleName string, cfg *config.Config) (string, int, error) {
	lines := strings.SplitAfter(original, "\n")
	if original != "" && !strings.HasSuffix(original, "\n") {
		lastIdx := len(lines) - 1
		if lastIdx >= 0 && lines[lastIdx] == "" {
			lines = lines[:lastIdx]
		}
	} else if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	tsx := strings.HasSuffix(filePath, ".tsx")
	var insertions []insertion

	// ---- Импорт ----
	tsImportStmt := getTypeScriptImportStmt(cfg)
	if !strings.Contains(original, `revizor-sdk`) && !strings.Contains(original, `revizor_core_ts`) && !strings.Contains(original, `revizor-ui`) {
		lastImport := findLastTSImport(lines)
		if lastImport >= 0 {
			insertions = append(insertions, insertion{float64(lastImport) + 1.0, "\n" + tsImportStmt})
		}
	}

	// ---- Поиск функций (function keyword) ----
	for _, match := range tsFuncRe.FindAllStringSubmatchIndex(original, -1) {
		indent := original[match[2]:match[3]]
		funcName := original[match[4]:match[5]]
		if strings.HasPrefix(funcName, "_") {
			continue
		}
		defLineIdx := strings.Count(original[:match[0]], "\n")

		// BUG-GO-10: пропустить функции внутри interface/type/enum блоков
		if isInsideTSDeclaration(lines, defLineIdx) {
			continue
		}

		// RE-033: trace-пути только lowercase [a-z0-9_.]+
		snakeFuncName := pascalToSnake(funcName)

		braceLine := findOpeningBrace(lines, defLineIdx)
		bodyEnd := getTSBodyEnd(lines, braceLine)
		bodyIndent := indent + "    "

		// React-компонент (.mounted) — REV-020: проверка на существующий
		if tsx && len(funcName) > 0 && funcName[0] >= 'A' && funcName[0] <= 'Z' {
			if braceLine >= 0 && !hasTSExistingEnter(lines, braceLine, bodyEnd) {
				// hasTSExistingEnter проверяет .enter, но mounted обычно рядом —
				// дополнительная проверка на .mounted
				if !hasTSExistingTrace(lines, braceLine, bodyEnd, ".mounted") {
					mountedLine := bodyIndent + `trace("` + moduleName + `.` + snakeFuncName + `.mounted");` + "\n"
					insertions = append(insertions, insertion{float64(braceLine) + 1.2, mountedLine})
				}
			}
		}

		// .enter
		if braceLine >= 0 {
			if !hasTSExistingEnter(lines, braceLine, bodyEnd) {
				enterLine := bodyIndent + `trace("` + moduleName + `.` + snakeFuncName + `.enter");` + "\n"
				insertions = append(insertions, insertion{float64(braceLine) + 1.3, enterLine})
			}
		}

		// .success перед return, .decision в if-блоках
		processTSFunctionBody(lines, braceLine+1, bodyEnd, bodyIndent, moduleName, snakeFuncName, &insertions)
	}

	// ---- Стрелочные функции ----
	for _, match := range tsArrowRe.FindAllStringSubmatchIndex(original, -1) {
		indent := original[match[2]:match[3]]
		funcName := original[match[4]:match[5]]
		if strings.HasPrefix(funcName, "_") {
			continue
		}
		defLineIdx := strings.Count(original[:match[0]], "\n")

		// BUG-GO-10: пропустить функции внутри interface/type/enum блоков
		if isInsideTSDeclaration(lines, defLineIdx) {
			continue
		}

		// RE-033: trace-пути только lowercase [a-z0-9_.]+
		snakeFuncName := pascalToSnake(funcName)

		braceLine := findOpeningBrace(lines, defLineIdx)
		bodyEnd := getTSBodyEnd(lines, braceLine)
		bodyIndent := indent + "    "

		if braceLine >= 0 {
			if !hasTSExistingEnter(lines, braceLine, bodyEnd) {
				enterLine := bodyIndent + `trace("` + moduleName + `.` + snakeFuncName + `.enter");` + "\n"
				insertions = append(insertions, insertion{float64(braceLine) + 1.3, enterLine})
			}
		}

		// .success и .decision для стрелочных функций
		processTSFunctionBody(lines, braceLine+1, bodyEnd, bodyIndent, moduleName, snakeFuncName, &insertions)
	}

	// ---- Применить ----
	insertions = dedupAndSort(insertions)
	newLines := applyInsertions(lines, insertions)
	newContent := strings.Join(newLines, "")

	return newContent, countInsertions(insertions), nil
}

func findOpeningBrace(lines []string, defLine int) int {
	for i := defLine; i < len(lines); i++ {
		if strings.Contains(lines[i], "{") {
			return i
		}
	}
	return defLine + 1
}

// getTSBodyEnd находит конец тела TypeScript-функции по фигурным скобкам.
func getTSBodyEnd(lines []string, openBraceLine int) int {
	if openBraceLine < 0 || openBraceLine >= len(lines) {
		return len(lines)
	}
	depth := 0
	for i := openBraceLine; i < len(lines); i++ {
		depth += strings.Count(lines[i], "{") - strings.Count(lines[i], "}")
		if depth <= 0 {
			return i + 1 // индекс за закрывающей }
		}
	}
	return len(lines)
}

// isInsideTSDeclaration проверяет, находится ли defLine внутри TypeScript-блока
// interface { }, type Foo { }, или enum { }. Идём ВВЕРХ от defLine, отслеживая
// баланс фигурных скобок. Если встречаем interface/type/enum на depth=0 — true.
// BUG-GO-10: предотвращает вставку trace() внутрь объявлений типов.
func isInsideTSDeclaration(lines []string, defLine int) bool {
	depth := 0
	for i := defLine - 1; i >= 0; i-- {
		line := lines[i]
		// Сначала проверяем declaration на текущем depth (до учёта скобок этой строки).
		// Это предотвращает ложное срабатывание на declaration, которая уже закрыта:
		// идя вверх, мы могли выйти из блока (depth=0), затем встретить declaration
		// с '{' — её скобки увеличат depth, а не уменьшат до 0.
		if depth <= 0 {
			if isDeclarationLine(strings.TrimSpace(line)) {
				return true
			}
		}
		depth += strings.Count(line, "}") - strings.Count(line, "{")
		if depth < 0 {
			depth = 0 // закрыли больше чем открыли — выравниваем
		}
	}
	return false
}

// isDeclarationLine проверяет, является ли строка объявлением interface/enum/type.
func isDeclarationLine(line string) bool {
	// Убираем export/declare/default
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "export ")
	line = strings.TrimPrefix(line, "declare ")
	line = strings.TrimPrefix(line, "default ")
	line = strings.TrimSpace(line)

	if strings.HasPrefix(line, "interface ") && strings.Contains(line, "{") {
		return true
	}
	if strings.HasPrefix(line, "enum ") && strings.Contains(line, "{") {
		return true
	}
	if strings.HasPrefix(line, "type ") && strings.Contains(line, "{") && !strings.Contains(line, "=") {
		return true
	}
	return false
}

// hasTSExistingTrace проверяет, есть ли trace с указанным суффиксом в теле функции.
func hasTSExistingTrace(lines []string, braceLine, bodyEnd int, suffix string) bool {
	for i := braceLine + 1; i < bodyEnd && i < len(lines); i++ {
		if strings.Contains(lines[i], `trace("`) && strings.Contains(lines[i], suffix) {
			return true
		}
	}
	return false
}

// hasTSExistingEnter проверяет, есть ли уже .enter trace в теле функции.
func hasTSExistingEnter(lines []string, braceLine, bodyEnd int) bool {
	for i := braceLine + 1; i < bodyEnd && i < len(lines); i++ {
		if strings.Contains(lines[i], `trace("`) && strings.Contains(lines[i], ".enter") {
			return true
		}
	}
	return false
}

// hasTSExistingSuccessOrFailed проверяет, есть ли .success/.failed перед позицией retLine.
func hasTSExistingSuccessOrFailed(lines []string, bodyStart, retLine int) bool {
	for i := bodyStart; i < retLine && i < len(lines); i++ {
		if strings.Contains(lines[i], `trace("`) && (strings.Contains(lines[i], ".success") || strings.Contains(lines[i], ".failed")) {
			return true
		}
	}
	return false
}

// processTSFunctionBody обрабатывает тело TS-функции: .success перед return, .decision в if.
func processTSFunctionBody(lines []string, bodyStart, bodyEnd int, bodyIndent, moduleName, funcName string, insertions *[]insertion) {
	traceBase := moduleName + "." + funcName
	seenReturnLines := make(map[int]bool)
	seenDecisionLines := make(map[int]bool)

	for i := bodyStart; i < bodyEnd && i < len(lines); i++ {
		stripped := strings.TrimSpace(lines[i])

		// .success перед return (не return; без значения)
		if strings.HasPrefix(stripped, "return ") || stripped == "return" {
			if seenReturnLines[i] {
				continue
			}
			seenReturnLines[i] = true

			// Пропускаем пустой return (return; без аргументов) — не замеряем
			if stripped == "return" || strings.HasPrefix(stripped, "return;") {
				continue
			}

			// REV-020: .success только для return на уровне тела функции,
			// не внутри вложенных блоков (if, for, while и т.д.)
			retIndent := computeLineIndent(lines[i])
			if retIndent != bodyIndent {
				continue
			}

			if !hasTSExistingSuccessOrFailed(lines, bodyStart, i) {
				successLine := retIndent + `trace("` + traceBase + `.success");` + "\n"
				*insertions = append(*insertions, insertion{float64(i) + 0.5, successLine})
			}
			continue
		}

		// .decision в критических if-блоках
		if strings.HasPrefix(stripped, "if ") || strings.HasPrefix(stripped, "if(") {
			if seenDecisionLines[i] {
				continue
			}
			seenDecisionLines[i] = true

			conditionText := getTSConditionText(stripped)
			if !isCriticalDecision(conditionText) {
				continue
			}

			// Найти открывающую скобку тела if
			braceLine := findOpeningBrace(lines, i)
			if braceLine < len(lines) {
				decName := decisionName(conditionText)
				decIndent := computeLineIndent(lines[i]) + "    "
				decisionLine := fmt.Sprintf(`%strace("%s.decision.%s", {"condition": %q});`+"\n",
					decIndent, traceBase, decName, conditionText)
				*insertions = append(*insertions, insertion{float64(braceLine) + 1.6, decisionLine})
			}
		}
	}
}

// getTSConditionText извлекает текст условия из if-строки TypeScript.
func getTSConditionText(stripped string) string {
	cond := stripped
	if strings.HasPrefix(cond, "if(") {
		cond = strings.TrimPrefix(cond, "if(")
	} else if strings.HasPrefix(cond, "if ") {
		cond = strings.TrimPrefix(cond, "if ")
	}
	// Убираем trailing ) { или ) {
	if idx := strings.LastIndex(cond, ")"); idx >= 0 {
		cond = cond[:idx+1]
	}
	// Убираем внешние скобки если это просто (condition)
	if strings.HasPrefix(cond, "(") && strings.HasSuffix(cond, ")") {
		cond = cond[1 : len(cond)-1]
	}
	return strings.TrimSpace(cond)
}

func findLastTSImport(lines []string) int {
	last := -1
	for i, line := range lines {
		stripped := strings.TrimSpace(line)
		if stripped == "" {
			continue
		}
		if strings.Contains(stripped, "import ") {
			last = i
			if strings.Contains(stripped, "(") && !strings.Contains(stripped, ")") {
				last = findClosingParen(lines, i)
			}
		}
	}
	return last
}

// =========================================================================
// Общие утилиты
// =========================================================================

// applyReplacements заменяет строки в слайсе на новые (REV-013).
func applyReplacements(lines []string, replacements []replacement) []string {
	// Сортируем по убыванию индекса — обрабатываем с конца, чтобы ранние индексы не сдвигались
	sort.Slice(replacements, func(i, j int) bool {
		return replacements[i].lineIdx > replacements[j].lineIdx
	})

	result := make([]string, len(lines))
	copy(result, lines)

	for _, r := range replacements {
		if r.lineIdx < 0 || r.lineIdx >= len(result) {
			continue
		}
		newResult := make([]string, 0, len(result)-1+len(r.lines))
		newResult = append(newResult, result[:r.lineIdx]...)
		newResult = append(newResult, r.lines...)
		newResult = append(newResult, result[r.lineIdx+1:]...)
		result = newResult
	}
	return result
}

func dedupAndSort(insertions []insertion) []insertion {
	seen := make(map[float64]bool)
	var result []insertion
	for _, ins := range insertions {
		if !seen[ins.lineIdx] {
			seen[ins.lineIdx] = true
			result = append(result, ins)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].lineIdx > result[j].lineIdx // обратный порядок
	})
	return result
}

func countInsertions(insertions []insertion) int {
	seen := make(map[float64]bool)
	for _, ins := range insertions {
		seen[ins.lineIdx] = true
	}
	return len(seen)
}

func applyInsertions(lines []string, insertions []insertion) []string {
	result := make([]string, 0, len(lines)+len(insertions))
	insMap := make(map[int][]insertion)
	for _, ins := range insertions {
		idx := int(math.Floor(ins.lineIdx))
		insMap[idx] = append(insMap[idx], ins)
	}

	// Сортируем вставки на одной строке по возрастанию дробной части
	for idx := range insMap {
		sort.Slice(insMap[idx], func(i, j int) bool {
			return insMap[idx][i].lineIdx < insMap[idx][j].lineIdx
		})
	}

	for i := 0; i < len(lines); i++ {
		// Сначала вставки перед строкой
		if ins, ok := insMap[i]; ok {
			for _, in := range ins {
				result = append(result, in.text)
			}
		}
		result = append(result, lines[i])
	}
	return result
}

// =========================================================================
// Запись и diff
// =========================================================================

func atomicWriteWithBackup(filePath string, original []byte, newContent string) error {
	// Бэкап
	backup := filePath + ".bak"
	if err := os.WriteFile(backup, original, 0644); err != nil {
		return fmt.Errorf("creating backup: %w", err)
	}

	// Атомарная запись
	dir := filepath.Dir(filePath)
	tmpFile, err := os.CreateTemp(dir, ".trace_inject_*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.WriteString(newContent); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("writing temp file: %w", err)
	}
	tmpFile.Close()

	if err := os.Rename(tmpPath, filePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("replacing file: %w", err)
	}

	return nil
}

func unifiedDiff(original, new, fileName string) string {
	origLines := strings.SplitAfter(original, "\n")
	newLines := strings.SplitAfter(new, "\n")

	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("--- %s (original)\n", fileName))
	buf.WriteString(fmt.Sprintf("+++ %s (injected)\n", fileName))

	// Простой LCS-based diff
	diffLines := computeUnifiedDiff(origLines, newLines)
	for _, dl := range diffLines {
		buf.WriteString(dl)
	}
	return buf.String()
}

func computeUnifiedDiff(a, b []string) []string {
	// LCS-based diff: строим таблицу, затем восстанавливаем изменения.
	// Без контекстных строк — только фактические изменения.
	lena, lenb := len(a), len(b)

	// Строим LCS таблицу
	lcs := make([][]int, lena+1)
	for i := range lcs {
		lcs[i] = make([]int, lenb+1)
	}
	for i := 1; i <= lena; i++ {
		for j := 1; j <= lenb; j++ {
			if a[i-1] == b[j-1] {
				lcs[i][j] = lcs[i-1][j-1] + 1
			} else {
				lcs[i][j] = max(lcs[i-1][j], lcs[i][j-1])
			}
		}
	}

	// Восстанавливаем diff с конца
	type diffOp struct {
		op   byte   // '-', '+', ' '
		text string
	}
	var ops []diffOp
	i, j := lena, lenb
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && a[i-1] == b[j-1] {
			ops = append(ops, diffOp{' ', a[i-1]})
			i--
			j--
		} else if j > 0 && (i == 0 || lcs[i][j-1] >= lcs[i-1][j]) {
			ops = append(ops, diffOp{'+', b[j-1]})
			j--
		} else {
			ops = append(ops, diffOp{'-', a[i-1]})
			i--
		}
	}

	// Разворачиваем
	var result []string
	for k := len(ops) - 1; k >= 0; k-- {
		op := ops[k]
		text := strings.TrimSuffix(op.text, "\n")
		switch op.op {
		case ' ':
			result = append(result, " "+text+"\n")
		case '-':
			result = append(result, "-"+text+"\n")
		case '+':
			result = append(result, "+"+text+"\n")
		}
	}
	return result
}
