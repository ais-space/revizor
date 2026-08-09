# Revizor Go-ядро (ais_products/revizor)

**Статус:** Pre-Deploy — v58 (max_ver + PostgresStore + версионирование)
**Версия:** 0.1.0
**Язык:** Go 1.21+
**Дата создания:** 2026-05-07
**Дата обновления:** 2026-07-02 (v58: VERSION-001 (max_ver), POSTGRES-001 (PostgresStore pgx/v5), версионирование сборок, авто-проверка обновлений, заморозка версий distrib/, селектор версий)
**Автор:** AIS Platform Team

## Назначение

Go-реализация ядра Ревизора — standalone-бинарник `revizor serve` для сбора trace-событий. Разработчик запускает бинарник одной командой и получает HTTP API для отладочного логирования. AI-агенты управляют трассировкой через MCP-интерфейс. Полный список инструментов: [DEV_README.md](DEV_README.md).

## Архитектура

```
ais_products/revizor/
├── cmd/
│   ├── revizor/main.go              # Точка входа, лицензия, heartbeat, uninstall
│   └── installer/main.go            # Windows-инсталлятор (GOOS=windows, embed)
├── internal/
│   ├── config/config.go             # Загрузка revizor.yaml + env
│   ├── trace/
│   │   ├── matcher.go               # compile_config + should_trace + glob_match
│   │   └── sanitize.go              # deep_sanitize (двухслойная)
│   ├── store/
│   │   ├── store.go                 # TraceStore interface + доменные типы
│   │   ├── sqlite.go                # SQLite-реализация (+лимиты, +orchestrator)
│   │   └── migrations/
│   │       ├── 001_init.sql         # Базовая схема
│   │       └── 002_orchestrator_meta.sql  # orchestrator_meta
│   ├── server/
│   │   ├── server.go                # HTTP API (9 эндпоинтов) + batch-воркер
│   │   └── middleware.go            # API-key auth, rate limiting
│   ├── mcp/
│   │   ├── mcp.go                   # JSON-RPC 2.0 типы
│   │   ├── handler.go               # MCPHandler + лицензионные ограничения
│   │   ├── register.go              # toolAdapter (tools → mcp.ToolHandler)
│   │   ├── stdio.go                 # stdio-режим (stdin → stdout)
│   │   ├── sunday_check.go          # SundayChecker с кэшем (ALERT-002, v56)
│   │   └── tools/                   # 34 MCP-инструмента
│   ├── license/
│   │   ├── license.go               # ParseKey v3, Validate, ErrLicenseRevoked
│   │   ├── features.go              # AllowTool, IsMCPBasic/mcp_full/postgres
│   │   ├── sunday.go                # CheckSundayUnlimited, fetchServerTime (v56)
│   │   ├── time_check.go            # 3 источника времени, EvaluateTimeConsensus
│   │   ├── machine_id.go            # SHA256(machine-id + hostname + MAC)
│   │   └── heartbeat.go             # HeartbeatClient + SendActivation/SendAlert
│   ├── cmd/uninstall.go             # Логика деинсталляции (v55)
│   └── webhook/                     # Webhook-уведомления
├── sdk/
│   ├── python/                      # Python SDK (stdlib, нуль зависимостей)
│   │   └── revizor_sdk/
│   └── typescript/                  # TypeScript SDK (Web API, нуль зависимостей)
├── revizor.yaml.example
├── go.mod
├── Makefile
└── README.md
```

## Ключевые алгоритмы

- **Матчинг:** точный порт Python `_glob_match` — сегменты через `.`, `**` greedy, `*` = 1 сегмент. Приоритет: exclude > exact > glob.
- **Санитизация:** двухслойная, идентичная Python-эталону (19 ключей + 2 префикса + 6 regex + truncate на глубине 5).
- **API:** 9 HTTP-эндпоинтов (включая batch). Открытый `/log` и `/log/batch`, защищённые config/session/stats/mcp (API-key).
- **Batching:** буфер 50 записей / 100 мс flush на сервере + batch-эндпоинт для SDK.
- **Лицензирование:** Ed25519+zstd, Community/Pro-режимы, heartbeat 24ч (grace 72ч), привязка к машине.

## Зависимости

