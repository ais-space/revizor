-- Миграция 001: начальная схема БД Ревизора
-- Совместима с SQLite (эталонная схема из revizor_core_0_1_0)

CREATE TABLE IF NOT EXISTS trace_config (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    trace_path TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 0,
    output_file TEXT,
    sample_rate REAL NOT NULL DEFAULT 1.0,
    owner TEXT,
    description TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(trace_path, owner)
);

CREATE TABLE IF NOT EXISTS trace_session (
    session_id TEXT PRIMARY KEY,
    owner TEXT NOT NULL,
    description TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at TEXT
);

CREATE TABLE IF NOT EXISTS trace_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    trace_path TEXT NOT NULL,
    data TEXT,
    session_id TEXT,
    request_id TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Индексы
CREATE INDEX IF NOT EXISTS idx_trace_config_path ON trace_config(trace_path);
CREATE INDEX IF NOT EXISTS idx_trace_config_owner ON trace_config(owner);
CREATE INDEX IF NOT EXISTS idx_trace_config_enabled ON trace_config(enabled);
CREATE INDEX IF NOT EXISTS idx_trace_log_session ON trace_log(session_id);
CREATE INDEX IF NOT EXISTS idx_trace_log_request ON trace_log(request_id);
CREATE INDEX IF NOT EXISTS idx_trace_log_path ON trace_log(trace_path);
CREATE INDEX IF NOT EXISTS idx_trace_log_created ON trace_log(created_at);

-- Полнотекстовый поиск (FTS5)
CREATE VIRTUAL TABLE IF NOT EXISTS trace_log_fts USING fts5(
    trace_path,
    data,
    content='trace_log',
    content_rowid='id'
);

-- Пресеты (гибрид БД+YAML: YAML seed при первом запуске, БД — источник правды)
CREATE TABLE IF NOT EXISTS trace_preset (
    name TEXT PRIMARY KEY,
    description TEXT NOT NULL DEFAULT '',
    paths TEXT NOT NULL DEFAULT '[]',  -- JSON-массив строк
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Триггеры синхронизации FTS
CREATE TRIGGER IF NOT EXISTS trace_log_ai AFTER INSERT ON trace_log BEGIN
    INSERT INTO trace_log_fts(rowid, trace_path, data)
    VALUES (new.id, new.trace_path, new.data);
END;

CREATE TRIGGER IF NOT EXISTS trace_log_ad AFTER DELETE ON trace_log BEGIN
    INSERT INTO trace_log_fts(trace_log_fts, rowid, trace_path, data)
    VALUES ('delete', old.id, old.trace_path, old.data);
END;

CREATE TRIGGER IF NOT EXISTS trace_log_au AFTER UPDATE ON trace_log BEGIN
    INSERT INTO trace_log_fts(trace_log_fts, rowid, trace_path, data)
    VALUES ('delete', old.id, old.trace_path, old.data);
    INSERT INTO trace_log_fts(rowid, trace_path, data)
    VALUES (new.id, new.trace_path, new.data);
END;
