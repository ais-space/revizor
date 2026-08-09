package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/mcp"
	"github.com/ais-platform/ais_products/revizor/internal/store"
	"github.com/ais-platform/ais_products/revizor/internal/trace"
	"github.com/ais-platform/ais_products/revizor/internal/webhook"
)

// Константы батчинга.
const (
	batchSize    = 50
	flushInterval = 100 * time.Millisecond
)

// Server — HTTP-сервер Ревизора.
type Server struct {
	cfg        *config.Config
	store      store.TraceStore
	logger     *slog.Logger
	httpSrv    *http.Server
	rl         *RateLimiter
	mcpHandler *mcp.MCPHandler

	// Graceful shutdown trigger
	shutdownCh chan struct{}

	// Webhook-диспетчер (REV-W-001)
	webhookDispatcher *webhook.Dispatcher

	// Батчинг
	bufMu   sync.Mutex
	buffer  []store.TraceEntry
	stopCh  chan struct{}
	stopped bool
}

// ShutdownTrigger возвращает канал, который закрывается при запросе trace_shutdown.
// main() слушает этот канал для graceful остановки.
func (s *Server) ShutdownTrigger() <-chan struct{} {
	return s.shutdownCh
}

// InitiateShutdown запускает graceful остановку через HTTP-сервер.
// Вызывается из MCP-инструмента trace_shutdown или HTTP-эндпоинта.
func (s *Server) InitiateShutdown() error {
	s.logger.Info("trace_shutdown: initiating graceful shutdown...")
	// Закрываем канал — main() получит сигнал и вызовет srv.Shutdown()
	select {
	case <-s.shutdownCh:
		// Уже закрыт
	default:
		close(s.shutdownCh)
	}
	return nil
}

// NewServer создаёт новый HTTP-сервер.
func NewServer(cfg *config.Config, st store.TraceStore, logger *slog.Logger, mcpHandler *mcp.MCPHandler) *Server {
	// Webhook dispatcher (REV-W-001)
	whDispatcher := webhook.NewDispatcher(cfg.Integrations.Webhooks, logger)
	mcpHandler.SetWebhookDispatcher(whDispatcher)

	s := &Server{
		cfg:               cfg,
		store:             st,
		logger:            logger,
		rl:                NewRateLimiter(),
		mcpHandler:        mcpHandler,
		webhookDispatcher: whDispatcher,
		shutdownCh:        make(chan struct{}),
		stopCh:            make(chan struct{}),
	}

	s.rl.SetLimit("log", cfg.Security.RateLimit.LogPerSec)
	s.rl.SetLimit("config", cfg.Security.RateLimit.ConfigPerSec)

	// Запуск batch-воркера
	go s.batchWorker()

	return s
}

// Handler возвращает HTTP-обработчик для использования с httptest.
func (s *Server) Handler() http.Handler {
	return corsMiddleware(s.buildMux())
}

// ListenAndServe запускает HTTP-сервер.
func (s *Server) ListenAndServe(addr string) error {
	s.httpSrv = &http.Server{
		Addr:         addr,
		Handler:      corsMiddleware(s.buildMux()),
		ReadTimeout:  time.Duration(s.cfg.Server.ReadTimeoutSec) * time.Second,
		WriteTimeout: time.Duration(s.cfg.Server.WriteTimeoutSec) * time.Second,
	}
	return s.httpSrv.ListenAndServe()
}

// Shutdown gracefully останавливает сервер.
func (s *Server) Shutdown(ctx context.Context) error {
	// Финальный flush буфера
	s.flushBuffer()

	// Остановка batch-воркера
	if !s.stopped {
		s.stopped = true
		close(s.stopCh)
	}

	if s.httpSrv != nil {
		return s.httpSrv.Shutdown(ctx)
	}
	return nil
}

