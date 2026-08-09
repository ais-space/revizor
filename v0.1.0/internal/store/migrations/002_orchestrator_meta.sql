-- Миграция 002: добавление поля orchestrator_meta для интеграции с оркестраторами
-- Используется Прорабом (Foreman) и другими AI-оркестраторами для пассивных крючков

-- Колонка с метаданными оркестратора (JSON)
ALTER TABLE trace_log ADD COLUMN orchestrator_meta TEXT;

-- Индекс для поиска по task_id (ключевой фильтр в trace_orchestrator_events)
CREATE INDEX IF NOT EXISTS idx_trace_log_orchestrator ON trace_log((json_extract(orchestrator_meta, '$.task_id')));
