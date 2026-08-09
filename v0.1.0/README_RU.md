# Revizor — отладка без `print()` и `console.log()`

**Русскоязычная версия.** Оригинал: [README.md](README.md). Для разработчиков Ревизора — см. [DEV_README.md](DEV_README.md).

## Что такое Ревизор

Вы тратите часы на отладку. Ставите `print()`, перезапускаете сервер, забываете убрать — код замусорен.

Ревизор делает иначе. Вы запускаете один бинарник. Ваш AI-агент читает trace-логи и находит баг. Вы смотрите результат.

**Ревизор — это AI-управляемое отладочное логирование.** Вы даёте AI-агенту задачу. Агент читает поток событий и объясняет, что произошло.

## Как выглядит работа с Ревизором (60 секунд)

У вас не работает OAuth-вход через Google. Пользователь нажимает «Sign In», проходит редирект — и снова неавторизован.

```bash
# 1. Ревизор уже запущен (revizor serve)
# 2. AI-агент читает логи — БЕЗ создания сессии, БЕЗ перезапуска серверов:
curl -s -X POST http://127.0.0.1:9876/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"trace_search","arguments":{"search":"auth.","lines":50}}}'
```

```
[
  {"trace_path": "auth.callback.enter", "data": {"provider": "google"}},
  {"trace_path": "auth.callback.fetch_profile", "data": null},
  {"trace_path": "auth.callback.decision", "data": {"existing_link": null}},
  {"trace_path": "auth.create_identity.enter", "data": {"user_id": 42}},
  {"trace_path": "auth.create_session.success", "data": null},
  {"trace_path": "auth.redirect.success", "data": null},
  ❌ Нет auth.frontend.token_saved  ← вот где обрыв!
]
```

AI-агент сообщает: «Токен передан в URL, но фронтенд не сохранил его в localStorage. Смотри `auth/success/page.tsx:51`.» Баг найден за минуту.

---

## AI-агенту

> Полная инструкция по отладке вкомпилирована в бинарник. Начните с `trace_ping` для проверки соединения, затем вызовите **`trace_get_instructions`** — получите полное руководство: стартовый протокол, рецепты, паттерны и лучшие практики. Доступ к файлам на диске не требуется.

---

## Установка и запуск

### Установка из дистрибутива

```bash
cd ais_revizor && sudo ./install.sh
# Устанавливает бинарник в /usr/local/bin/ais_tools/revizor (symlink /usr/local/bin/revizor)
# Конфиг — в ~/.config/ais_tools/revizor/
```

### Запуск

```bash
# Из корня проекта (важно для trace_inject/trace_audit — они сканируют модули)
cd /path/to/your/project
revizor serve

# Или явно:
revizor serve
```

Бинарник стартует мгновенно. HTTP-сервер на порту 9876. БД `revizor.db` создаётся в `~/.config/ais_tools/revizor/` (или в CWD если конфигурационная директория недоступна).

### Остановка

```bash
# Graceful (flush буферов, закрытие БД):
curl -s -X POST http://127.0.0.1:9876/api/v1/trace/shutdown

# Или Ctrl+C в терминале где запущен revizor serve
```

### Ревизор с Docker

Если ваше приложение работает в Docker-контейнере, а Ревизор — на хосте:

1. **Настройте прослушивание на всех интерфейсах.** В `revizor.yaml`:
   ```yaml
   server:
     host: "0.0.0.0"
     port: 9876
   ```
   По умолчанию `host: "127.0.0.1"` — контейнер не сможет подключиться.

2. **Передайте URL в контейнер.** В `docker-compose.yml`:
   ```yaml
   services:
     backend:
       environment:
         REVIZOR_URL: http://host.docker.internal:9876
   ```
   `host.docker.internal` — специальный DNS, резолвящийся в хост-машину.

3. **Создайте сессию трассировки** (один раз при старте):
   ```bash
   curl -s -X POST http://127.0.0.1:9876/mcp \
     -H 'Content-Type: application/json' \
     -d '{"jsonrpc":"2.0","id":1,"method":"tools/call",
          "params":{"name":"trace_start",
                    "arguments":{"paths":"*","description":"app debugging"}}}'
   ```
   Сохраните `session_id` из ответа и передайте в контейнер через `REVIZOR_SESSION_ID`.

4. **SDK автоматически передаёт session_id** через HTTP-заголовок `X-Revizor-Session`. Убедитесь что у вас актуальная версия SDK (v0.1.1+).

---

## Четыре способа работы

