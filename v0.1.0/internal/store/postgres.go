package store

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/license"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/001_init.postgres.sql
var pgMigration001SQL string

//go:embed migrations/002_orchestrator_meta.postgres.sql
var pgMigration002SQL string

//go:embed migrations/003_audit_log.postgres.sql
var pgMigration003SQL string

// PostgresStore — реализация TraceStore на PostgreSQL (через pgx).
type PostgresStore struct {
	pool     *pgxpool.Pool
	ttlHours int
	lic      *license.License
}

// SetLicense устанавливает лицензию для проверки лимитов при записи.
func (s *PostgresStore) SetLicense(lic *license.License) {
	s.lic = lic
}

// SetEffectiveLimits обновляет эффективные лимиты (с учётом воскресного безлимита).
func (s *PostgresStore) SetEffectiveLimits(lim license.Limitations) {
	effLimMu.Lock()
	defer effLimMu.Unlock()
	l := lim
	effLim = &l
}

// NewPostgresStore создаёт пул соединений PostgreSQL и выполняет миграции.
func NewPostgresStore(databaseURL string, ttlHours int) (*PostgresStore, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("открытие PostgreSQL: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping PostgreSQL: %w", err)
	}

	s := &PostgresStore{pool: pool, ttlHours: ttlHours}
	if err := s.Migrate(); err != nil {
		pool.Close()
		return nil, fmt.Errorf("миграция: %w", err)
	}

	return s, nil
}

// Migrate выполняет SQL-миграции из embedded .postgres.sql файлов.
func (s *PostgresStore) Migrate() error {
	ctx := context.Background()

	if _, err := s.pool.Exec(ctx, pgMigration001SQL); err != nil {
		return fmt.Errorf("выполнение миграции 001: %w", err)
	}
	// Миграция 002: ALTER TABLE ADD COLUMN — ошибка "duplicate column" не фатальна
	if _, err := s.pool.Exec(ctx, pgMigration002SQL); err != nil {
		errStr := err.Error()
		if !strings.Contains(errStr, "duplicate column") && !strings.Contains(errStr, "already exists") {
			return fmt.Errorf("выполнение миграции 002: %w", err)
		}
	}
	// Миграция 003: CREATE TABLE IF NOT EXISTS — идемпотентна
	if _, err := s.pool.Exec(ctx, pgMigration003SQL); err != nil {
		return fmt.Errorf("выполнение миграции 003 (audit_log): %w", err)
	}
	return nil
}

// Close закрывает пул соединений.
func (s *PostgresStore) Close() error {
	s.pool.Close()
	return nil
}

// EnableTrace включает trace-точку (или обновляет существующую).
func (s *PostgresStore) EnableTrace(path string, sessionID *string, opts EnableOpts) error {
	var desc, outFile *string
	sr := 1.0

	if opts.Description != nil {
		desc = opts.Description
	}
	if opts.OutputFile != nil {
		outFile = opts.OutputFile
	}
	if opts.SampleRate != nil {
		sr = *opts.SampleRate
	}

	_, err := s.pool.Exec(context.Background(), `
		INSERT INTO trace_config (trace_path, enabled, output_file, sample_rate, owner, description, updated_at)
		VALUES ($1, 1, $2, $3, $4, $5, NOW())
		ON CONFLICT(trace_path, owner) DO UPDATE SET
			enabled = 1,
			output_file = COALESCE(EXCLUDED.output_file, trace_config.output_file),
			sample_rate = COALESCE(EXCLUDED.sample_rate, trace_config.sample_rate),
			description = COALESCE(EXCLUDED.description, trace_config.description),
			updated_at = NOW()
	`, path, outFile, sr, sessionID, desc)

	if err != nil {
		return fmt.Errorf("enable_trace: %w", err)
	}
	return nil
}

// DisableTrace выключает trace-точку.
func (s *PostgresStore) DisableTrace(path string, sessionID *string) error {
	_, err := s.pool.Exec(context.Background(), `
		UPDATE trace_config SET enabled = 0, updated_at = NOW()
		WHERE trace_path = $1 AND (owner = $2 OR (owner IS NULL AND $2 IS NULL))
	`, path, sessionID)
	if err != nil {
		return fmt.Errorf("disable_trace: %w", err)
	}
	return nil
}

