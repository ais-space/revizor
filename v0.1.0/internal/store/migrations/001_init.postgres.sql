-- Миграция 001: начальная схема БД Ревизора (PostgreSQL)
-- Аналог 001_init.sql для PostgreSQL. Использует SERIAL, NOW(), ILIKE.

CREATE TABLE IF NOT EXISTS trace_config (
    id SERIAL PRIMARY KEY,
    trace_path TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 0,
    output_file TEXT,
    sample_rate REAL NOT NULL DEFAULT 1.0,
    owner TEXT,
    description TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE(trace_path, owner)
);

CREATE TABLE IF NOT EXISTS trace_session (
    session_id TEXT PRIMARY KEY,
    owner TEXT NOT NULL,
    description TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMP WITH TIME ZONE
);

CREATE TABLE IF NOT EXISTS trace_log (
    id BIGSERIAL PRIMARY KEY,
    trace_path TEXT NOT NULL,
    data TEXT,
    session_id TEXT,
    request_id TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- Индексы
CREATE INDEX IF NOT EXISTS idx_trace_config_path ON trace_config(trace_path);
CREATE INDEX IF NOT EXISTS idx_trace_config_owner ON trace_config(owner);
CREATE INDEX IF NOT EXISTS idx_trace_config_enabled ON trace_config(enabled);
CREATE INDEX IF NOT EXISTS idx_trace_log_session ON trace_log(session_id);
CREATE INDEX IF NOT EXISTS idx_trace_log_request ON trace_log(request_id);
CREATE INDEX IF NOT EXISTS idx_trace_log_path ON trace_log(trace_path);
CREATE INDEX IF NOT EXISTS idx_trace_log_created ON trace_log(created_at);

-- Пресеты (гибрид БД+YAML: YAML seed при первом запуске, БД — источник правды)
CREATE TABLE IF NOT EXISTS trace_preset (
    name TEXT PRIMARY KEY,
    description TEXT NOT NULL DEFAULT '',
    paths TEXT NOT NULL DEFAULT '[]',  -- JSON-массив строк
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- Примечание: FTS5 не используется в PostgreSQL.
-- Поиск реализован через ILIKE в postgres.go (SearchTraceLog).