func (s *Server) buildMux() *http.ServeMux {
	mux := http.NewServeMux()

	// Открытые эндпоинты
	mux.HandleFunc("POST /api/v1/trace/log", requestLogger(s.logger,
		rateLimitMiddleware(s.rl, "log", s.logger,
			s.handleTraceLog)))
	mux.HandleFunc("POST /api/v1/trace/log/batch", requestLogger(s.logger,
		rateLimitMiddleware(s.rl, "log", s.logger,
			s.handleTraceLogBatch)))

	// Защищённые эндпоинты
	protected := func(h http.HandlerFunc) http.HandlerFunc {
		return requestLogger(s.logger,
			rateLimitMiddleware(s.rl, "config", s.logger,
				apiKeyMiddleware(s.cfg.Security.APIKey, h)))
	}

	mux.HandleFunc("GET /api/v1/trace/log", protected(s.handleTraceLogRead))
	mux.HandleFunc("POST /api/v1/trace/config", protected(s.handleConfigSet))
	mux.HandleFunc("GET /api/v1/trace/config", protected(s.handleConfigGet))
	mux.HandleFunc("DELETE /api/v1/trace/config", protected(s.handleConfigDelete))
	mux.HandleFunc("POST /api/v1/trace/session", protected(s.handleSessionCreate))
	mux.HandleFunc("GET /api/v1/trace/sessions", protected(s.handleSessionsList))
	mux.HandleFunc("GET /api/v1/trace/stats", protected(s.handleStats))

	// MCP-эндпоинт
	mux.HandleFunc("POST /mcp", protected(s.handleMCP))

	// Graceful shutdown (защищённый)
	mux.HandleFunc("POST /api/v1/trace/shutdown", protected(s.handleShutdown))

	return mux
}

// corsMiddleware добавляет CORS-заголовки для браузерных запросов с localhost:3000/3001.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "http://localhost:3000" || origin == "http://localhost:3001" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- Обработчики ---

// POST /api/v1/trace/log
func (s *Server) handleTraceLog(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path      string `json:"path"`
		Data      any    `json:"data"`
		SessionID string `json:"session_id"`
		RequestID string `json:"request_id"`
	}

	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"status": "error",
			"detail": "Невалидный JSON",
		})
		return
	}

	if !trace.ValidatePath(req.Path) {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"status": "error",
			"detail": fmt.Sprintf("Невалидный trace-путь: %s", req.Path),
		})
		return
	}

	if req.SessionID != "" && !trace.ShouldTrace(req.Path, req.SessionID) {
		writeJSON(w, http.StatusOK, map[string]string{
			"status": "skipped",
			"path":   req.Path,
			"reason": "disabled",
		})
		return
	}

	// Санитизация
	data := trace.DeepSanitize(req.Data)

	entry := store.TraceEntry{
		TracePath: req.Path,
		Data:      data,
		RequestID: strPtr(req.RequestID),
	}

	if req.SessionID != "" {
		entry.SessionID = &req.SessionID
	}

	// Self-trace идёт напрямую, минуя буфер
	if strings.HasPrefix(req.Path, "revizor.") {
		if err := s.store.WriteTrace(entry); err != nil {
			s.logger.Error("write trace failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"status": "error",
				"detail": "Ошибка записи",
			})
			return
		}
		// Self-trace тоже диспатчим (webhook) — после записи
		s.webhookDispatcher.Dispatch(entry)
	} else {
		s.enqueue(entry)
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
		"path":   req.Path,
	})
}

// POST /api/v1/trace/log/batch
func (s *Server) handleTraceLogBatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Events []struct {
			Path      string `json:"path"`
			Data      any    `json:"data"`
			SessionID string `json:"session_id"`
			RequestID string `json:"request_id"`
		} `json:"events"`
	}

	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"status": "error",
			"detail": "Невалидный JSON",
		})
		return
	}

	if len(req.Events) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":   "ok",
			"accepted": 0,
			"skipped":  0,
			"rejected": 0,
		})
		return
	}

	var accepted, skipped, rejected int
	entries := make([]store.TraceEntry, 0, len(req.Events))

	for i := range req.Events {
		ev := &req.Events[i]
		// Нормализация PascalCase → snake_case (REV-014)
		if normalized := trace.NormalizePath(ev.Path); normalized != ev.Path {
			s.logger.Warn("trace path normalized: PascalCase → snake_case", "original", ev.Path, "normalized", normalized)
			ev.Path = normalized
		}

		// Валидация пути — всегда пропускаем после нормализации.
		// Если путь всё ещё невалидный — логируем но принимаем.
		if !trace.ValidatePath(ev.Path) {
			s.logger.Warn("trace path still invalid after normalization", "path", ev.Path)
		}

		// Проверка конфига
		if ev.SessionID != "" && !trace.ShouldTrace(ev.Path, ev.SessionID) {
			skipped++
			continue
		}

		// Санитизация
		data := trace.DeepSanitize(ev.Data)

		entry := store.TraceEntry{
			TracePath: ev.Path,
			Data:      data,
			RequestID: strPtr(ev.RequestID),
		}
		if ev.SessionID != "" {
			entry.SessionID = &ev.SessionID
		}

		// Self-trace — пишем напрямую, минуя батч
		if strings.HasPrefix(ev.Path, "revizor.") {
			if err := s.store.WriteTrace(entry); err != nil {
				s.logger.Error("write trace failed (batch self-trace)", "error", err)
				rejected++
				continue
			}
		} else {
			entries = append(entries, entry)
		}
		accepted++
	}

	// Пакетная запись остальных
	if len(entries) > 0 {
		if err := s.store.WriteTraceBatch(entries); err != nil {
			s.logger.Error("batch write failed", "error", err, "count", len(entries))
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"status": "error",
				"detail": "Ошибка пакетной записи",
			})
			return
		}
		// Webhook dispatch после успешной записи батча (REV-W-001)
		if s.webhookDispatcher != nil {
			for _, entry := range entries {
				s.webhookDispatcher.Dispatch(entry)
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"accepted": accepted,
		"skipped":  skipped,
		"rejected": rejected,
	})
}