// GetConfig возвращает все записи конфигурации для сессии и глобальные.
func (s *PostgresStore) GetConfig(sessionID *string) ([]TraceConfigRow, error) {
	rows, err := s.pool.Query(context.Background(), `
		SELECT trace_path, (enabled != 0) AS enabled, output_file, sample_rate, owner, description
		FROM trace_config
		WHERE owner = $1 OR owner IS NULL
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
func (s *PostgresStore) CreateSession(owner, description string) (*Session, error) {
	ctx := context.Background()
	isSunday := license.CheckLocalTime().IsSunday
	limits := license.EffectiveLimits(s.lic, isSunday)

	// Проверка лимита сессий
	if limits.MaxSessions > 0 {
		var activeCount int
		err := s.pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM trace_session WHERE expires_at > NOW() OR expires_at IS NULL`).Scan(&activeCount)
		if err != nil {
			return nil, fmt.Errorf("create_session count: %w", err)
		}
		if activeCount >= limits.MaxSessions {
			expired, _ := s.ExpireOutdatedSessions()
			if expired > 0 {
				err2 := s.pool.QueryRow(ctx,
					`SELECT COUNT(*) FROM trace_session WHERE expires_at > NOW() OR expires_at IS NULL`).Scan(&activeCount)
				if err2 == nil && activeCount < limits.MaxSessions {
					goto create
				}
			}
			return nil, fmt.Errorf("достигнут лимит активных сессий (%d). Увеличьте лимит, получив Pro-лицензию: https://ais-platform.dev/revizor", limits.MaxSessions)
		}
	}

create:
	sessionID := uuid.New().String()
	expiresAt := time.Now().UTC().Add(time.Duration(s.ttlHours) * time.Hour)

	var desc *string
	if description != "" {
		desc = &description
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO trace_session (session_id, owner, description, created_at, expires_at)
		VALUES ($1, $2, $3, NOW(), $4)
	`, sessionID, owner, desc, expiresAt)
	if err != nil {
		return nil, fmt.Errorf("create_session: %w", err)
	}

	createdAt := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	expAtStr := expiresAt.Format("2006-01-02T15:04:05Z")
	return &Session{
		SessionID:   sessionID,
		Owner:       owner,
		Description: desc,
		CreatedAt:   createdAt,
		ExpiresAt:   &expAtStr,
	}, nil
}

// GetActiveSessions возвращает список активных сессий.
func (s *PostgresStore) GetActiveSessions() ([]Session, error) {
	rows, err := s.pool.Query(context.Background(), `
		SELECT session_id, owner, description, created_at::text, expires_at::text
		FROM trace_session
		WHERE expires_at > NOW() OR expires_at IS NULL
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
func (s *PostgresStore) ExpireSession(sessionID string) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx, `UPDATE trace_config SET enabled = 0 WHERE owner = $1 AND enabled = 1`, sessionID)
	if err != nil {
		return fmt.Errorf("expire_session disable: %w", err)
	}
	_, err = s.pool.Exec(ctx, `UPDATE trace_session SET expires_at = NOW() WHERE session_id = $1`, sessionID)
	if err != nil {
		return fmt.Errorf("expire_session update: %w", err)
	}
	return nil
}

