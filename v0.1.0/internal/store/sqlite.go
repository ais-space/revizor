package store

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/license"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

//go:embed migrations/001_init.sql
var migration001SQL string

//go:embed migrations/002_orchestrator_meta.sql
var migration002SQL string

//go:embed migrations/003_audit_log.sql
var migration003SQL string

// SQLiteStore — реализация TraceStore на SQLite.
type SQLiteStore struct {
	db      *sql.DB
	ttlHours int
	lic     *license.License
}

// SetLicense устанавливает лицензию для проверки лимитов при записи.
func (s *SQLiteStore) SetLicense(lic *license.License) {
	s.lic = lic
}

// effLim хранит актуальные лимиты с учётом воскресного безлимита.
// Обновляется из MCP handler перед каждым вызовом инструмента.
var effLimMu sync.RWMutex
var effLim *license.Limitations

// SetEffectiveLimits обновляет эффективные лимиты (с учётом воскресного безлимита).
func (s *SQLiteStore) SetEffectiveLimits(lim license.Limitations) {
	effLimMu.Lock()
	defer effLimMu.Unlock()
	l := lim
	effLim = &l
}

// NewSQLiteStore открывает БД SQLite и выполняет миграции.
func NewSQLiteStore(path string, ttlHours int) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("открытие SQLite: %w", err)
	}

	db.SetMaxOpenConns(1) // SQLite: одно соединение для избежания блокировок

	s := &SQLiteStore{db: db, ttlHours: ttlHours}
	if err := s.Migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("миграция: %w", err)
	}

	return s, nil
}

// Migrate выполняет SQL-миграции из embedded файлов.
// Миграция 001 — обязательная (схема БД).
// Миграция 002 — опциональная (ALTER TABLE ADD COLUMN, может уже существовать).
// Миграция 003 — опциональная (CREATE TABLE IF NOT EXISTS).
func (s *SQLiteStore) Migrate() error {
	if _, err := s.db.Exec(migration001SQL); err != nil {
		return fmt.Errorf("выполнение миграции 001: %w", err)
	}
	// Миграция 002: ALTER TABLE ADD COLUMN — ошибка "duplicate column" не фатальна
	if _, err := s.db.Exec(migration002SQL); err != nil {
		errStr := err.Error()
		if !strings.Contains(errStr, "duplicate column") && !strings.Contains(errStr, "already exists") {
			return fmt.Errorf("выполнение миграции 002: %w", err)
		}
	}
	// Миграция 003: CREATE TABLE IF NOT EXISTS — идемпотентна
	if _, err := s.db.Exec(migration003SQL); err != nil {
		return fmt.Errorf("выполнение миграции 003 (audit_log): %w", err)
	}
	return nil
}

// Close закрывает соединение с БД.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// EnableTrace включает trace-точку (или обновляет существующую).
func (s *SQLiteStore) EnableTrace(path string, sessionID *string, opts EnableOpts) error {
	var desc, outFile any
	var sampleRate float64 = 1.0

	if opts.Description != nil {
		desc = *opts.Description
	}
	if opts.OutputFile != nil {
		outFile = *opts.OutputFile
	}
	if opts.SampleRate != nil {
		sampleRate = *opts.SampleRate
	}

	_, err := s.db.Exec(`
		INSERT INTO trace_config (trace_path, enabled, output_file, sample_rate, owner, description, updated_at)
		VALUES (?, 1, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(trace_path, owner) DO UPDATE SET
			enabled = 1,
			output_file = COALESCE(?, output_file),
			sample_rate = COALESCE(?, sample_rate),
			description = COALESCE(?, description),
			updated_at = datetime('now')
	`, path, outFile, sampleRate, sessionID, desc, outFile, sampleRate, desc)

	if err != nil {
		return fmt.Errorf("enable_trace: %w", err)
	}
	return nil
}

// DisableTrace выключает trace-точку.
func (s *SQLiteStore) DisableTrace(path string, sessionID *string) error {
	_, err := s.db.Exec(`
		UPDATE trace_config SET enabled = 0, updated_at = datetime('now')
		WHERE trace_path = ? AND (owner = ? OR (owner IS NULL AND ? IS NULL))
	`, path, sessionID, sessionID)
	if err != nil {
		return fmt.Errorf("disable_trace: %w", err)
	}
	return nil
}