// GET /api/v1/trace/log
func (s *Server) handleTraceLogRead(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sessionID := q.Get("session_id")
	lines := 50
	if l := q.Get("lines"); l != "" {
		if parsed, err := fmt.Sscanf(l, "%d", &lines); err != nil || parsed != 1 {
			lines = 50
		}
	}
	pathFilter := q.Get("path")

	var sidPtr, pfPtr *string
	if sessionID != "" {
		sidPtr = &sessionID
	}
	if pathFilter != "" {
		pfPtr = &pathFilter
	}

	logs, err := s.store.ReadTraceLog(sidPtr, 0, lines, pfPtr, nil, nil)
	if err != nil {
		s.logger.Error("read log failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"status": "error",
			"detail": "Ошибка чтения лога",
		})
		return
	}

	if logs == nil {
		logs = []store.TraceEntry{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"logs":  logs,
		"count": len(logs),
	})
}

// POST /api/v1/trace/config
func (s *Server) handleConfigSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TracePath   string  `json:"trace_path"`
		Enabled     *bool   `json:"enabled"`
		SessionID   string  `json:"session_id"`
		Description string  `json:"description"`
		OutputFile  *string `json:"output_file"`
		SampleRate  *float64 `json:"sample_rate"`
	}

	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"status": "error",
			"detail": "Невалидный JSON",
		})
		return
	}

	if req.Enabled == nil {
		enabled := true
		req.Enabled = &enabled
	}

	var sidPtr *string
	if req.SessionID != "" {
		sidPtr = &req.SessionID
	}

	if *req.Enabled {
		opts := store.EnableOpts{}
		if req.Description != "" {
			opts.Description = &req.Description
		}
		opts.OutputFile = req.OutputFile
		opts.SampleRate = req.SampleRate

		if err := s.store.EnableTrace(req.TracePath, sidPtr, opts); err != nil {
			s.logger.Error("enable trace failed", "error", err)
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"status": "error",
				"detail": "Не удалось включить точку",
			})
			return
		}
	} else {
		if err := s.store.DisableTrace(req.TracePath, sidPtr); err != nil {
			s.logger.Error("disable trace failed", "error", err)
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"status": "error",
				"detail": "Не удалось выключить точку",
			})
			return
		}
	}

	// Инвалидация кэша и перекомпиляция
	trace.InvalidateCache(req.SessionID)
	storeRows, _ := s.store.GetConfig(sidPtr)
	traceRows := make([]trace.ConfigRow, len(storeRows))
	for i, r := range storeRows {
		traceRows[i] = trace.ConfigRow{
			TracePath: r.TracePath,
			Enabled:   r.Enabled,
			Owner:     r.Owner,
		}
	}
	trace.CompileConfig(traceRows, sidPtr)

	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"trace_path": req.TracePath,
		"enabled":    *req.Enabled,
	})
}

// GET /api/v1/trace/config
func (s *Server) handleConfigGet(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")

	var sidPtr *string
	if sessionID != "" {
		sidPtr = &sessionID
	}

	storeRows, err := s.store.GetConfig(sidPtr)
	if err != nil {
		s.logger.Error("get config failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"status": "error",
			"detail": "Ошибка получения конфига",
		})
		return
	}

	traceRows := make([]trace.ConfigRow, len(storeRows))
	for i, r := range storeRows {
		traceRows[i] = trace.ConfigRow{
			TracePath: r.TracePath,
			Enabled:   r.Enabled,
			Owner:     r.Owner,
		}
	}

	config, err := trace.CompileConfig(traceRows, sidPtr)
	if err != nil {
		s.logger.Error("compile config failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"status": "error",
			"detail": "Ошибка компиляции конфига",
		})
		return
	}

	if config == nil {
		config = make(map[string]bool)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"session_id":   sessionID,
		"config":       config,
		"total_paths":  len(config),
	})
}

