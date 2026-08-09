package trace

import (
	"regexp"
	"strings"
	"sync"
)

// Максимальная длина trace-пути
const MaxPathLength = 120

// pathPattern — regex для валидации trace-пути.
var pathPattern = regexp.MustCompile(`^[a-z0-9_.*]+$`)

// pascalToSnakeRe — regex для конвертации PascalCase/camelCase → snake_case.
var pascalToSnakeRe1 = regexp.MustCompile(`([a-z0-9])([A-Z])`)
var pascalToSnakeRe2 = regexp.MustCompile(`([A-Z]+)([A-Z][a-z])`)

// pascalToSnake конвертирует PascalCase/camelCase в snake_case.
func PascalToSnake(s string) string {
	// Если начинается с заглавной — добавляем префикс для корректной обработки
	if len(s) > 0 && s[0] >= 'A' && s[0] <= 'Z' {
		s = "_" + s
	}
	result := pascalToSnakeRe1.ReplaceAllString(s, "${1}_${2}")
	result = pascalToSnakeRe2.ReplaceAllString(result, "${1}_${2}")
	result = strings.TrimPrefix(result, "_")
	return strings.ToLower(result)
}

// NormalizePath приводит trace-путь к snake_case, заменяя PascalCase-сегменты.
// Если путь уже валидный — возвращает без изменений.
// Если содержит заглавные буквы — каждый сегмент прогоняется через PascalToSnake.
func NormalizePath(path string) string {
	if ValidatePath(path) {
		return path
	}
	parts := strings.Split(path, ".")
	for i, p := range parts {
		// Проверяем, содержит ли сегмент заглавные буквы
		hasUpper := false
		for _, r := range p {
			if r >= 'A' && r <= 'Z' {
				hasUpper = true
				break
			}
		}
		if hasUpper {
			parts[i] = PascalToSnake(p)
		}
	}
	return strings.Join(parts, ".")
}

// Кэш скомпилированных конфигов.
var (
	mu                sync.RWMutex
	compiledConfigs   = make(map[string]map[string]bool) // {cacheKey: {path: enabled}}
	compiledExcludes  = make(map[string]map[string]bool) // {cacheKey: {excludedPath: true}}
)

// ConfigRow — строка конфига для матчера.
type ConfigRow struct {
	TracePath string
	Enabled   bool
	Owner     *string
}

func getCacheKey(sessionID string) string {
	if sessionID == "" {
		return "__global__"
	}
	return sessionID
}

// ValidatePath проверяет валидность формата trace-пути.
func ValidatePath(path string) bool {
	if path == "" {
		return false
	}
	if len(path) > MaxPathLength {
		return false
	}
	return pathPattern.MatchString(path)
}

// CompileConfig компилирует плоский словарь trace-путей из слайса строк конфига.
func CompileConfig(rows []ConfigRow, sessionID *string) (map[string]bool, error) {
	var cacheKey string
	if sessionID != nil {
		cacheKey = getCacheKey(*sessionID)
	} else {
		cacheKey = getCacheKey("")
	}

	includes := make(map[string]bool)
	excludes := make(map[string]bool)

	for _, row := range rows {
		if strings.HasPrefix(row.TracePath, "!") {
			excludes[row.TracePath[1:]] = true
		} else if row.Enabled {
			includes[row.TracePath] = true
		}
	}

	mu.Lock()
	compiledConfigs[cacheKey] = includes
	compiledExcludes[cacheKey] = excludes
	mu.Unlock()

	return includes, nil
}

// ShouldTrace проверяет, включена ли trace-точка.
// Приоритет: exclude > exact match > glob match > miss (false).
// Проверяет как сессионный, так и глобальный кэш.
func ShouldTrace(path string, sessionID string) bool {
	cacheKey := getCacheKey(sessionID)

	mu.RLock()
	config, configOk := compiledConfigs[cacheKey]
	excludes, _ := compiledExcludes[cacheKey]
	globalConfig, globalOk := compiledConfigs["__global__"]
	globalExcludes := compiledExcludes["__global__"]
	mu.RUnlock()

	// Объединяем сессионные и глобальные конфиги
	if !configOk && !globalOk {
		return false
	}
	if !configOk {
		config = make(map[string]bool)
	}

	// 1. Проверить excludes (сначала сессионные, потом глобальные)
	if matchExcludes(path, excludes) || matchExcludes(path, globalExcludes) {
		return false
	}

	// 2. Exact match (сессионный приоритетнее глобального)
	if enabled, ok := config[path]; ok {
		return enabled
	}
	if enabled, ok := globalConfig[path]; ok {
		return enabled
	}

	// 3. Glob-матчинг — первый match возвращает enabled
	// Сначала сессионные паттерны
	for pattern, enabled := range config {
		if globMatch(path, pattern) {
			return enabled
		}
	}
	// Затем глобальные
	for pattern, enabled := range globalConfig {
		if globMatch(path, pattern) {
			return enabled
		}
	}

	// 4. Miss
	return false
}

func matchExcludes(path string, excludes map[string]bool) bool {
	for exPath := range excludes {
		if path == exPath || globMatch(path, exPath) {
			return true
		}
	}
	return false
}

// GlobMatch проверяет соответствие пути glob-паттерну.
// `**` — greedy (всегда true если встретился), `*` — ровно один сегмент.
func GlobMatch(path, pattern string) bool {
	return globMatch(path, pattern)
}

func globMatch(path, pattern string) bool {
	if pattern == path {
		return true
	}

	pathParts := strings.Split(path, ".")
	patternParts := strings.Split(pattern, ".")

	pi, pp := 0, 0
	for pi < len(pathParts) && pp < len(patternParts) {
		if patternParts[pp] == "**" {
			pp++
			if pp >= len(patternParts) {
				return true // ** в конце = всё что угодно дальше
			}
			// ** в середине: ищем следующий паттерн в оставшихся частях пути
			for pi < len(pathParts) {
				if globMatch(strings.Join(pathParts[pi:], "."), strings.Join(patternParts[pp:], ".")) {
					return true
				}
				pi++
			}
			return false
		}
		if patternParts[pp] == "*" {
			pi++
			pp++
		} else if patternParts[pp] == pathParts[pi] {
			pi++
			pp++
		} else {
			return false
		}
	}

	return pi == len(pathParts) && pp == len(patternParts)
}

// InvalidateCache сбрасывает кэш. Если sessionID пуст — сбрасывает весь кэш.
func InvalidateCache(sessionID string) {
	mu.Lock()
	defer mu.Unlock()

	if sessionID == "" {
		compiledConfigs = make(map[string]map[string]bool)
		compiledExcludes = make(map[string]map[string]bool)
	} else {
		cacheKey := getCacheKey(sessionID)
		delete(compiledConfigs, cacheKey)
		delete(compiledExcludes, cacheKey)
	}
}

// SetCacheForTest — ТОЛЬКО ДЛЯ ТЕСТОВ: устанавливает кэш напрямую.
func SetCacheForTest(sessionID string, config map[string]bool, excludes map[string]bool) {
	mu.Lock()
	defer mu.Unlock()
	cacheKey := getCacheKey(sessionID)
	compiledConfigs[cacheKey] = config
	if excludes != nil {
		compiledExcludes[cacheKey] = excludes
	}
}

// ResetCache — ТОЛЬКО ДЛЯ ТЕСТОВ: полная очистка кэша.
func ResetCache() {
	mu.Lock()
	defer mu.Unlock()
	compiledConfigs = make(map[string]map[string]bool)
	compiledExcludes = make(map[string]map[string]bool)
}
