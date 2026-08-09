// Пакет mcp — встроенный MCP-сервер (Model Context Protocol) для Ревизора.
// Реализует JSON-RPC 2.0 транспорт для инструментов управления трассировкой.
// Актуальное количество инструментов: см. tools.All() и ais_products/revizor/DEV_README.md.
package mcp

import (
	"context"
	"encoding/json"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/store"
)

// ToolHandler — интерфейс, который реализует каждый MCP-инструмент.
type ToolHandler interface {
	Name() string
	Schema() MCPTool
	Execute(ctx context.Context, args json.RawMessage, st store.TraceStore, cfg *config.Config) (string, error)
}

// --- JSON-RPC 2.0 типы ---

// JSONRPCRequest — входящий JSON-RPC 2.0 запрос.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id"` // nil = уведомление (ответ не требуется)
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse — исходящий JSON-RPC 2.0 ответ.
type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      *int          `json:"id"`
	Result  any           `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

// JSONRPCError — ошибка JSON-RPC.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data,omitempty"`
}

// --- MCP-специфичные типы ---

// ToolParams — параметры вызова tools/call.
type ToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// MCPToolResult — результат tools/call (спецификация MCP).
type MCPToolResult struct {
	Content []MCPContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

// MCPContent — элемент контента в результате.
type MCPContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// MCPTool — описание инструмента для tools/list.
type MCPTool struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	InputSchema JSONSchema `json:"inputSchema"`
}

// JSONSchema — JSON Schema для аргументов инструмента.
type JSONSchema struct {
	Type       string                    `json:"type"`
	Properties map[string]JSONSchemaProp `json:"properties"`
	Required   []string                  `json:"required,omitempty"`
}

// JSONSchemaProp — свойство в JSON Schema.
type JSONSchemaProp struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

// --- Вспомогательные функции ---

// newToolResult создаёт MCPToolResult с одним текстовым блоком.
func newToolResult(text string) MCPToolResult {
	return MCPToolResult{
		Content: []MCPContent{{Type: "text", Text: text}},
	}
}

// newErrorResult создаёт MCPToolResult с флагом isError.
func newErrorResult(text string) MCPToolResult {
	return MCPToolResult{
		Content: []MCPContent{{Type: "text", Text: text}},
		IsError: true,
	}
}

// newJSONRPCError создаёт стандартные ошибки JSON-RPC.
func newJSONRPCError(code int, message, data string) JSONRPCError {
	return JSONRPCError{Code: code, Message: message, Data: data}
}