// DELETE /api/v1/trace/config
func (s *Server) handleConfigDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TracePath string `json:"trace_path"`
		SessionID string `json:"session_id"`
	}

	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"status": "error",
			"detail": "Невалидный JSON",
		})
		return
	}

	var sidPtr *string
	if req.SessionID != "" {
		sidPtr = &req.SessionID
	}

	if err := s.store.DisableTrace(req.TracePath, sidPtr); err != nil {
		s.logger.Error("delete config failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"status": "error",
			"detail": "Не удалось удалить точку",
		})
		return
	}

	trace.InvalidateCache(req.SessionID)
	storeRows, _ := s.store.GetConfig(sidPtr)
	traceRowsDel := make([]trace.ConfigRow, len(storeRows))
	for i, r := range storeRows {
		traceRowsDel[i] = trace.ConfigRow{
			TracePath: r.TracePath,
			Enabled:   r.Enabled,
			Owner:     r.Owner,
		}
	}
	trace.CompileConfig(traceRowsDel, sidPtr)

	writeJSON(w, http.StatusOK, map[string]string{
		"status":     "ok",
		"trace_path": req.TracePath,
	})
}

// POST /api/v1/trace/session
func (s *Server) handleSessionCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Owner       string `json:"owner"`
		Description string `json:"description"`
	}

	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"status": "error",
			"detail": "Невалидный JSON",
		})
		return
	}

	if req.Owner == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"status": "error",
			"detail": "owner обязателен",
		})
		return
	}

	// Очистка истекших сессий перед созданием
	s.store.ExpireOutdatedSessions()

	sess, err := s.store.CreateSession(req.Owner, req.Description)
	if err != nil {
		s.logger.Error("create session failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"status": "error",
			"detail": "Ошибка создания сессии",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"session_id":  sess.SessionID,
		"owner":       sess.Owner,
		"description": sess.Description,
		"created_at":  sess.CreatedAt,
		"expires_at":  sess.ExpiresAt,
	})
}

// GET /api/v1/trace/sessions
func (s *Server) handleSessionsList(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.store.GetActiveSessions()
	if err != nil {
		s.logger.Error("list sessions failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"status": "error",
			"detail": "Ошибка получения сессий",
		})
		return
	}

	if sessions == nil {
		sessions = []store.Session{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"sessions": sessions,
	})
}

// GET /api/v1/trace/stats
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")

	var sidPtr *string
	if sessionID != "" {
		sidPtr = &sessionID
	}

	stats, err := s.store.GetTraceStats(sidPtr)
	if err != nil {
		s.logger.Error("stats failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"status": "error",
			"detail": "Ошибка получения статистики",
		})
		return
	}

	writeJSON(w, http.StatusOK, stats)
}

// POST /mcp
func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read body"})
		return
	}
	defer r.Body.Close()

	response := s.mcpHandler.HandleJSONRPCSafe(r.Context(), body)

	w.Header().Set("Content-Type", "application/json")
	w.Write(response)
}

// --- Батчинг ---

func (s *Server) enqueue(entry store.TraceEntry) {
	s.bufMu.Lock()
	s.buffer = append(s.buffer, entry)
	shouldFlush := len(s.buffer) >= batchSize
	s.bufMu.Unlock()

	if shouldFlush {
		s.flushBuffer()
	}
}

func (s *Server) flushBuffer() {
	s.bufMu.Lock()
	if len(s.buffer) == 0 {
		s.bufMu.Unlock()
		return
	}
	batch := make([]store.TraceEntry, len(s.buffer))
	copy(batch, s.buffer)
	s.buffer = s.buffer[:0]
	s.bufMu.Unlock()

	if err := s.store.WriteTraceBatch(batch); err != nil {
		s.logger.Error("batch write failed", "error", err, "count", len(batch))
		// Возвращаем записи в буфер для retry
		s.bufMu.Lock()
		s.buffer = append(batch, s.buffer...)
		s.bufMu.Unlock()
		return
	}
	// Webhook dispatch после успешной записи батча (REV-W-001)
	if s.webhookDispatcher != nil {
		for _, entry := range batch {
			s.webhookDispatcher.Dispatch(entry)
		}
	}
}

func (s *Server) batchWorker() {
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.flushBuffer()
		case <-s.stopCh:
			ticker.Stop()
			s.flushBuffer()
			return
		}
	}
}

// POST /api/v1/trace/shutdown
func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	s.logger.Info("HTTP shutdown requested")
	if err := s.InitiateShutdown(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"status": "error",
			"detail": "Ошибка при остановке",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"message": "Revizor is shutting down gracefully...",
	})
}

// --- Вспомогательные функции ---

func decodeJSON(r *http.Request, v any) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	return json.Unmarshal(body, v)
}

func encodeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
