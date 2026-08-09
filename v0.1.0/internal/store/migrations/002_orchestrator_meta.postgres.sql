-- Миграция 002: добавление поля orchestrator_meta для интеграции с оркестраторами (PostgreSQL)
-- Аналог 002_orchestrator_meta.sql для PostgreSQL.

-- Колонка с метаданными оркестратора (JSON)
ALTER TABLE trace_log ADD COLUMN IF NOT EXISTS orchestrator_meta TEXT;

-- Индекс для поиска по task_id (ключевой фильтр в trace_orchestrator_events)
CREATE INDEX IF NOT EXISTS idx_trace_log_orchestrator ON trace_log((orchestrator_meta::jsonb->>'task_id'));
