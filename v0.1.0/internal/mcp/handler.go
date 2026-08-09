package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/license"
	"github.com/ais-platform/ais_products/revizor/internal/mcp/tools"
	"github.com/ais-platform/ais_products/revizor/internal/store"
)

// MCPHandler — диспетчер JSON-RPC сообщений для MCP-инструментов.
type MCPHandler struct {
	tools         map[string]ToolHandler
	store         store.TraceStore
	cfg           *config.Config
	license       *license.License
	sundayChecker *SundayChecker // v56: кэшируемая проверка воскресного безлимита
	tailCursorsMu sync.Mutex
	tailCursors   map[string]int64 // session_id -> last_id для trace_tail
	debugLog      atomic.Bool     // режим отладки: запись запросов/ответов в файл
	debugLogMu    sync.Mutex      // защита файла от concurrent write
}

// SetDebugLog включает/выключает отладочную запись MCP-запросов/ответов в файл.
func (h *MCPHandler) SetDebugLog(on bool) {
	h.debugLog.Store(on)
}

// IsDebugLog возвращает текущее состояние отладочного режима.
func (h *MCPHandler) IsDebugLog() bool {
	return h.debugLog.Load()
}

// NewMCPHandler создаёт диспетчер и регистрирует все инструменты.
// shutdownFn — опциональная функция graceful остановки (nil — в stdio-only режиме).
func NewMCPHandler(st store.TraceStore, cfg *config.Config, lic *license.License, shutdownFn tools.ShutdownFunc) *MCPHandler {
	h := &MCPHandler{
		tools:         make(map[string]ToolHandler),
		store:         st,
		cfg:           cfg,
		license:       lic,
		sundayChecker: NewSundayChecker(cfg.Server.LicenseServerURL, lic),
		tailCursors:   make(map[string]int64),
	}

	// Устанавливаем shutdown-функцию ДО регистрации
	if shutdownFn != nil {
		allTools := tools.All()
		tools.SetShutdownFunc(allTools, shutdownFn)
	}

	h.registerTools()
	return h
}

// auditLog записывает вызов инструмента в audit_log БД.
func (h *MCPHandler) auditLog(toolName string, args json.RawMessage, result string, rpcErr string, duration time.Duration) {
	// Обрезаем результат до 500 символов
	shortResult := result
	if len(shortResult) > 500 {
		shortResult = shortResult[:500] + "..."
	}

	entry := store.AuditEntry{
		ToolName:   toolName,
		Args:       string(args),
		Result:     shortResult,
		Error:      rpcErr,
		DurationMs: duration.Milliseconds(),
	}

	// Пишем асинхронно — ошибка аудита не должна ронять инструмент
	_ = h.store.WriteAudit(entry)
}

// License возвращает текущую лицензию (может быть nil для Community-режима).
func (h *MCPHandler) License() *license.License {
	return h.license
}

// HandleJSONRPC обрабатывает одно JSON-RPC сообщение и возвращает ответ.
// Никогда не паникует — все ошибки возвращаются как JSON-RPC Error.
func (h *MCPHandler) HandleJSONRPC(ctx context.Context, body []byte) []byte {
	// Разбор запроса
	var req JSONRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		resp := mustMarshal(JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      nil,
			Error:   &JSONRPCError{Code: -32700, Message: "JSON parse error — the request body is not valid JSON.", Data: err.Error()},
		})
		h.writeDebugLog(body, resp)
		return resp
	}

	// Уведомления (без id) не требуют ответа
	if req.ID == nil {
		return nil
	}

	// Диспетчеризация с recover-защитой
	result, rpcErr := h.safeDispatch(ctx, req)

	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
		Error:   rpcErr,
	}
	responseBytes := mustMarshal(resp)
	h.writeDebugLog(body, responseBytes)
	return responseBytes
}

// writeDebugLog записывает запрос и ответ в файл лога если режим отладки включён.
func (h *MCPHandler) writeDebugLog(request, response []byte) {
	if !h.debugLog.Load() {
		return
	}
	h.debugLogMu.Lock()
	defer h.debugLogMu.Unlock()

	logDir := "logs"
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return
	}
	logPath := filepath.Join(logDir, "revizor_mcp_debug.log")

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	now := time.Now().UTC().Format(time.RFC3339Nano)

	// Компактная запись: строка с таймстампом, затем запрос, затем ответ
	fmt.Fprintf(f, "=== %s ===\n", now)
	fmt.Fprintf(f, ">> REQUEST (%d bytes):\n%s\n", len(request), compactJSON(request))
	fmt.Fprintf(f, "<< RESPONSE (%d bytes):\n%s\n\n", len(response), compactJSON(response))
}

// compactJSON сжимает JSON в одну строку если он меньше 4KB, иначе обрезает.
func compactJSON(data []byte) string {
	if len(data) <= 4096 {
		return string(data)
	}
	return string(data[:4096]) + fmt.Sprintf("\n... (truncated, total %d bytes)", len(data))
}

// dispatch маршрутизирует запрос к нужному обработчику.
func (h *MCPHandler) dispatch(ctx context.Context, req JSONRPCRequest) (any, *JSONRPCError) {
	switch req.Method {
	case "initialize":
		return h.handleInitialize(), nil
	case "tools/list":
		return h.handleToolsList(), nil
	case "tools/call":
		return h.handleToolsCall(ctx, req.Params)
	default:
		return nil, &JSONRPCError{Code: -32601, Message: fmt.Sprintf("Method not found: %s. Use tools/list to see available methods.", req.Method)}
	}
}

