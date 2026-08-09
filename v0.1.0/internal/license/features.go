package license

// Маппинг feature-флагов на группы MCP-инструментов.
//
// Базовый набор (mcp_basic):
//   - trace_start, trace_read, trace_expire
//
// mcp_full — все инструменты.
//
// Специфичные фичи:
//   - inject_apply — trace_inject с dry_run=false
//   - postgres — PostgreSQL-хранилище
//   - sse — SSE / live config
//   - analytics — trace_diff, trace_generate_test, trace_cost

// IsMCPBasic проверяет что инструмент доступен в Community-режиме.
func IsMCPBasic(toolName string) bool {
	switch toolName {
	case "trace_start", "trace_read", "trace_expire":
		return true
	default:
		return false
	}
}

// AllowTool проверяет доступность MCP-инструмента с учётом лицензии.
func AllowTool(lic *License, toolName string) bool {
	// Community-режим: только базовые инструменты
	if lic == nil {
		return IsMCPBasic(toolName)
	}

	// mcp_full: все инструменты
	if hasFeatureInList(lic, "mcp_full") {
		return true
	}

	// mcp: основные инструменты (без inject_apply, analytics)
	if hasFeatureInList(lic, "mcp") && IsMCPBasic(toolName) {
		return true
	}

	// inject_apply: нужна отдельная фича
	if toolName == "trace_inject" && hasFeatureInList(lic, "inject_apply") {
		return true
	}

	// analytics: trace_diff, trace_generate_test, trace_cost
	if isAnalyticsTool(toolName) && hasFeatureInList(lic, "analytics") {
		return true
	}

	return false
}

// isAnalyticsTool возвращает true для аналитических инструментов.
func isAnalyticsTool(name string) bool {
	switch name {
	case "trace_diff", "trace_generate_test", "trace_cost", "trace_verify_coverage", "trace_suggest_coverage":
		return true
	default:
		return false
	}
}