// ExpireOutdatedSessions завершает все истекшие сессии.
func (s *PostgresStore) ExpireOutdatedSessions() (int, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx,
		`SELECT session_id FROM trace_session WHERE expires_at <= NOW() AND expires_at IS NOT NULL`)
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
func (s *PostgresStore) WriteTrace(entry TraceEntry) error {
	ctx := context.Background()
	isSunday := license.CheckLocalTime().IsSunday
	limits := license.EffectiveLimits(s.lic, isSunday)

	if limits.MaxEventsPerDay > 0 {
		var todayCount int
		err := s.pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM trace_log WHERE created_at::date = CURRENT_DATE`).Scan(&todayCount)
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

	_, err = s.pool.Exec(ctx, `
		INSERT INTO trace_log (trace_path, data, session_id, request_id, orchestrator_meta)
		VALUES ($1, $2, $3, $4, $5)
	`, entry.TracePath, string(dataJSON), entry.SessionID, entry.RequestID, orchMeta)
	if err != nil {
		return fmt.Errorf("write_trace: %w", err)
	}
	return nil
}

// WriteTraceBatch записывает несколько trace-событий в одной транзакции.
func (s *PostgresStore) WriteTraceBatch(entries []TraceEntry) error {
	ctx := context.Background()
	isSunday := license.CheckLocalTime().IsSunday
	limits := license.EffectiveLimits(s.lic, isSunday)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("write_batch begin: %w", err)
	}
	defer tx.Rollback(ctx)

	if limits.MaxEventsPerDay > 0 {
		var todayCount int
		err := tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM trace_log WHERE created_at::date = CURRENT_DATE`).Scan(&todayCount)
		if err != nil {
			return fmt.Errorf("write_batch count: %w", err)
		}
		if todayCount >= limits.MaxEventsPerDay {
			return fmt.Errorf("достигнут дневной лимит событий (%d). Получите Pro-лицензию: https://ais-platform.dev/revizor", limits.MaxEventsPerDay)
		}
	}

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
		if _, err := tx.Exec(ctx,
			`INSERT INTO trace_log (trace_path, data, session_id, request_id, orchestrator_meta) VALUES ($1, $2, $3, $4, $5)`,
			entry.TracePath, string(dataJSON), entry.SessionID, entry.RequestID, orchMeta); err != nil {
			return fmt.Errorf("write_batch exec: %w", err)
		}
	}

	return tx.Commit(ctx)
}

// ReadTraceLog читает записи лога.
func (s *PostgresStore) ReadTraceLog(sessionID *string, offset int, limit int, pathFilter *string, since, until *time.Time) ([]TraceEntry, error) {
	ctx := context.Background()
	query := `SELECT id, trace_path, data, session_id, request_id, created_at::text, orchestrator_meta FROM trace_log WHERE 1=1`
	var args []any
	argIdx := 1

	if sessionID != nil {
		query += fmt.Sprintf(` AND session_id = $%d`, argIdx)
		args = append(args, *sessionID)
		argIdx++
	}
	if pathFilter != nil {
		query += fmt.Sprintf(` AND trace_path LIKE $%d`, argIdx)
		args = append(args, strings.ReplaceAll(*pathFilter, "*", "%"))
		argIdx++
	}
	if since != nil {
		query += fmt.Sprintf(` AND created_at >= $%d`, argIdx)
		args = append(args, *since)
		argIdx++
	}
	if until != nil {
		query += fmt.Sprintf(` AND created_at <= $%d`, argIdx)
		args = append(args, *until)
		argIdx++
	}

	query += fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := s.pool.Query(ctx, query, args...)
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
func (s *PostgresStore) SearchTraceLog(search string, sessionID *string, pathFilter *string, offset int, limit int, since, until *time.Time) ([]TraceEntry, error) {
	return s.SearchTraceLogWithContext(search, sessionID, pathFilter, nil, offset, limit, since, until, 0)
}

// buildSearchQueryPG строит WHERE-условия для поиска в PostgreSQL (с $N placeholders).
func (s *PostgresStore) buildSearchQueryPG(search string, sessionID *string, pathFilter *string, dataFilter *string) (string, []any, int) {
	var query strings.Builder
	var args []any
	argIdx := 1

	if search != "" {
		query.WriteString(fmt.Sprintf(` WHERE (data ILIKE $%d OR trace_path ILIKE $%d)`, argIdx, argIdx+1))
		searchPattern := "%" + search + "%"
		args = append(args, searchPattern, searchPattern)
		argIdx += 2
	} else {
		query.WriteString(` WHERE 1=1`)
	}

	if sessionID != nil {
		query.WriteString(fmt.Sprintf(` AND session_id = $%d`, argIdx))
		args = append(args, *sessionID)
		argIdx++
	}
	if pathFilter != nil {
		query.WriteString(fmt.Sprintf(` AND trace_path LIKE $%d`, argIdx))
		args = append(args, strings.ReplaceAll(*pathFilter, "*", "%"))
		argIdx++
	}
	// dataFilter: "key=value" — точное совпадение, "key" — проверка существования
	if dataFilter != nil && *dataFilter != "" {
		if idx := strings.IndexByte(*dataFilter, '='); idx >= 0 {
			key := (*dataFilter)[:idx]
			value := (*dataFilter)[idx+1:]
			query.WriteString(fmt.Sprintf(` AND data->>$%d = $%d`, argIdx, argIdx+1))
			args = append(args, key, value)
			argIdx += 2
		} else {
			query.WriteString(fmt.Sprintf(` AND data->>$%d IS NOT NULL`, argIdx))
			args = append(args, *dataFilter)
			argIdx++
		}
	}

	return query.String(), args, argIdx
}

// SearchTraceLogWithContext ищет записи с опциональным контекстом.
func (s *PostgresStore) SearchTraceLogWithContext(search string, sessionID *string, pathFilter *string, dataFilter *string, offset int, limit int, since, until *time.Time, contextLines int) ([]TraceEntry, error) {
	ctx := context.Background()
	baseQuery, baseArgs, nextIdx := s.buildSearchQueryPG(search, sessionID, pathFilter, dataFilter)

	// Добавляем since/until
	timeClause := ""
	var timeArgs []any
	if since != nil {
		timeClause += fmt.Sprintf(` AND created_at >= $%d`, nextIdx)
		timeArgs = append(timeArgs, *since)
		nextIdx++
	}
	if until != nil {
		timeClause += fmt.Sprintf(` AND created_at <= $%d`, nextIdx)
		timeArgs = append(timeArgs, *until)
		nextIdx++
	}

	allArgs := append([]any{}, baseArgs...)
	allArgs = append(allArgs, timeArgs...)
	allArgs = append(allArgs, limit, offset)

	query := `SELECT id, trace_path, data, session_id, request_id, created_at::text, orchestrator_meta FROM trace_log`
	query += baseQuery + timeClause
	query += fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, nextIdx, nextIdx+1)

	rows, err := s.pool.Query(ctx, query, allArgs...)
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

	// Контекстные строки — выборка соседних ID
	return s.expandContextPG(ctx, matched, sessionID, pathFilter, dataFilter, since, until, contextLines, baseQuery, timeClause, baseArgs, timeArgs, nextIdx-2)
}