| Пакет | Назначение |
|-------|-----------|
| `modernc.org/sqlite` | Чистый Go SQLite (без CGO) |
| `gopkg.in/yaml.v3` | Парсинг revizor.yaml |
| `github.com/google/uuid` | Генерация UUID для сессий |
| `github.com/klauspost/compress` | zstd-сжатие лицензионного ключа (чистый Go) |

## Сборка и запуск

```bash
go build -o revizor ./cmd/revizor/
./revizor serve
```

## Тесты

```bash
go test ./internal/... -v -count=1
```

## MCP-инструменты (эталонный список: [DEV_README.md](DEV_README.md))

### Группа 0: Диагностика (1)
`trace_ping` — тест MCP-доступа (pong), статус лицензии

### Группа 1: Управление сессией (5)
`trace_start`, `trace_enable`, `trace_disable`, `trace_expire`, `trace_kill`

### Группа 2: Чтение логов (4)
`trace_read`, `trace_search`, `trace_config`, `trace_tail`

### Группа 3: Статистика и справочная (6)
`trace_validate_path`, `trace_why`, `trace_list_sessions`, `trace_presets_list`, `trace_preset_set`, `trace_stats`

### Группа 4: Анализ (4)
`trace_list_points`, `trace_verify_coverage`, `trace_diff`, `trace_suggest_coverage`

### Группа 5: Генерация (2)
`trace_cost`, `trace_generate_test`

### Группа 6: Ревизоризация кода (4)
`trace_inject`, `trace_targets`, `trace_audit`, `trace_remove`

### Группа 7: Оркестраторы (2)
`trace_session_summary`, `trace_orchestrator_events`

### Группа 8: Управление процессом и лицензией (2)
`trace_shutdown`, `trace_license_renew`

### Внутренний диагностический инструмент (не публикуется)
`trace_debug_log` — запись MCP-запросов/ответов в отладочный файл

Все инструменты — на чистом Go, без внешних зависимостей. Поддерживаются через MCP stdio (`./revizor --mcp`) и HTTP API (`POST /mcp`). Community-режим: базовый набор (trace_start, trace_read, trace_expire, trace_validate_path, trace_presets_list и др.).

**Адаптация под проект:** `trace_targets` и `trace_audit` по умолчанию сканируют `modules/`. Для проектов с другой структурой (`src/`, `lib/`, `packages/`) — секция `project:` в `revizor.yaml` позволяет задать `source_root`, кастомные формы импорта и исключаемые директории.

## SDK (Фаза 4)

- **Python:** `sdk/python/revizor_sdk/` — stdlib-only, fire-and-forget, batching, санитизация
- **TypeScript:** `sdk/typescript/` — Web API only, `@ais-platform/revizor-sdk`, sendBeacon при beforeunload

## История изменений

- **2026-06-14 (v45):** 33 инструмента (32 публичных + 1 внутренний). REV-008 (data_filter), REV-009 (count_by), REV-010 (context_lines), REV-011 (ping-first в документации). REV-W-001: webhook-уведомления — `trace_webhook_list`, `trace_webhook_test`. Исправлены: defer-утечка в heartbeat.go, globMatch `**` в matcher.go, race condition в WriteTraceBatch, webhook-конфиг в yaml.example. Удалён мёртвый пакет integrations/webhook.go.
   136→- **2026-06-12 (v43):** Актуализация — 31 инструмент (30 публичных + 1 внутренний). README.md и README_RU.md переработаны для AI-агентов: стартовый протокол, 6 рецептов, методики, лучшие практики. `trace_debug_log` вынесен из общей таблицы как внутренний диагностический инструмент.
- **2026-05-21:** `trace_shutdown` — graceful остановка процесса через все 4 интерфейса (HTTP, MCP, CLI, prompt). `install.sh` — копирование README.md при установке.
- **2026-05-13:** `trace_kill` — принудительное завершение всех активных сессий. `trace_inject` для TypeScript (regex).
- **2026-05-08:** Фаза 4 — Python и TypeScript SDK. Batch-эндпоинт `/log/batch`. Исправлен `WriteTraceBatch` (лимиты).
- **2026-05-08:** Фаза 2.6 + Фаза 3 — оркестраторы (2 инструмента), лицензирование (Ed25519+zstd), Community/Pro-режимы, heartbeat.
- **2026-05-07:** Фаза 1 — первый работающий бинарник. HTTP API, SQLite (CGO-free), матчинг, санитизация, batching.
