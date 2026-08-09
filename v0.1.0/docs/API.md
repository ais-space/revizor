# HTTP API Ревизора

**Базовый URL:** `http://127.0.0.1:9876/api/v1/trace`

## Аутентификация

Защищённые эндпоинты требуют заголовок `Authorization: Bearer <api_key>`. Если `api_key` не задан в конфигурации — все эндпоинты открыты.

## Эндпоинты

### Открытые

| Метод | Путь | Назначение |
|-------|------|-----------|
| POST | `/log` | Отправить одно trace-событие |
| POST | `/log/batch` | Отправить пачку trace-событий |

### Защищённые (API-key)

| Метод | Путь | Назначение |
|-------|------|-----------|
| GET | `/log` | Прочитать лог |
| POST | `/config` | Включить/выключить точку |
| GET | `/config` | Получить активные точки |
| DELETE | `/config` | Удалить точку |
| POST | `/session` | Создать сессию |
| GET | `/sessions` | Список активных сессий |
| GET | `/stats` | Статистика сессии |
| POST | `/mcp` | MCP JSON-RPC 2.0 |
| POST | `/shutdown` | Graceful остановка сервера |

---

## POST /log

Отправить одно trace-событие.

**Request:**
```json
{
    "path": "auth.login.enter",
    "data": {"provider": "google", "user_id": 42},
    "session_id": "550e8400-e29b-41d4-a716-446655440000",
    "request_id": "a3f2b9e1"
}
```

| Поле | Тип | Обязательное | Описание |
|------|-----|-------------|----------|
| `path` | string | Да | Путь trace-точки (`[a-z0-9_.*]+`, ≤120 символов) |
| `data` | object | Нет | Данные события (будут санитизированы) |
| `session_id` | string | Нет | ID сессии |
| `request_id` | string | Нет | ID запроса (для объединения в цепочки) |

**Response 200 (точка включена):**
```json
{"status": "ok", "path": "auth.login.enter"}
```

**Response 200 (точка выключена):**
```json
{"status": "skipped", "path": "auth.login.enter", "reason": "disabled"}
```

**Response 400:**
```json
{"status": "error", "detail": "Невалидный trace-путь: Auth.Login.Enter"}
```

**Response 429:**
```json
{"status": "error", "detail": "Rate limit exceeded", "retry_after_ms": 1000}
```

---

## POST /log/batch

Отправить пачку trace-событий. События обрабатываются индивидуально: валидация, проверка конфига, санитизация. Принятые события записываются одной транзакцией.

**Request:**
```json
{
    "events": [
        {"path": "auth.login.enter", "data": {...}, "session_id": "uuid"},
        {"path": "auth.login.success", "data": {...}, "session_id": "uuid"}
    ]
}
```

**Response 200:**
```json
{"status": "ok", "accepted": 2, "skipped": 0, "rejected": 0}
```

| Поле | Описание |
|------|----------|
| `accepted` | Количество успешно записанных событий |
| `skipped` | Количество событий, пропущенных по конфигу (точка выключена) |
| `rejected` | Количество событий с невалидным путём |

**Response 500:**
```json
{"status": "error", "detail": "Ошибка пакетной записи"}
```

---

## GET /log

Прочитать trace-лог.

**Query-параметры:**

| Параметр | Тип | По умолчанию | Описание |
|----------|-----|-------------|----------|
| `session_id` | string | — | Фильтр по сессии |
| `lines` | int | 50 | Количество строк |
| `path` | string | — | Фильтр по trace-пути |
| `since` | string | — | Начало временного окна (ISO 8601) |
| `until` | string | — | Конец временного окна (ISO 8601) |

**Response 200:**
```json
{
    "logs": [
        {
            "id": 1,
            "trace_path": "auth.login.enter",
            "data": {"provider": "google"},
            "session_id": "uuid",
            "request_id": "rid",
            "created_at": "2026-05-08T12:00:00Z"
        }
    ],
    "count": 1
}
```

---

## POST /config

Включить или выключить trace-точку.

**Request:**
```json
{
    "trace_path": "auth.login.**",
    "enabled": true,
    "session_id": "uuid",
    "description": "Отладка OAuth"
}
```

