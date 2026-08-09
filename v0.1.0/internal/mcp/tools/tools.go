// Пакет tools содержит реализации MCP-инструментов Ревизора.
// Актуальное количество: см. All() и revizor-go/DEV_README.md.
//
// Использует локальные типы для избежания циклического импорта с пакетом mcp.
// Внешний пакет (mcp) конвертирует эти типы в свои через структурный typing Go.
package tools

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/license"
	"github.com/ais-platform/ais_products/revizor/internal/store"
)

// --- Локальные типы (зеркало mcp.MCPTool, mcp.JSONSchema) ---

// ToolSchema — описание инструмента для tools/list.
type ToolSchema struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	InputSchema JSONSchema `json:"inputSchema"`
}

// JSONSchema — JSON Schema для аргументов инструмента.
type JSONSchema struct {
	Type       string              `json:"type"`
	Properties map[string]PropSpec `json:"properties"`
	Required   []string            `json:"required,omitempty"`
}

// PropSpec — свойство в JSON Schema.
type PropSpec struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

// Tool — интерфейс, который реализует каждый MCP-инструмент.
// Идентичен mcp.ToolHandler по структуре, что позволяет Go использовать структурный typing.
type Tool interface {
	Name() string
	Schema() ToolSchema
	Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error)
}

// stringProp — хелпер для создания PropSpec типа string.
func stringProp(desc string) PropSpec {
	return PropSpec{Type: "string", Description: desc}
}

// intProp — хелпер для создания PropSpec типа integer.
func intProp(desc string) PropSpec {
	return PropSpec{Type: "integer", Description: desc}
}

// LicenseSetter — интерфейс для инструментов, которым нужна лицензия.
type LicenseSetter interface {
	SetLicense(lic *license.License)
}

// LastHeartbeatSetter — интерфейс для инструментов, которым нужно знать время последнего heartbeat.
type LastHeartbeatSetter interface {
	SetLastHeartbeat(t *time.Time)
}

// parseArgs парсит args в структуру v. Если args пуст — v остаётся с zero-значениями.
func parseArgs(args json.RawMessage, v any) error {
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	return json.Unmarshal(args, v)
}

// All возвращает все инструменты. Вызывается из mcp.NewMCPHandler.
// Актуальное количество: см. revizor-go/DEV_README.md (эталонный список).
func All() []Tool {
	return []Tool{
		// Группа 1: Управление сессией
		&TraceStartTool{},
		&TraceEnableTool{},
		&TraceDisableTool{},
		&TraceExpireTool{},
		&TraceKillTool{},

		// Группа 2: Чтение логов
		&TraceReadTool{},
		&TraceSearchTool{},
		&TraceConfigTool{},
		&TraceTailTool{},

		// Группа 3: Статистика и справочная
		&TraceValidatePathTool{},
		&TraceWhyTool{},
		&TraceListSessionsTool{},
		&TracePresetsListTool{},
		&TracePresetSetTool{},
		&TraceStatsTool{},

		// Группа 4: Анализ
		&TraceListPointsTool{},
		&TraceVerifyCoverageTool{},
		&TraceDiffTool{},

		// Группа 5: Генерация
		&TraceCostTool{},
		&TraceGenerateTestTool{},
		&TraceSuggestCoverageTool{},

		// Группа 6: Ревизоризация кода
		&TraceInjectTool{},
		&TraceRemoveTool{},
		&TraceTargetsTool{},
		&TraceAuditTool{},

		// Группа 0: Диагностика соединения и лицензия
		&TracePingTool{},
		&TraceLicenseRenewTool{},
		&TraceEchoTool{},

		// Группа 7: Оркестраторы
		&TraceSessionSummaryTool{},
		&TraceOrchestratorEventsTool{},

		// Группа 8: Управление процессом
		&TraceShutdownTool{},

		// Группа 9: Отладка (скрытые инструменты)
		&TraceDebugLogTool{},

		// Группа 10: Webhook-уведомления (REV-W-001)
		&TraceWebhookListTool{},
		&TraceWebhookTestTool{},

		// Группа 11: Обновления (v55, Фаза 6)
		&TraceUpdateCheckTool{},

		// Группа 12: Встроенная инструкция AI-агента (вкомпилирована в бинарник)
		&TraceGetInstructionsTool{},
	}
}

// SetShutdownFunc устанавливает функцию остановки для TraceShutdownTool.
// Вызывается из mcp.NewMCPHandler после создания всех инструментов.
func SetShutdownFunc(tools []Tool, fn ShutdownFunc) {
	for _, t := range tools {
		if sht, ok := t.(*TraceShutdownTool); ok {
			sht.SetShutdown(fn)
			return
		}
	}
}
