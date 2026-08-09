# Revizor SDK для TypeScript

Клиентская библиотека для отправки trace-событий в Go-бинарник Ревизора.

## Установка

```bash
npm install @ais-platform/revizor-sdk
```

## Быстрый старт

```typescript
import { initTrace, trace, traceStart } from '@ais-platform/revizor-sdk';

// Инициализация
initTrace({
    apiBaseUrl: 'http://localhost:9876',
    apiKey: 'sk-revizor-...',
    sessionId: 'my-session',
    bufferSize: 50,
});

// Простое событие
trace('auth.login.enter', { provider: 'google' });

// Замер длительности
const end = traceStart('auth.login.validate');
// ... ваш код ...
end({ user_id: user.id });
// Отправлено: auth.login.validate.enter + .success (с duration_ms)
```

## Принципы

- **Fire-and-forget** — `trace()` никогда не ждёт ответа сервера
- **Never-throw** — ошибки сети молча глотаются
- **Zero-overhead** — если точка выключена, O(1) проверка локального кэша
- **Санитизация** — PII маскируется на клиенте до отправки
- **Batching** — события накапливаются и отправляются пакетами (50 событий / 100 мс)
- **sendBeacon** — при beforeunload буфер сбрасывается через sendBeacon

## API

### `initTrace(options)`

Единая точка инициализации. Вызывается один раз при старте.

| Параметр | Тип | По умолчанию | Описание |
|----------|-----|-------------|----------|
| `apiBaseUrl` | string | `http://localhost:9876` | URL Go-бинарника |
| `apiKey` | string | — | Ключ API (sk-revizor-...) |
| `sessionId` | string | — | ID сессии |
| `bufferSize` | number | 50 | Размер batch-буфера (10-200) |
| `flushIntervalMs` | number | 100 | Интервал сброса (50-5000) |

### `trace(path, data?, sessionId?)`

Отправить trace-событие. Fire-and-forget.

### `traceStart(path, sessionId?)`

Начать замер длительности. Возвращает `end(data?)`.

### `flushTrace()`

Принудительный сброс буфера.

### `deepSanitize(obj)`

Двухслойная санитизация данных. Экспортируется для тестирования.

## Зависимости

Нуль runtime-зависимостей. Только Web API.
