-- Миграция: таблица аудита MCP-операций (PostgreSQL).
-- Аналог 003_audit_log.sql для PostgreSQL.
-- Хранит историю вызовов инструментов: кто, когда, какой инструмент, параметры, результат, ошибки.

CREATE TABLE IF NOT EXISTS audit_log (
    id          BIGSERIAL PRIMARY KEY,
    tool_name   TEXT    NOT NULL,                -- имя инструмента (trace_start, trace_validate_path, ...)
    args        TEXT    DEFAULT '',              -- JSON-строка аргументов
    result      TEXT    DEFAULT '',              -- результат (обрезанный до 500 символов)
    error       TEXT    DEFAULT '',              -- ошибка (пустая строка если успех)
    duration_ms INTEGER NOT NULL DEFAULT 0,      -- длительность выполнения в мс
    created_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audit_log_tool ON audit_log(tool_name);
CREATE INDEX IF NOT EXISTS idx_audit_log_created ON audit_log(created_at);
