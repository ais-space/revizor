# Revizor SDK для Python

Клиентская библиотека для отправки trace-событий в Go-бинарник Ревизора.

## Установка

```bash
pip install revizor-sdk
```

## Быстрый старт

```python
from revizor_sdk import configure, trace, trace_start

# Настройка
configure(
    endpoint="http://localhost:9876",
    api_key="sk-revizor-...",
    session_id="my-session",
)

# Простое событие
trace("auth.login.enter", {"provider": "google"})

# Замер длительности
end = trace_start("auth.login.validate")
# ... ваш код ...
end({"user_id": user.id})
# Отправлено: auth.login.validate.enter + .success (с duration_ms)
```

## Принципы

- **Fire-and-forget** — `trace()` никогда не ждёт ответа сервера
- **Never-throw** — ошибки сети молча глотаются
- **Zero-overhead** — если точка выключена, O(1) проверка локального кэша
- **Санитизация** — PII маскируется на клиенте до отправки
- **Batching** — события накапливаются и отправляются пакетами (50 событий / 100 мс)

## API

### `configure(**kwargs)`

Настройка SDK. Все параметры опциональны.

| Параметр | Тип | По умолчанию | Описание |
|----------|-----|-------------|----------|
| `endpoint` | str | `http://localhost:9876` | URL Go-бинарника Ревизора |
| `api_key` | str | None | Ключ API (sk-revizor-...) |
| `session_id` | str | None | ID сессии по умолчанию |
| `buffer_size` | int | 50 | Размер batch-буфера |
| `flush_interval_ms` | int | 100 | Интервал сброса буфера (мс) |
| `enabled` | bool | True | Глобальное вкл/выкл |

### `trace(path, data?, session_id?)`

Отправить trace-событие. Fire-and-forget.

### `trace_start(path, session_id?)`

Начать замер длительности. Возвращает `end(data?)`.

### `load_config()`

Загрузить конфиг точек с сервера. Возвращает `{path: enabled}`.

### `flush()`

Принудительно сбросить batch-буфер.

## Зависимости

Только стандартная библиотека Python (≥3.9). Нуль внешних зависимостей.