Ревизор предоставляет четыре интерфейса — выберите подходящий под вашу модель AI-агента.

| Способ | Для кого | Как запустить |
|--------|----------|--------------|
| **MCP напрямую** | AI-агенты с поддержкой MCP (Claude Code, Gemini, Cursor) | Подключить бинарник как MCP-сервер |
| **CLI-обёртка** | AI-агенты БЕЗ tool_calls (DeepSeek, локальные модели) | `python3 mcp_client_cli_0_1_0.py --server './revizor --mcp'` |
| **HTTP JSON-RPC** | Скрипты, curl, CI/CD, любые HTTP-клиенты | `curl -X POST http://127.0.0.1:9876/mcp` |
| **Prompt-адаптер** | LLM без function calling (текстовый протокол) | `./revizor --mcp` + `mcp_client_prompt_0_1_0` |

### Способ 1: MCP напрямую (Claude Code, Gemini, Cursor)

Добавьте бинарник в конфигурацию вашего MCP-хоста:

```json
{
  "mcpServers": {
    "revizor": {
      "command": "/usr/local/bin/ais_tools/revizor",
      "args": ["--mcp"]
    }
  }
}
```

### Способ 2: CLI-обёртка (DeepSeek, локальные модели)

Для моделей, не поддерживающих прямые MCP-вызовы:

```bash
CLI="python3 modules/mcp_client_cli_0_1_0/mcp_client_cli_0_1_0.py --server './revizor --mcp'"

# Проверка соединения
$CLI --tool trace_ping

# Поиск по логам
$CLI --tool trace_search --args '{"search": "elevation", "lines": 50}'

# Чтение логов
$CLI --tool trace_read --args '{"lines": 100}'

# Завершить сессию
$CLI --tool trace_expire --args '{"session_id": "uuid-сессии"}'
```

> **Примечание:** CLI запускает бинарник в режиме `--mcp` (stdio JSON-RPC). Если бинарник уже запущен как HTTP-сервер (`revizor serve`), используйте способ 3 (HTTP JSON-RPC через curl).

### Способ 3: HTTP JSON-RPC (curl, скрипты, CI/CD)

Бинарник запущен как `revizor serve` на порту 9876. Все инструменты доступны через `POST /mcp` (полный список: [DEV_README.md](DEV_README.md)).

**Ping-first паттерн (REV-011):** Перед любым вызовом к Ревизору проверьте что бинарник жив. Если `trace_ping` не отвечает — остановитесь, бинарник не запущен.

```bash
# ВСЕГДА первым: проверка соединения
PING=$(curl -s -X POST http://127.0.0.1:9876/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"trace_ping","arguments":{}}}')

if echo "$PING" | grep -q '"text.*pong'; then
    echo "Ревизор жив, работаем."
else
    echo "Ревизор НЕ запущен. Запустите: revizor serve" >&2
    exit 1
fi
```

После успешного ping'а продолжайте отладку:

```bash
# Поиск по ключевому слову (БЕЗ сессии)
curl -s -X POST http://127.0.0.1:9876/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"trace_search","arguments":{"search":"elevation","lines":50}}}'

# Поиск с фильтром по пути
curl -s -X POST http://127.0.0.1:9876/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"trace_search","arguments":{"search":"callback","path_filter":"auth.*","lines":30}}}'

# Последние 100 строк лога (БЕЗ сессии)
curl -s -X POST http://127.0.0.1:9876/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"trace_read","arguments":{"lines":100}}}'

# Создать сессию (опционально — только для фильтрации по времени)
curl -s -X POST http://127.0.0.1:9876/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"trace_start","arguments":{"paths":"elevation.**,auth.callback.*","description":"отладка elevation"}}}'

# Прочитать логи сессии
curl -s -X POST http://127.0.0.1:9876/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"trace_read","arguments":{"session_id":"uuid-сессии","lines":100}}}'

# Завершить сессию
curl -s -X POST http://127.0.0.1:9876/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"trace_expire","arguments":{"session_id":"uuid-сессии"}}}'

# Список активных сессий
curl -s -X POST http://127.0.0.1:9876/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"trace_list_sessions","arguments":{}}}'

# Список всех инструментов
curl -s -X POST http://127.0.0.1:9876/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":9,"method":"tools/list","params":{}}'
```

> **Примечание о `trace_start`:** На больших БД (сотни тысяч событий) создание сессии может занять десятки секунд из-за FTS-индексации. HTTP-клиенты с коротким таймаутом могут не дождаться ответа — при этом сессия создаётся в БД. Используйте `trace_search` и `trace_read` без `session_id` для мгновенного доступа к логам.