// GetConfig возвращает все записи конфигурации для сессии и глобальные.
func (s *SQLiteStore) GetConfig(sessionID *string) ([]TraceConfigRow, error) {
	rows, err := s.db.Query(`
		SELECT trace_path, enabled, output_file, sample_rate, owner, description
		FROM trace_config
		WHERE owner = ? OR owner IS NULL
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("get_config: %w", err)
	}
	defer rows.Close()

	var result []TraceConfigRow
	for rows.Next() {
		var r TraceConfigRow
		if err := rows.Scan(&r.TracePath, &r.Enabled, &r.OutputFile, &r.SampleRate, &r.Owner, &r.Description); err != nil {
			return nil, fmt.Errorf("get_config scan: %w", err)
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// CreateSession создаёт новую сессию трассировки.
// Проверяет лимит сессий согласно лицензии (Community: 1, Pro: 10, Enterprise: unlimited).
// Воскресный безлимит (v55): в воскресенье лимиты снимаются для всех tier.
func (s *SQLiteStore) CreateSession(owner, description string) (*Session, error) {
	isSunday := license.CheckLocalTime().IsSunday
	limits := license.EffectiveLimits(s.lic, isSunday)

	// Проверка лимита сессий
	if limits.MaxSessions > 0 {
		var activeCount int
		err := s.db.QueryRow(`SELECT COUNT(*) FROM trace_session WHERE expires_at > datetime('now') OR expires_at IS NULL`).Scan(&activeCount)
		if err != nil {
			return nil, fmt.Errorf("create_session count: %w", err)
		}
		if activeCount >= limits.MaxSessions {
			// Попытка авто-экспайра истекших сессий перед отказом
			expired, _ := s.ExpireOutdatedSessions()
			if expired > 0 {
				// Перепроверяем после экспайра
				err2 := s.db.QueryRow(`SELECT COUNT(*) FROM trace_session WHERE expires_at > datetime('now') OR expires_at IS NULL`).Scan(&activeCount)
				if err2 == nil && activeCount < limits.MaxSessions {
					goto create
				}
			}
			return nil, fmt.Errorf("достигнут лимит активных сессий (%d). Увеличьте лимит, получив Pro-лицензию: https://ais-platform.dev/revizor", limits.MaxSessions)
		}
	}

create:

	sessionID := uuid.New().String()
	expiresAt := time.Now().UTC().Add(time.Duration(s.ttlHours) * time.Hour).Format("2006-01-02T15:04:05Z")

	var desc *string
	if description != "" {
		desc = &description
	}

	_, err := s.db.Exec(`
		INSERT INTO trace_session (session_id, owner, description, created_at, expires_at)
		VALUES (?, ?, ?, datetime('now'), ?)
	`, sessionID, owner, desc, expiresAt)
	if err != nil {
		return nil, fmt.Errorf("create_session: %w", err)
	}

	createdAt := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	return &Session{
		SessionID:   sessionID,
		Owner:       owner,
		Description: desc,
		CreatedAt:   createdAt,
		ExpiresAt:   &expiresAt,
	}, nil
}

// GetActiveSessions возвращает список активных сессий.
func (s *SQLiteStore) GetActiveSessions() ([]Session, error) {
	rows, err := s.db.Query(`
		SELECT session_id, owner, description, created_at, expires_at
		FROM trace_session
		WHERE expires_at > datetime('now') OR expires_at IS NULL
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("get_active_sessions: %w", err)
	}
	defer rows.Close()

	var result []Session
	for rows.Next() {
		var sess Session
		if err := rows.Scan(&sess.SessionID, &sess.Owner, &sess.Description, &sess.CreatedAt, &sess.ExpiresAt); err != nil {
			return nil, fmt.Errorf("get_active_sessions scan: %w", err)
		}
		result = append(result, sess)
	}
	return result, rows.Err()
}

// ExpireSession завершает сессию: выключает все её точки и устанавливает expires_at.
func (s *SQLiteStore) ExpireSession(sessionID string) error {
	// Выключить все точки сессии
	_, err := s.db.Exec(`UPDATE trace_config SET enabled = 0 WHERE owner = ? AND enabled = 1`, sessionID)
	if err != nil {
		return fmt.Errorf("expire_session disable: %w", err)
	}
	// Установить expires_at
	_, err = s.db.Exec(`UPDATE trace_session SET expires_at = datetime('now') WHERE session_id = ?`, sessionID)
	if err != nil {
		return fmt.Errorf("expire_session update: %w", err)
	}
	return nil
}