// handleInitialize обрабатывает MCP initialize — стандартный handshake.
func (h *MCPHandler) handleInitialize() any {
	return map[string]any{
		"protocolVersion": "2024-11-05",
		"serverInfo": map[string]string{
			"name":    "revizor",
			"version": tools.Version,
		},
		"capabilities": map[string]any{
			"tools": map[string]bool{},
		},
	}
}

// safeDispatch — обёртка dispatch с recover.
func (h *MCPHandler) safeDispatch(ctx context.Context, req JSONRPCRequest) (result any, rpcErr *JSONRPCError) {
	defer func() {
		if r := recover(); r != nil {
			result = nil
			rpcErr = &JSONRPCError{Code: -32603, Message: "Internal error — the tool panicked. Check server logs for details.", Data: fmt.Sprintf("panic: %v", r)}
		}
	}()
	return h.dispatch(ctx, req)
}

// handleToolsList возвращает список всех зарегистрированных инструментов.
func (h *MCPHandler) handleToolsList() any {
	toolList := make([]MCPTool, 0, len(h.tools))
	for _, t := range h.tools {
		toolList = append(toolList, t.Schema())
	}
	return map[string]any{"tools": toolList}
}

// handleToolsCall вызывает конкретный инструмент.
func (h *MCPHandler) handleToolsCall(ctx context.Context, params json.RawMessage) (any, *JSONRPCError) {
	var tp ToolParams
	if err := json.Unmarshal(params, &tp); err != nil {
		return nil, &JSONRPCError{Code: -32602, Message: "Invalid params", Data: err.Error()}
	}

	tool, ok := h.tools[tp.Name]
	if !ok {
		return nil, &JSONRPCError{
			Code:    -32602,
			Message: fmt.Sprintf("Unknown tool: %s", tp.Name),
			Data:    "Use tools/list to see available tools",
		}
	}

	// Execute с recover-защитой + аудит
	startTime := time.Now()
	text, execErr := h.safeExecute(ctx, tool, tp.Arguments)

	var errStr string
	if execErr != nil {
		errStr = execErr.Error()
	}
	h.auditLog(tp.Name, tp.Arguments, text, errStr, time.Since(startTime))

	if execErr != nil {
		return newErrorResult(fmt.Sprintf("Error: %s", errStr)), nil
	}

	return newToolResult(text), nil
}

// safeExecute вызывает Execute инструмента с recover-защитой.
// v56 (ALERT-002): перед выполнением проверяет воскресный безлимит.
func (h *MCPHandler) safeExecute(ctx context.Context, tool ToolHandler, args json.RawMessage) (text string, err error) {
	defer func() {
		if r := recover(); r != nil {
			text = ""
			err = fmt.Errorf("tool %s panicked: %v", tool.Name(), r)
		}
	}()

	// v56: проверка воскресного безлимита перед выполнением инструмента
	if h.sundayChecker != nil && h.license != nil {
		isSunday := h.sundayChecker.IsSunday(ctx)
		// Обновляем лимиты в сторе с учётом воскресного безлимита
		effLim := license.EffectiveLimits(h.license, isSunday)
		h.store.SetEffectiveLimits(effLim)
	}

	return tool.Execute(ctx, args, h.store, h.cfg)
}

// HandleJSONRPCSafe — публичная обёртка с recover на верхнем уровне.
func (h *MCPHandler) HandleJSONRPCSafe(ctx context.Context, body []byte) []byte {
	defer func() {
		if r := recover(); r != nil {
			// Последний рубеж — если даже сам диспетчер упал
		}
	}()
	return h.HandleJSONRPC(ctx, body)
}

// TailCursor возвращает last_id для сессии (для trace_tail).
func (h *MCPHandler) TailCursor(sessionID string) int64 {
	h.tailCursorsMu.Lock()
	defer h.tailCursorsMu.Unlock()
	return h.tailCursors[sessionID]
}

// SetShutdownFunc обновляет shutdown-функцию для инструмента trace_shutdown.
// Вызывается после создания HTTP-сервера (сервер нужен для shutdown).
func (h *MCPHandler) SetShutdownFunc(fn tools.ShutdownFunc) {
	// Ищем зарегистрированный адаптер для trace_shutdown и обновляем shutdown на его tool.
	for name, adapter := range h.tools {
		if name == "trace_shutdown" {
			if sht, ok := adapter.(*toolAdapter).t.(*tools.TraceShutdownTool); ok {
				sht.SetShutdown(fn)
			}
			return
		}
	}
}

// SetTailCursor сохраняет last_id для сессии.
func (h *MCPHandler) SetTailCursor(sessionID string, lastID int64) {
	h.tailCursorsMu.Lock()
	defer h.tailCursorsMu.Unlock()
	h.tailCursors[sessionID] = lastID
}

// --- Вспомогательные ---

func mustMarshal(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		// Фолбэк — простая JSON-ошибка
		return []byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"Internal error","data":"marshal failed"}}`)
	}
	return data
}