### Способ 4: Prompt-адаптер (текстовый протокол)

Для LLM, которые не поддерживают ни MCP, ни function calling. Адаптер преобразует инструменты в текстовые инструкции. Подробнее в `mcp_client_prompt_0_1_0`.

---

## Пресеты

Готовые наборы trace-путей для типовых сценариев:

```bash
# Список пресетов
curl -s -X POST http://127.0.0.1:9876/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"trace_presets_list","arguments":{}}}'

# Создать сессию с пресетом
curl -s -X POST http://127.0.0.1:9876/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"trace_start","arguments":{"paths":"debug_elevation","description":"отладка elevation"}}}'
```

---

## Отправка событий в Ревизор (SDK)

Если вы хотите, чтобы ваш код отправлял trace-события в бинарник:

**Python:**
```python
from revizor_sdk import configure, trace
configure(endpoint="http://localhost:9876")
trace("my_module.my_function.enter", {"key": "value"})
```

**TypeScript:**
```typescript
import { initTrace, trace } from '@ais-platform/revizor-sdk';
initTrace({ apiBaseUrl: 'http://localhost:9876' });
trace('my_module.my_function.enter', { key: 'value' });
```

---

## Все инструменты (34 публичных + 1 внутренний = 35 всего)

Полная спецификация: [DEV_README.md](DEV_README.md).

| # | Инструмент | Назначение |
|---|-----------|-------------|
| 1 | `trace_ping` | Тест соединения. Возвращает `"pong"` без побочных эффектов. **Всегда вызывайте первым.** |
| 2 | `trace_get_instructions` | Встроенная в бинарник инструкция для AI-агента: стартовый протокол, рецепты отладки, лучшие практики. **Вызывайте после ping.** |
| 3 | `trace_read` | Последние N строк лога (`session_id` опционален). С `format: "table"` — человекочитаемый вывод. |
| 4 | `trace_search` | Полнотекстовый поиск с опциональными `path_filter`, `data_filter` (REV-008, `key=value` или `key`), `count_by` (REV-009, `path` для агрегации), `context_lines` (REV-010, grep -C). **Основной инструмент отладки.** |
| 5 | `trace_config` | Показать активные trace-точки для сессии. |
| 6 | `trace_tail` | Потоковое чтение: новые события с момента последнего чтения (курсор). |
| 7 | `trace_start` | Создать сессию и включить trace-точки по пресету или списку путей. |
| 8 | `trace_enable` | Включить одну точку. Поддерживает glob-шаблоны (`auth.**`). |
| 9 | `trace_disable` | Выключить одну точку. |
| 10 | `trace_expire` | Принудительно завершить сессию: выключить все точки и установить срок истечения. |
| 11 | `trace_kill` | Завершить одну или все активные сессии с принудительной очисткой. |
| 12 | `trace_validate_path` | Проверить формат пути (lowercase, `[a-z0-9_.]+`, макс 120 символов). **Используйте перед каждым коммитом.** |
| 13 | `trace_why` | Объяснить, почему точка включена или выключена — проверка всех glob-правил. |
| 14 | `trace_list_sessions` | Показать все активные сессии. |
| 15 | `trace_presets_list` | Список доступных пресетов (из БД, fallback на YAML). |
| 16 | `trace_preset_set` | Создать или обновить пресет в БД. Поддерживает `!` для исключения. |
| 17 | `trace_stats` | Метрики сессии: событий, уникальных путей, время последнего события. |
| 18 | `trace_list_points` | Все известные trace-пути в системе, сгруппированные по модулям. |
| 19 | `trace_verify_coverage` | Проверка целостности цепочек: `.enter` → `.success` / `.failed`. |
| 20 | `trace_suggest_coverage` | Сравнение зарегистрированных путей с ожидаемым паттерном. |
| 21 | `trace_diff` | Сравнить две сессии: какие trace-пути различаются. |
| 22 | `trace_inject` | Вставка `trace()` в Python/TypeScript файлы. Поддерживает `dry_run`. |
| 23 | `trace_targets` | Сканирование файловой системы для приоритизации файлов (P1–P5). |
| 24 | `trace_audit` | Аудит модулей на RE-033 (наличие trace-импорта). **Используйте перед каждым коммитом.** |
| 25 | `trace_cost` | Частота вызова точки за период (событий/минуту). |
| 26 | `trace_generate_test` | Генерация pytest-теста из цепочки событий одного `request_id`. |
| 27 | `trace_session_summary` | Сводка по сессии: событий, уникальных путей, время, статус. |
| 28 | `trace_orchestrator_events` | Таймлайн событий для задачи оркестратора (фильтр по `task_id`). |
| 29 | `trace_shutdown` | Graceful остановка процесса (flush буферов, закрытие БД). |
| 30 | `trace_license_renew` | Ручное подтверждение лицензии агентом (заменяет heartbeat). |
| 31 | `trace_remove` | Удаление `trace()` из исходного кода (реверс trace_inject). |
| 32 | `trace_webhook_list` | Список зарегистрированных webhook-уведомлений и статус доставки (REV-W-001). |
| 33 | `trace_webhook_test` | Тестовый ping на конкретный webhook (REV-W-001). |