// ExpireOutdatedSessions завершает все истекшие сессии.
func (s *SQLiteStore) ExpireOutdatedSessions() (int, error) {
	rows, err := s.db.Query(`SELECT session_id FROM trace_session WHERE expires_at <= datetime('now') AND expires_at IS NOT NULL`)
	if err != nil {
		return 0, fmt.Errorf("expire_outdated query: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	for _, id := range ids {
		if err := s.ExpireSession(id); err != nil {
			return 0, err
		}
	}

	return len(ids), nil
}

// WriteTrace записывает одно trace-событие.
// Проверяет лимит событий в день согласно лицензии.
// Воскресный безлимит (v55): в воскресенье лимиты снимаются для всех tier.
func (s *SQLiteStore) WriteTrace(entry TraceEntry) error {
	isSunday := license.CheckLocalTime().IsSunday
	limits := license.EffectiveLimits(s.lic, isSunday)

	// Проверка лимита событий в день
	if limits.MaxEventsPerDay > 0 {
		var todayCount int
		err := s.db.QueryRow(`SELECT COUNT(*) FROM trace_log WHERE date(created_at) = date('now')`).Scan(&todayCount)
		if err != nil {
			return fmt.Errorf("write_trace count: %w", err)
		}
		if todayCount >= limits.MaxEventsPerDay {
			return fmt.Errorf("достигнут дневной лимит событий (%d). Получите Pro-лицензию: https://ais-platform.dev/revizor", limits.MaxEventsPerDay)
		}
	}

	dataJSON, err := json.Marshal(entry.Data)
	if err != nil {
		return fmt.Errorf("write_trace marshal: %w", err)
	}

	var orchMeta *string
	if entry.OrchestratorMeta != nil && len(entry.OrchestratorMeta) > 0 {
		orchJSON, errMar := json.Marshal(entry.OrchestratorMeta)
		if errMar != nil {
			return fmt.Errorf("write_trace orchestrator_meta marshal: %w", errMar)
		}
		s := string(orchJSON)
		orchMeta = &s
	}

	if entry.SessionID == nil {
		slog.Debug("trace event without session_id", "trace_path", entry.TracePath)
	}

	_, err = s.db.Exec(`
		INSERT INTO trace_log (trace_path, data, session_id, request_id, orchestrator_meta)
		VALUES (?, ?, ?, ?, ?)
	`, entry.TracePath, string(dataJSON), entry.SessionID, entry.RequestID, orchMeta)
	if err != nil {
		return fmt.Errorf("write_trace: %w", err)
	}
	return nil
}

// WriteTraceBatch записывает несколько trace-событий в одной транзакции.
func (s *SQLiteStore) WriteTraceBatch(entries []TraceEntry) error {
	isSunday := license.CheckLocalTime().IsSunday
	limits := license.EffectiveLimits(s.lic, isSunday)

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("write_batch begin: %w", err)
	}
	defer tx.Rollback()

	// Проверка лимита событий в день — внутри транзакции (BUG-REV-006)
	if limits.MaxEventsPerDay > 0 {
		var todayCount int
		err := tx.QueryRow(`SELECT COUNT(*) FROM trace_log WHERE date(created_at) = date('now')`).Scan(&todayCount)
		if err != nil {
			return fmt.Errorf("write_batch count: %w", err)
		}
		if todayCount >= limits.MaxEventsPerDay {
			return fmt.Errorf("достигнут дневной лимит событий (%d). Получите Pro-лицензию: https://ais-platform.dev/revizor", limits.MaxEventsPerDay)
		}
	}

	stmt, err := tx.Prepare(`INSERT INTO trace_log (trace_path, data, session_id, request_id, orchestrator_meta) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("write_batch prepare: %w", err)
	}
	defer stmt.Close()

	for _, entry := range entries {
		dataJSON, errMar := json.Marshal(entry.Data)
		if errMar != nil {
			return fmt.Errorf("write_batch marshal: %w", errMar)
		}
		var orchMeta *string
		if entry.OrchestratorMeta != nil && len(entry.OrchestratorMeta) > 0 {
			orchJSON, errOrch := json.Marshal(entry.OrchestratorMeta)
			if errOrch != nil {
				return fmt.Errorf("write_batch orchestrator_meta marshal: %w", errOrch)
			}
			s := string(orchJSON)
			orchMeta = &s
		}
		if _, err := stmt.Exec(entry.TracePath, string(dataJSON), entry.SessionID, entry.RequestID, orchMeta); err != nil {
			return fmt.Errorf("write_batch exec: %w", err)
		}
	}

	return tx.Commit()
}

// ReadTraceLog читает записи лога.
func (s *SQLiteStore) ReadTraceLog(sessionID *string, offset int, limit int, pathFilter *string, since, until *time.Time) ([]TraceEntry, error) {
	query := `SELECT id, trace_path, data, session_id, request_id, created_at, orchestrator_meta FROM trace_log WHERE 1=1`
	var args []any

	if sessionID != nil {
		query += ` AND session_id = ?`
		args = append(args, *sessionID)
	}
	if pathFilter != nil {
		query += ` AND trace_path LIKE ?`
		args = append(args, strings.ReplaceAll(*pathFilter, "*", "%"))
	}
	if since != nil {
		query += ` AND created_at >= ?`
		args = append(args, since.Format("2006-01-02T15:04:05Z"))
	}
	if until != nil {
		query += ` AND created_at <= ?`
		args = append(args, until.Format("2006-01-02T15:04:05Z"))
	}

	query += ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("read_trace_log: %w", err)
	}
	defer rows.Close()

	var result []TraceEntry
	for rows.Next() {
		var e TraceEntry
		var dataStr string
		var orchMetaStr *string
		if err := rows.Scan(&e.ID, &e.TracePath, &dataStr, &e.SessionID, &e.RequestID, &e.CreatedAt, &orchMetaStr); err != nil {
			return nil, fmt.Errorf("read_trace_log scan: %w", err)
		}
		if dataStr != "" {
			var data any
			if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
				e.Data = dataStr
			} else {
				e.Data = data
			}
		}
		if orchMetaStr != nil && *orchMetaStr != "" {
			var meta map[string]any
			if err := json.Unmarshal([]byte(*orchMetaStr), &meta); err == nil {
				e.OrchestratorMeta = meta
			}
		}
		result = append(result, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Реверсировать (как в Python: ORDER BY DESC + reverse)
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	return result, nil
}

// SearchTraceLog ищет записи лога по подстроке.
func (s *SQLiteStore) SearchTraceLog(search string, sessionID *string, pathFilter *string, offset int, limit int, since, until *time.Time) ([]TraceEntry, error) {
	return s.SearchTraceLogWithContext(search, sessionID, pathFilter, nil, offset, limit, since, until, 0)
}

// buildSearchQuery строит WHERE-условия для поиска. Переиспользуется SearchTraceLogWithContext и CountByPath.
func (s *SQLiteStore) buildSearchQuery(search string, sessionID *string, pathFilter *string, dataFilter *string) (string, []any) {
	var query strings.Builder
	var args []any

	// Основное условие поиска
	if search != "" {
		query.WriteString(` WHERE (data LIKE ? OR trace_path LIKE ?)`)
		searchPattern := "%" + search + "%"
		args = append(args, searchPattern, searchPattern)
	} else {
		query.WriteString(` WHERE 1=1`)
	}

	if sessionID != nil {
		query.WriteString(` AND session_id = ?`)
		args = append(args, *sessionID)
	}
	if pathFilter != nil {
		query.WriteString(` AND trace_path LIKE ?`)
		args = append(args, strings.ReplaceAll(*pathFilter, "*", "%"))
	}
	// dataFilter (REV-008): "key=value" — точное совпадение, "key" — проверка существования
	if dataFilter != nil && *dataFilter != "" {
		if idx := strings.IndexByte(*dataFilter, '='); idx >= 0 {
			key := (*dataFilter)[:idx]
			value := (*dataFilter)[idx+1:]
			query.WriteString(` AND json_extract(data, ?) = ?`)
			args = append(args, "$."+key, value)
		} else {
			query.WriteString(` AND json_extract(data, ?) IS NOT NULL`)
			args = append(args, "$."+*dataFilter)
		}
	}

	return query.String(), args
}

// SearchTraceLogWithContext ищет записи с опциональным контекстом (REV-010: context_lines).
func (s *SQLiteStore) SearchTraceLogWithContext(search string, sessionID *string, pathFilter *string, dataFilter *string, offset int, limit int, since, until *time.Time, contextLines int) ([]TraceEntry, error) {
	baseQuery, baseArgs := s.buildSearchQuery(search, sessionID, pathFilter, dataFilter)

	// Добавляем since/until
	var timeArgs []any
	timeClause := ""
	if since != nil {
		timeClause += ` AND created_at >= ?`
		timeArgs = append(timeArgs, since.Format("2006-01-02T15:04:05Z"))
	}
	if until != nil {
		timeClause += ` AND created_at <= ?`
		timeArgs = append(timeArgs, until.Format("2006-01-02T15:04:05Z"))
	}

	// Основной запрос с лимитом и оффсетом
	query := `SELECT id, trace_path, data, session_id, request_id, created_at, orchestrator_meta FROM trace_log`
	query += baseQuery + timeClause
	query += ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	allArgs := append(baseArgs, timeArgs...)
	allArgs = append(allArgs, limit, offset)

	rows, err := s.db.Query(query, allArgs...)
	if err != nil {
		return nil, fmt.Errorf("search_trace_log: %w", err)
	}
	defer rows.Close()

	var matched []TraceEntry
	for rows.Next() {
		var e TraceEntry
		var dataStr string
		var orchMetaStr *string
		if err := rows.Scan(&e.ID, &e.TracePath, &dataStr, &e.SessionID, &e.RequestID, &e.CreatedAt, &orchMetaStr); err != nil {
			return nil, fmt.Errorf("search_trace_log scan: %w", err)
		}
		if dataStr != "" {
			var data any
			if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
				e.Data = dataStr
			} else {
				e.Data = data
			}
		}
		if orchMetaStr != nil && *orchMetaStr != "" {
			var meta map[string]any
			if err := json.Unmarshal([]byte(*orchMetaStr), &meta); err == nil {
				e.OrchestratorMeta = meta
			}
		}
		matched = append(matched, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Без контекста — простой реверс
	if contextLines <= 0 || len(matched) == 0 {
		for i, j := 0, len(matched)-1; i < j; i, j = i+1, j-1 {
			matched[i], matched[j] = matched[j], matched[i]
		}
		return matched, nil
	}

	// REV-010: контекстные строки — выборка соседних ID
	return s.expandContext(matched, sessionID, pathFilter, dataFilter, since, until, contextLines, baseQuery, timeClause, baseArgs, timeArgs)
}

// expandContext добавляет контекстные строки вокруг найденных событий.
func (s *SQLiteStore) expandContext(matched []TraceEntry, sessionID *string, pathFilter *string, dataFilter *string, since, until *time.Time, contextLines int, baseQuery string, timeClause string, baseArgs []any, timeArgs []any) ([]TraceEntry, error) {
	seen := make(map[int64]bool, len(matched)*(1+2*contextLines))
	var result []TraceEntry

	// Собираем ID-диапазоны для контекста
	for _, e := range matched {
		idMin := e.ID - int64(contextLines)
		idMax := e.ID + int64(contextLines)

		// Строим запрос с теми же фильтрами (кроме search), плюс диапазон ID
		ctxQuery := `SELECT id, trace_path, data, session_id, request_id, created_at, orchestrator_meta FROM trace_log`
		ctxQuery += baseQuery
		ctxQuery += ` AND id >= ? AND id <= ?` + timeClause
		ctxQuery += ` ORDER BY created_at ASC`
		ctxAllArgs := append([]any{}, baseArgs...)
		ctxAllArgs = append(ctxAllArgs, idMin, idMax)
		ctxAllArgs = append(ctxAllArgs, timeArgs...)

		ctxRows, err := s.db.Query(ctxQuery, ctxAllArgs...)
		if err != nil {
			return nil, fmt.Errorf("expand_context: %w", err)
		}
		for ctxRows.Next() {
			var ce TraceEntry
			var dataStr string
			var orchMetaStr *string
			if err := ctxRows.Scan(&ce.ID, &ce.TracePath, &dataStr, &ce.SessionID, &ce.RequestID, &ce.CreatedAt, &orchMetaStr); err != nil {
				ctxRows.Close()
				return nil, fmt.Errorf("expand_context scan: %w", err)
			}
			if seen[ce.ID] {
				continue
			}
			seen[ce.ID] = true
			if dataStr != "" {
				var data any
				if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
					ce.Data = dataStr
				} else {
					ce.Data = data
				}
			}
			if orchMetaStr != nil && *orchMetaStr != "" {
				var meta map[string]any
				if err := json.Unmarshal([]byte(*orchMetaStr), &meta); err == nil {
					ce.OrchestratorMeta = meta
				}
			}
			result = append(result, ce)
		}
		ctxRows.Close()
	}

	return result, nil
}

// CountByPath возвращает агрегацию количества событий по trace_path (REV-009).
func (s *SQLiteStore) CountByPath(search string, dataFilter *string, pathFilter *string, sessionID *string, since, until *time.Time) (map[string]int64, error) {
	baseQuery, baseArgs := s.buildSearchQuery(search, sessionID, pathFilter, dataFilter)

	var timeArgs []any
	timeClause := ""
	if since != nil {
		timeClause += ` AND created_at >= ?`
		timeArgs = append(timeArgs, since.Format("2006-01-02T15:04:05Z"))
	}
	if until != nil {
		timeClause += ` AND created_at <= ?`
		timeArgs = append(timeArgs, until.Format("2006-01-02T15:04:05Z"))
	}

	query := `SELECT trace_path, COUNT(*) as cnt FROM trace_log`
	query += baseQuery + timeClause
	query += ` GROUP BY trace_path ORDER BY trace_path`
	allArgs := append(baseArgs, timeArgs...)

	rows, err := s.db.Query(query, allArgs...)
	if err != nil {
		return nil, fmt.Errorf("count_by_path: %w", err)
	}
	defer rows.Close()

	result := make(map[string]int64)
	for rows.Next() {
		var path string
		var cnt int64
		if err := rows.Scan(&path, &cnt); err != nil {
			return nil, fmt.Errorf("count_by_path scan: %w", err)
		}
		result[path] = cnt
	}
	return result, rows.Err()
}

// GetTraceStats возвращает статистику сессии.
func (s *SQLiteStore) GetTraceStats(sessionID *string) (*TraceStats, error) {
	stats := &TraceStats{SessionID: sessionID}

	whereClause := ""
	var args []any
	if sessionID != nil {
		whereClause = " WHERE session_id = ?"
		args = append(args, *sessionID)
	}

	// Total events
	err := s.db.QueryRow(`SELECT COUNT(*) FROM trace_log`+whereClause, args...).Scan(&stats.TotalEvents)
	if err != nil {
		return nil, fmt.Errorf("stats count: %w", err)
	}

	// Unique paths
	err = s.db.QueryRow(`SELECT COUNT(DISTINCT trace_path) FROM trace_log`+whereClause, args...).Scan(&stats.UniquePaths)
	if err != nil {
		return nil, fmt.Errorf("stats unique: %w", err)
	}

	// Last event
	var lastAt sql.NullString
	err = s.db.QueryRow(`SELECT created_at FROM trace_log`+whereClause+` ORDER BY created_at DESC LIMIT 1`, args...).Scan(&lastAt)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("stats last: %w", err)
	}
	if lastAt.Valid {
		stats.LastEventAt = &lastAt.String
	}

	return stats, nil
}

// GetDistinctPaths возвращает все уникальные trace-пути (из лога + зарегистрированные).
func (s *SQLiteStore) GetDistinctPaths(sessionID *string, modulePrefix *string) ([]string, error) {
	query := `SELECT trace_path FROM (
		SELECT DISTINCT trace_path FROM trace_log WHERE 1=1`
	var args []any

	if sessionID != nil && *sessionID != "" {
		query += ` AND session_id = ?`
		args = append(args, *sessionID)
	}
	if modulePrefix != nil && *modulePrefix != "" {
		query += ` AND trace_path LIKE ?`
		args = append(args, *modulePrefix+".%")
	}

	query += ` UNION SELECT DISTINCT trace_path FROM trace_config WHERE 1=1`

	if sessionID != nil && *sessionID != "" {
		query += ` AND owner = ?`
		args = append(args, *sessionID)
	}
	if modulePrefix != nil && *modulePrefix != "" {
		query += ` AND trace_path LIKE ?`
		args = append(args, *modulePrefix+".%")
	}

	query += `) ORDER BY trace_path`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("get_distinct_paths: %w", err)
	}
	defer rows.Close()

	var result []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, fmt.Errorf("get_distinct_paths scan: %w", err)
		}
		result = append(result, path)
	}
	return result, rows.Err()
}

// GetPathFrequency возвращает частоту срабатывания trace-точки по минутным интервалам.
func (s *SQLiteStore) GetPathFrequency(sessionID *string, path string, hours int) ([]FrequencyBucket, error) {
	offset := fmt.Sprintf("-%d hours", hours)
	query := `
		SELECT strftime('%Y-%m-%dT%H:%M:00', created_at) AS bucket, COUNT(*) AS cnt
		FROM trace_log
		WHERE trace_path = ?
		  AND created_at >= datetime('now', ?)
	`
	args := []any{path, offset}

	if sessionID != nil && *sessionID != "" {
		query += ` AND session_id = ?`
		args = append(args, *sessionID)
	}

	query += ` GROUP BY bucket ORDER BY bucket`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("get_path_frequency: %w", err)
	}
	defer rows.Close()

	var result []FrequencyBucket
	for rows.Next() {
		var b FrequencyBucket
		if err := rows.Scan(&b.Bucket, &b.Count); err != nil {
			return nil, fmt.Errorf("get_path_frequency scan: %w", err)
		}
		result = append(result, b)
	}
	return result, rows.Err()
}

// GetRequestChain возвращает все trace-события для одного request_id.
func (s *SQLiteStore) GetRequestChain(requestID string, sessionID *string) ([]TraceEntry, error) {
	query := `SELECT id, trace_path, data, session_id, request_id, created_at, orchestrator_meta FROM trace_log WHERE request_id = ?`
	args := []any{requestID}

	if sessionID != nil && *sessionID != "" {
		query += ` AND session_id = ?`
		args = append(args, *sessionID)
	}

	query += ` ORDER BY created_at ASC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("get_request_chain: %w", err)
	}
	defer rows.Close()

	var result []TraceEntry
	for rows.Next() {
		var e TraceEntry
		var dataStr string
		var orchMetaStr *string
		if err := rows.Scan(&e.ID, &e.TracePath, &dataStr, &e.SessionID, &e.RequestID, &e.CreatedAt, &orchMetaStr); err != nil {
			return nil, fmt.Errorf("get_request_chain scan: %w", err)
		}
		if dataStr != "" {
			var data any
			if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
				e.Data = dataStr
			} else {
				e.Data = data
			}
		}
		if orchMetaStr != nil && *orchMetaStr != "" {
			var meta map[string]any
			if err := json.Unmarshal([]byte(*orchMetaStr), &meta); err == nil {
				e.OrchestratorMeta = meta
			}
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

// GetEnterChains находит .enter события без соответствующего .success или .failed.
func (s *SQLiteStore) GetEnterChains(sessionID *string, modulePrefix *string) ([]EnterChain, error) {
	query := `
		SELECT DISTINCT
			REPLACE(e.trace_path, '.enter', '') AS base_path,
			e.trace_path AS enter_path
		FROM trace_log e
		WHERE e.trace_path LIKE '%.enter'
		  AND e.request_id IS NOT NULL
	`
	var args []any

	if sessionID != nil && *sessionID != "" {
		query += ` AND e.session_id = ?`
		args = append(args, *sessionID)
	}
	if modulePrefix != nil && *modulePrefix != "" {
		query += ` AND e.trace_path LIKE ?`
		args = append(args, *modulePrefix+".%")
	}

	query += `
		  AND NOT EXISTS (
			SELECT 1 FROM trace_log s
			WHERE s.request_id = e.request_id
			  AND s.session_id = e.session_id
			  AND (s.trace_path = REPLACE(e.trace_path, '.enter', '.success')
				   OR s.trace_path = REPLACE(e.trace_path, '.enter', '.failed'))
		  )
		ORDER BY base_path
	`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("get_enter_chains: %w", err)
	}
	defer rows.Close()

	var result []EnterChain
	for rows.Next() {
		var c EnterChain
		if err := rows.Scan(&c.BasePath, &c.EnterPath); err != nil {
			return nil, fmt.Errorf("get_enter_chains scan: %w", err)
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

// SeedPresets заполняет таблицу trace_preset из YAML-конфига (INSERT OR IGNORE).
func (s *SQLiteStore) SeedPresets(presets map[string]config.PresetConfig) error {
	for name, preset := range presets {
		pathsJSON, err := json.Marshal(preset.Paths)
		if err != nil {
			return fmt.Errorf("seed_presets marshal %s: %w", name, err)
		}
		desc := preset.Description
		if desc == "" {
			desc = name
		}
		_, err = s.db.Exec(`
			INSERT OR IGNORE INTO trace_preset (name, description, paths)
			VALUES (?, ?, ?)
		`, name, desc, string(pathsJSON))
		if err != nil {
			return fmt.Errorf("seed_presets insert %s: %w", name, err)
		}
	}
	return nil
}

// GetPresets возвращает все пресеты из БД. Если БД пуста — возвращает пресеты из YAML.
func (s *SQLiteStore) GetPresets() ([]Preset, error) {
	rows, err := s.db.Query(`SELECT name, description, paths FROM trace_preset ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("get_presets: %w", err)
	}
	defer rows.Close()

	var result []Preset
	for rows.Next() {
		var p Preset
		var pathsJSON string
		if err := rows.Scan(&p.Name, &p.Description, &pathsJSON); err != nil {
			return nil, fmt.Errorf("get_presets scan: %w", err)
		}
		if err := json.Unmarshal([]byte(pathsJSON), &p.Paths); err != nil {
			p.Paths = []string{}
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

// SetPreset создаёт или обновляет пресет в БД.
func (s *SQLiteStore) SetPreset(name, description string, paths []string) error {
	pathsJSON, err := json.Marshal(paths)
	if err != nil {
		return fmt.Errorf("set_preset marshal: %w", err)
	}
	if description == "" {
		description = name
	}
	_, err = s.db.Exec(`
		INSERT INTO trace_preset (name, description, paths, updated_at)
		VALUES (?, ?, ?, datetime('now'))
		ON CONFLICT(name) DO UPDATE SET
			description = excluded.description,
			paths = excluded.paths,
			updated_at = datetime('now')
	`, name, description, string(pathsJSON))
	if err != nil {
		return fmt.Errorf("set_preset: %w", err)
	}
	return nil
}

// DeletePreset удаляет пресет из БД.
func (s *SQLiteStore) DeletePreset(name string) error {
	_, err := s.db.Exec(`DELETE FROM trace_preset WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("delete_preset: %w", err)
	}
	return nil
}

// OrchestratorEvent — событие оркестратора (для QueryOrchestratorEvents).
type OrchestratorEvent struct {
	CreatedAt string `json:"created_at"`
	TracePath string `json:"trace_path"`
	EventType string `json:"event_type"`
	Data      string `json:"data"`
}

// QueryOrchestratorEvents возвращает события оркестратора по task_id за временное окно.
func (s *SQLiteStore) QueryOrchestratorEvents(taskID string, windowSec int) ([]OrchestratorEvent, error) {
	offset := fmt.Sprintf("-%d seconds", windowSec)
	rows, err := s.db.Query(`
		SELECT created_at, trace_path, orchestrator_meta, data
		FROM trace_log
		WHERE json_extract(orchestrator_meta, '$.task_id') = ?
		  AND created_at >= datetime('now', ?)
		ORDER BY created_at ASC
	`, taskID, offset)
	if err != nil {
		return nil, fmt.Errorf("query orchestrator events: %w", err)
	}
	defer rows.Close()

	var events []OrchestratorEvent
	for rows.Next() {
		var createdAt, tracePath, dataStr string
		var orchMetaStr *string
		if errScan := rows.Scan(&createdAt, &tracePath, &orchMetaStr, &dataStr); errScan != nil {
			return nil, fmt.Errorf("scan orchestrator event: %w", errScan)
		}
		eventType := "unknown"
		if orchMetaStr != nil && *orchMetaStr != "" {
			var meta map[string]any
			if json.Unmarshal([]byte(*orchMetaStr), &meta) == nil {
				if et, ok := meta["event_type"].(string); ok {
					eventType = et
				} else if _, ok := meta["step"]; ok {
					eventType = "step"
				} else if _, ok := meta["retry"]; ok {
					eventType = "retry"
				}
			}
		}
		events = append(events, OrchestratorEvent{
			CreatedAt: createdAt,
			TracePath: tracePath,
			EventType: eventType,
			Data:      dataStr,
		})
	}
	return events, rows.Err()
}

// RegisterPath регистрирует новый trace-путь (если ещё не существует).
func (s *SQLiteStore) RegisterPath(path string, sessionID *string) error {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM trace_config WHERE trace_path = ? AND (owner = ? OR (owner IS NULL AND ? IS NULL))`,
		path, sessionID, sessionID).Scan(&count)
	if err != nil {
		return fmt.Errorf("register_path check: %w", err)
	}
	if count > 0 {
		return nil
	}

	_, err = s.db.Exec(`INSERT INTO trace_config (trace_path, enabled, owner) VALUES (?, 0, ?)`,
		path, sessionID)
	if err != nil {
		return fmt.Errorf("register_path insert: %w", err)
	}
	return nil
}

// WriteAudit записывает запись аудита MCP-операции в БД.
func (s *SQLiteStore) WriteAudit(entry AuditEntry) error {
	_, err := s.db.Exec(
		`INSERT INTO audit_log (tool_name, args, result, error, duration_ms) VALUES (?, ?, ?, ?, ?)`,
		entry.ToolName,
		entry.Args,
		entry.Result,
		entry.Error,
		entry.DurationMs,
	)
	if err != nil {
		return fmt.Errorf("write_audit: %w", err)
	}
	return nil
}