| Поле | Тип | Обязательное | Описание |
|------|-----|-------------|----------|
| `trace_path` | string | Да | Путь или glob-паттерн |
| `enabled` | bool | Нет | Включить (true) / выключить (false). По умолчанию true |
| `session_id` | string | Нет | Привязка к сессии |
| `description` | string | Нет | Описание (для документации) |
| `output_file` | string | Нет | Путь к файлу для вывода |
| `sample_rate` | float | Нет | Частота сэмплирования (0.0-1.0) |

**Response 200:**
```json
{"status": "ok", "trace_path": "auth.login.**", "enabled": true}
```

---

## GET /config

Получить список активных trace-точек.

**Query-параметры:**

| Параметр | Тип | Описание |
|----------|-----|----------|
| `session_id` | string | Фильтр по сессии |

**Response 200:**
```json
{
    "session_id": "uuid",
    "config": {
        "auth.login.enter": true,
        "auth.login.success": true,
        "elevation.**": true
    },
    "total_paths": 3
}
```

---

## DELETE /config

Удалить trace-точку из конфигурации.

**Request:**
```json
{
    "trace_path": "auth.login.**",
    "session_id": "uuid"
}
```

**Response 200:**
```json
{"status": "ok", "trace_path": "auth.login.**"}
```

---

## POST /session

Создать новую сессию трассировки.

**Request:**
```json
{
    "owner": "developer@company.com",
    "description": "Отладка elevation"
}
```

**Response 200:**
```json
{
    "session_id": "550e8400-e29b-41d4-a716-446655440000",
    "owner": "developer@company.com",
    "description": "Отладка elevation",
    "created_at": "2026-05-08T12:00:00Z",
    "expires_at": "2026-05-15T12:00:00Z"
}
```

---

## GET /sessions

Получить список всех активных сессий.

**Response 200:**
```json
{
    "sessions": [
        {
            "session_id": "uuid",
            "owner": "developer@company.com",
            "description": "Отладка elevation",
            "created_at": "2026-05-08T12:00:00Z",
            "expires_at": "2026-05-15T12:00:00Z"
        }
    ]
}
```

---

## GET /stats

Получить статистику сессии.

**Query-параметры:**

| Параметр | Тип | Описание |
|----------|-----|----------|
| `session_id` | string | ID сессии |

**Response 200:**
```json
{
    "total_events": 1523,
    "unique_paths": 45,
    "last_event_at": "2026-05-08T12:30:00Z",
    "session_id": "uuid"
}
```

---

## POST /mcp

MCP JSON-RPC 2.0 эндпоинт. Принимает стандартные JSON-RPC запросы (`tools/list`, `tools/call`).

**Request (tools/list):**
```json
{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}
```

**Request (tools/call):**
```json
{
    "jsonrpc": "2.0",
    "id": 2,
    "method": "tools/call",
    "params": {
        "name": "trace_read",
        "arguments": {"session_id": "uuid", "lines": 50}
    }
}
```

Full list of tools: [DEV_README.md](../DEV_README.md).

---

## Rate Limiting

| Маршрут | Лимит | Ключ |
|---------|-------|------|
| `POST /log`, `POST /log/batch` | 100 req/s | session_id или IP |
| Остальные (`/config`, `/session`, `/stats`, `/mcp`) | 10 req/s | session_id или IP |

При превышении возвращается `429 Too Many Requests` с `retry_after_ms: 1000`.

## Санитизация

Все данные проходят двухслойную санитизацию на сервере:

- **Слой 1:** маскируются ключи из списка (password, token, secret, api_key, ...) и префиксы `pii_`, `violator_`
- **Слой 2:** regex-поиск JWT, email, API-ключей, base64-строк в значениях

SDK выполняют санитизацию на клиенте до отправки по сети.

## Формат trace-пути

- Только `[a-z0-9_.*]`
- Максимум 120 символов
- Сегменты разделены `.`
- `*` — ровно один сегмент
- `**` — любая глубина (greedy)
- `!` в начале — исключение (exclude)

**Примеры валидных путей:** `auth.login.enter`, `elevation.**`, `module.component.event`

**Примеры невалидных путей:** `Auth.Login.Enter` (uppercase), `auth_login_enter` (подчёркивания)

---

## POST /shutdown

Graceful остановка сервера Ревизора. Сбрасывает буферы, закрывает БД, завершает процесс.

**Response 200:**
```json
{"status": "ok", "message": "Revizor is shutting down gracefully..."}
```

**Response 500:**
```json
{"status": "error", "detail": "Ошибка при остановке"}
```