> **Внутренний диагностический инструмент:** `trace_debug_log` — записывает MCP-запросы/ответы в отладочный лог-файл. Не публикуется в общем списке; используется только для диагностики самого Ревизора.

---

## Webhook-уведомления (REV-W-001)

Ревизор может уведомлять внешние системы о конкретных trace-событиях. Используется для инвалидации устаревших вердиктов в AIS Surveyor.

Настройка в `revizor.yaml`:

```yaml
webhooks:
  - id: "surveyor-invalidation"
    url: "http://localhost:9877/api/v1/revizor-events"
    path_filter: "auth.**,payment.**"
    enabled: true
```

Доставка асинхронная (goroutine), с повторными попытками: 3 попытки с интервалом 5 секунд. Неудачные доставки после всех попыток логируются на уровне CRITICAL.

**MCP-инструменты:**
- `trace_webhook_list` — список всех webhook'ов со статусом доставки
- `trace_webhook_test` — тестовый ping на конкретный webhook

```bash
# Список зарегистрированных webhook'ов
curl -s -X POST http://127.0.0.1:9876/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"trace_webhook_list","arguments":{}}}'

# Тестовый webhook
curl -s -X POST http://127.0.0.1:9876/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"trace_webhook_test","arguments":{"webhook_id":"surveyor-invalidation"}}}'
```

## Лицензирование

### Сравнение тарифов

| Режим | Сессии | Событий/день | Машин | Инструменты | Хранилище | Макс. версия |
|-------|--------|-------------|-------|-------------|-----------|-------------|
| **Community** (без ключа) | 1 | 100 | 1 | Базовые (3) | SQLite | — |
| **Trial** | ∞ | ∞ | 3 | Все | SQLite | — |
| **Indie** | 5 | 10 000 | 1 | Все | SQLite | — |
| **Pro** | 10 | 100 000 | 3 | Все | **PostgreSQL** | по лицензии |
| **Enterprise** | ∞ | ∞ | ∞ | Все | **PostgreSQL** | по лицензии |
| **Privileged** | ∞ | ∞ | настраивается | Все | **PostgreSQL** | по лицензии |

- **Хранилище:** Тарифы Pro/Enterprise/Privileged могут использовать PostgreSQL вместо SQLite. Требуется фича `postgres` в лицензионном ключе и `storage.type: postgres` в `revizor.yaml` (v58).
- **Макс. версия:** Вечные лицензии (`exp=-1`) могут содержать поле `max_ver` — версия бинарника не должна превышать это значение, иначе лицензия переходит в Community mode (v58).
- **Воскресный безлимит** — бесплатное безлимитное использование по воскресеньям для всех тарифов.
- **Активация:** `REVIZOR_LICENSE_KEY="ваш-ключ" revizor serve` или `license_key` в `revizor.yaml`.

---

## Конфигурация

Файл `revizor.yaml` (ищется в CWD, затем `~/.config/ais_tools/revizor/`, затем `~/.config/revizor/` legacy). Пример: `revizor.yaml.example`.

```yaml
server:
  host: "127.0.0.1"
  port: 9876

storage:
  type: sqlite                 # или postgres
  sqlite_path: "./revizor.db"

logging:
  level: info                  # debug | info | warn | error

presets:                       # готовые наборы путей
  debug_elevation:
    description: "Отладка elevation"
    paths:
      - "elevation.**"
      - "auth.callback.*"
```

---

## Подробная документация

- [README.md](README.md) — английская версия (основная, входит в дистрибутив)
- [DEV_README.md](DEV_README.md) — руководство разработчика (сборка, MCP stdio, SDK, архитектура, полный список инструментов)
- [docs/API.md](docs/API.md) — полная спецификация HTTP API
- [passport.md](passport.md) — технический паспорт модуля