// expandContextPG добавляет контекстные строки вокруг найденных событий.
func (s *PostgresStore) expandContextPG(ctx context.Context, matched []TraceEntry, sessionID *string, pathFilter *string, dataFilter *string, since, until *time.Time, contextLines int, baseQuery string, timeClause string, baseArgs []any, timeArgs []any, baseNextIdx int) ([]TraceEntry, error) {
	seen := make(map[int64]bool, len(matched)*(1+2*contextLines))
	var result []TraceEntry

	for _, e := range matched {
		idMin := e.ID - int64(contextLines)
		idMax := e.ID + int64(contextLines)

		nextIdx := baseNextIdx + 1
		ctxQuery := `SELECT id, trace_path, data, session_id, request_id, created_at::text, orchestrator_meta FROM trace_log`
		ctxQuery += baseQuery
		ctxQuery += fmt.Sprintf(` AND id >= $%d AND id <= $%d`, nextIdx, nextIdx+1) + timeClause
		ctxQuery += ` ORDER BY created_at ASC`
		ctxAllArgs := append([]any{}, baseArgs...)
		ctxAllArgs = append(ctxAllArgs, idMin, idMax)
		ctxAllArgs = append(ctxAllArgs, timeArgs...)

		ctxRows, err := s.pool.Query(ctx, ctxQuery, ctxAllArgs...)
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

// CountByPath возвращает агрегацию количества событий по trace_path.
func (s *PostgresStore) CountByPath(search string, dataFilter *string, pathFilter *string, sessionID *string, since, until *time.Time) (map[string]int64, error) {
	ctx := context.Background()
	baseQuery, baseArgs, nextIdx := s.buildSearchQueryPG(search, sessionID, pathFilter, dataFilter)

	timeArgs := []any{}
	timeClause := ""
	if since != nil {
		timeClause += fmt.Sprintf(` AND created_at >= $%d`, nextIdx)
		timeArgs = append(timeArgs, *since)
		nextIdx++
	}
	if until != nil {
		timeClause += fmt.Sprintf(` AND created_at <= $%d`, nextIdx)
		timeArgs = append(timeArgs, *until)
		nextIdx++
	}

	query := `SELECT trace_path, COUNT(*) as cnt FROM trace_log`
	query += baseQuery + timeClause
	query += ` GROUP BY trace_path ORDER BY trace_path`
	allArgs := append(baseArgs, timeArgs...)

	rows, err := s.pool.Query(ctx, query, allArgs...)
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
func (s *PostgresStore) GetTraceStats(sessionID *string) (*TraceStats, error) {
	ctx := context.Background()
	stats := &TraceStats{SessionID: sessionID}

	whereClause := ""
	var args []any
	argIdx := 1
	if sessionID != nil {
		whereClause = fmt.Sprintf(` WHERE session_id = $%d`, argIdx)
		args = append(args, *sessionID)
		argIdx++
	}

	// Total events
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM trace_log`+whereClause, args...).Scan(&stats.TotalEvents)
	if err != nil {
		return nil, fmt.Errorf("stats count: %w", err)
	}

	// Unique paths
	err = s.pool.QueryRow(ctx, `SELECT COUNT(DISTINCT trace_path) FROM trace_log`+whereClause, args...).Scan(&stats.UniquePaths)
	if err != nil {
		return nil, fmt.Errorf("stats unique: %w", err)
	}

	// Last event
	var lastAt *string
	err = s.pool.QueryRow(ctx, `SELECT created_at::text FROM trace_log`+whereClause+` ORDER BY created_at DESC LIMIT 1`, args...).Scan(&lastAt)
	if err != nil && !strings.Contains(err.Error(), "no rows") {
		return nil, fmt.Errorf("stats last: %w", err)
	}
	if lastAt != nil {
		stats.LastEventAt = lastAt
	}

	return stats, nil
}

// GetDistinctPaths возвращает все уникальные trace-пути (из лога + зарегистрированные).
func (s *PostgresStore) GetDistinctPaths(sessionID *string, modulePrefix *string) ([]string, error) {
	ctx := context.Background()
	query := `SELECT trace_path FROM (
		SELECT DISTINCT trace_path FROM trace_log WHERE 1=1`
	var args []any
	argIdx := 1

	if sessionID != nil && *sessionID != "" {
		query += fmt.Sprintf(` AND session_id = $%d`, argIdx)
		args = append(args, *sessionID)
		argIdx++
	}
	if modulePrefix != nil && *modulePrefix != "" {
		query += fmt.Sprintf(` AND trace_path LIKE $%d`, argIdx)
		args = append(args, *modulePrefix+".%")
		argIdx++
	}

	query += ` UNION SELECT DISTINCT trace_path FROM trace_config WHERE 1=1`

	if sessionID != nil && *sessionID != "" {
		query += fmt.Sprintf(` AND owner = $%d`, argIdx)
		args = append(args, *sessionID)
		argIdx++
	}
	if modulePrefix != nil && *modulePrefix != "" {
		query += fmt.Sprintf(` AND trace_path LIKE $%d`, argIdx)
		args = append(args, *modulePrefix+".%")
		argIdx++
	}

	query += `) ORDER BY trace_path`

	rows, err := s.pool.Query(ctx, query, args...)
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
func (s *PostgresStore) GetPathFrequency(sessionID *string, path string, hours int) ([]FrequencyBucket, error) {
	ctx := context.Background()
	query := `
		SELECT TO_CHAR(created_at, 'YYYY-MM-DD"T"HH24:MI:00') AS bucket, COUNT(*) AS cnt
		FROM trace_log
		WHERE trace_path = $1
		  AND created_at >= NOW() - ($2 || ' hours')::INTERVAL
	`
	args := []any{path, fmt.Sprintf("%d", hours)}
	argIdx := 3

	if sessionID != nil && *sessionID != "" {
		query += fmt.Sprintf(` AND session_id = $%d`, argIdx)
		args = append(args, *sessionID)
	}

	query += ` GROUP BY bucket ORDER BY bucket`

	rows, err := s.pool.Query(ctx, query, args...)
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
func (s *PostgresStore) GetRequestChain(requestID string, sessionID *string) ([]TraceEntry, error) {
	ctx := context.Background()
	query := `SELECT id, trace_path, data, session_id, request_id, created_at::text, orchestrator_meta FROM trace_log WHERE request_id = $1`
	args := []any{requestID}
	argIdx := 2

	if sessionID != nil && *sessionID != "" {
		query += fmt.Sprintf(` AND session_id = $%d`, argIdx)
		args = append(args, *sessionID)
	}

	query += ` ORDER BY created_at ASC`

	rows, err := s.pool.Query(ctx, query, args...)
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
func (s *PostgresStore) GetEnterChains(sessionID *string, modulePrefix *string) ([]EnterChain, error) {
	ctx := context.Background()
	query := `
		SELECT DISTINCT
			REPLACE(e.trace_path, '.enter', '') AS base_path,
			e.trace_path AS enter_path
		FROM trace_log e
		WHERE e.trace_path LIKE '%.enter'
		  AND e.request_id IS NOT NULL
	`
	var args []any
	argIdx := 1

	if sessionID != nil && *sessionID != "" {
		query += fmt.Sprintf(` AND e.session_id = $%d`, argIdx)
		args = append(args, *sessionID)
		argIdx++
	}
	if modulePrefix != nil && *modulePrefix != "" {
		query += fmt.Sprintf(` AND e.trace_path LIKE $%d`, argIdx)
		args = append(args, *modulePrefix+".%")
		argIdx++
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

	rows, err := s.pool.Query(ctx, query, args...)
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

// SeedPresets заполняет таблицу trace_preset из YAML-конфига (ON CONFLICT DO NOTHING).
func (s *PostgresStore) SeedPresets(presets map[string]config.PresetConfig) error {
	ctx := context.Background()
	for name, preset := range presets {
		pathsJSON, err := json.Marshal(preset.Paths)
		if err != nil {
			return fmt.Errorf("seed_presets marshal %s: %w", name, err)
		}
		desc := preset.Description
		if desc == "" {
			desc = name
		}
		_, err = s.pool.Exec(ctx, `
			INSERT INTO trace_preset (name, description, paths)
			VALUES ($1, $2, $3)
			ON CONFLICT(name) DO NOTHING
		`, name, desc, string(pathsJSON))
		if err != nil {
			return fmt.Errorf("seed_presets insert %s: %w", name, err)
		}
	}
	return nil
}

// GetPresets возвращает все пресеты из БД.
func (s *PostgresStore) GetPresets() ([]Preset, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx, `SELECT name, description, paths FROM trace_preset ORDER BY name`)
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
func (s *PostgresStore) SetPreset(name, description string, paths []string) error {
	ctx := context.Background()
	pathsJSON, err := json.Marshal(paths)
	if err != nil {
		return fmt.Errorf("set_preset marshal: %w", err)
	}
	if description == "" {
		description = name
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO trace_preset (name, description, paths, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT(name) DO UPDATE SET
			description = EXCLUDED.description,
			paths = EXCLUDED.paths,
			updated_at = NOW()
	`, name, description, string(pathsJSON))
	if err != nil {
		return fmt.Errorf("set_preset: %w", err)
	}
	return nil
}

// DeletePreset удаляет пресет из БД.
func (s *PostgresStore) DeletePreset(name string) error {
	_, err := s.pool.Exec(context.Background(), `DELETE FROM trace_preset WHERE name = $1`, name)
	if err != nil {
		return fmt.Errorf("delete_preset: %w", err)
	}
	return nil
}

// QueryOrchestratorEvents возвращает события оркестратора по task_id за временное окно.
func (s *PostgresStore) QueryOrchestratorEvents(taskID string, windowSec int) ([]OrchestratorEvent, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx, `
		SELECT created_at::text, trace_path, orchestrator_meta, data
		FROM trace_log
		WHERE orchestrator_meta::jsonb->>'task_id' = $1
		  AND created_at >= NOW() - ($2 || ' seconds')::INTERVAL
		ORDER BY created_at ASC
	`, taskID, fmt.Sprintf("%d", windowSec))
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
func (s *PostgresStore) RegisterPath(path string, sessionID *string) error {
	ctx := context.Background()
	var count int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM trace_config WHERE trace_path = $1 AND (owner = $2 OR (owner IS NULL AND $2 IS NULL))`,
		path, sessionID).Scan(&count)
	if err != nil {
		return fmt.Errorf("register_path check: %w", err)
	}
	if count > 0 {
		return nil
	}

	_, err = s.pool.Exec(ctx,
		`INSERT INTO trace_config (trace_path, enabled, owner) VALUES ($1, 0, $2)`,
		path, sessionID)
	if err != nil {
		return fmt.Errorf("register_path insert: %w", err)
	}
	return nil
}

// WriteAudit записывает запись аудита MCP-операции в БД.
func (s *PostgresStore) WriteAudit(entry AuditEntry) error {
	_, err := s.pool.Exec(context.Background(),
		`INSERT INTO audit_log (tool_name, args, result, error, duration_ms) VALUES ($1, $2, $3, $4, $5)`,
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
