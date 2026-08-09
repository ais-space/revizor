// Пакет trace — матчинг и санитизация для подсистемы Ревизор.
package trace

import (
	"regexp"
	"strings"
)

const MaxDepth = 5
const Mask = "***"

// sensitiveFields — ключи, маскируемые Слоем 1 (case-insensitive).
var sensitiveFields = map[string]struct{}{
	"token": {}, "password": {}, "secret": {}, "api_key": {},
	"access_token": {}, "refresh_token": {}, "authorization": {},
	"cookie": {}, "set_cookie": {}, "credential": {},
	"private_key": {}, "client_secret": {}, "session_key": {},
	"csrf_token": {}, "jwt": {}, "bearer": {},
	"simca_session": {}, "simca_admin_session": {}, "auth_token": {},
}

// sensitivePrefixes — префиксы ключей для маскировки.
var sensitivePrefixes = []string{"pii_", "violator_"}

// sensitiveRegexps — скомпилированные regex-паттерны для Слоя 2.
var sensitiveRegexps = []*regexp.Regexp{
	regexp.MustCompile(`eyJ[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+`),
	regexp.MustCompile(`simca_session_[a-zA-Z0-9_-]+`),
	regexp.MustCompile(`simca_admin_session_[a-zA-Z0-9_-]+`),
	regexp.MustCompile(`\b[\w.+-]+@[\w-]+\.[\w.]+\b`),
	regexp.MustCompile(`[A-Za-z0-9+/=]{40,}`),
	regexp.MustCompile(`(?:sk|pk|key|api)-[a-zA-Z0-9]{20,}`),
}

// DeepSanitize — публичная точка входа для двухслойной санитизации данных.
func DeepSanitize(obj any) any {
	return deepSanitize(obj, 0)
}

func deepSanitize(obj any, depth int) any {
	if depth >= MaxDepth {
		return truncate(obj)
	}

	switch v := obj.(type) {
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, val := range v {
			if isSensitiveKey(key) {
				result[key] = Mask
			} else {
				result[key] = deepSanitize(val, depth+1)
			}
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, val := range v {
			result[i] = deepSanitize(val, depth+1)
		}
		return result
	case string:
		return sanitizeString(v)
	default:
		return obj
	}
}

func isSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	if _, ok := sensitiveFields[lower]; ok {
		return true
	}
	for _, prefix := range sensitivePrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func sanitizeString(value string) string {
	for _, re := range sensitiveRegexps {
		value = re.ReplaceAllStringFunc(value, maskMatch)
	}
	return value
}

func maskMatch(matched string) string {
	if len(matched) > 8 {
		return matched[:4] + Mask + matched[len(matched)-4:]
	}
	return matched[:min(4, len(matched))] + Mask
}

func truncate(obj any) any {
	switch v := obj.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		if len(keys) > 10 {
			keys = keys[:10]
		}
		return map[string]any{"_truncated": true, "keys": keys}
	case []any:
		return map[string]any{"_truncated": true, "length": len(v)}
	case string:
		if len(v) > 100 {
			return v[:100] + "..."
		}
		return v
	default:
		return obj
	}
}
