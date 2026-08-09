/**
 * Публичный API Revizor SDK.
 *
 * Предоставляет:
 * - trace(path, data?, sessionId?) — отправить trace-событие
 * - traceStart(path, sessionId?) → end(data?) — замер длительности
 * - initTrace(options) — единая точка инициализации
 * - flushTrace() — принудительный сброс буфера
 *
 * Принципы:
 * - Fire-and-forget: trace() никогда не ждёт ответа сервера
 * - Never-throw: исключения глотаются, приложение не роняется
 * - Zero-overhead: если точка выключена — O(1) проверка локального кэша
 * - Санитизация: PII маскируется на клиенте до отправки
 */

import type { TraceData, TraceEndFn, TraceInitOptions } from "./types";
import { deepSanitize } from "./sanitize";
import {
  enqueue,
  flush,
  startFlushTimer,
  stopFlushTimer,
  clearBuffer,
  installBeforeUnload,
} from "./buffer";
import {
  apiBaseUrl,
  apiKey,
  sessionId,
  configCache,
  CONFIG_TTL_MS,
  enabled,
  setApiBaseUrl,
  setApiKey,
  setSessionId,
  setBufferSize,
  setFlushIntervalMs,
} from "./config";

// --- Переменные для отслеживания кэша ---

let _cacheTimestamp = 0;

// --- Валидация пути ---

const MAX_PATH_LENGTH = 120;
const PATH_PATTERN = /^[a-z0-9_.*]+$/;

function validatePath(path: string): boolean {
  if (!path || path.length > MAX_PATH_LENGTH) return false;
  return PATH_PATTERN.test(path);
}

// --- Glob-матчинг ---

function matchGlob(pattern: string, path: string): boolean {
  if (pattern === path) return true;

  const pathParts = path.split(".");
  const patternParts = pattern.split(".");

  let pi = 0, pp = 0;
  while (pi < pathParts.length && pp < patternParts.length) {
    if (patternParts[pp] === "**") return true; // greedy
    if (patternParts[pp] === "*") {
      pi++;
      pp++;
    } else if (patternParts[pp] === pathParts[pi]) {
      pi++;
      pp++;
    } else {
      return false;
    }
  }

  return pi === pathParts.length && pp === patternParts.length;
}

// --- Кэш конфига ---

async function loadConfig(): Promise<Map<string, boolean>> {
  let url = `${apiBaseUrl}/api/v1/trace/config`;
  if (sessionId) {
    url += `?session_id=${encodeURIComponent(sessionId)}`;
  }

  const headers: Record<string, string> = {};
  if (apiKey) {
    headers["Authorization"] = `Bearer ${apiKey}`;
  }

  try {
    // Ручной таймаут вместо AbortSignal.timeout() для совместимости с Midori
    const ctrl = new AbortController();
    const timeoutId = setTimeout(() => ctrl.abort(), 5000);
    const resp = await fetch(url, {
      headers,
      signal: ctrl.signal,
    });
    clearTimeout(timeoutId);
    if (!resp.ok) return configCache ?? new Map();

    const data = await resp.json();
    const map = new Map<string, boolean>();

    if (data.config && typeof data.config === "object") {
      for (const [path, enabled] of Object.entries(data.config)) {
        map.set(path, enabled as boolean);
      }
    }

    // Обновляем кэш в config.ts (let-переменные)
    (configCache as Map<string, boolean> | null) = map;
    _cacheTimestamp = Date.now();

    // REV-017: если ни одна точка не активна — остановить таймер и очистить буфер
    let hasActive = false;
    for (const v of map.values()) { if (v) { hasActive = true; break; } }
    if (hasActive) {
      startFlushTimer();
    } else {
      stopFlushTimer();
      clearBuffer();
    }

    return map;
  } catch {
    // Ошибка загрузки — оставляем старый кэш
    return configCache ?? new Map();
  }
}

function shouldTrace(path: string): boolean {
  const now = Date.now();

  // Загружаем конфиг если нужно
  if (!configCache || (now - _cacheTimestamp) > CONFIG_TTL_MS) {
    // Асинхронно запускаем загрузку, синхронно возвращаем default allow
    loadConfig().catch(() => {});
    if (!configCache) return true; // нет кэша — разрешаем
  }

  if (configCache.size === 0) {
    return true; // пустой кэш — разрешаем всё
  }

  // Точное совпадение
  if (configCache.has(path)) {
    return configCache.get(path)!;
  }

  // Glob-матчинг: наиболее специфичный паттерн
  let bestMatch: boolean | undefined;
  let bestSpecificity = -1;
  for (const [pattern, enabled] of configCache) {
    if (!matchGlob(pattern, path)) continue;
    const specificity = pattern.replace(/\*\*/g, "").length;
    if (specificity > bestSpecificity) {
      bestSpecificity = specificity;
      bestMatch = enabled;
    }
  }

  if (bestMatch !== undefined) return bestMatch;

  // Нет совпадений
  return false;
}

// --- Авто-инициализация (ленивая, при первом вызове trace) ---

let _autoInitialized = false;

function autoInit(): void {
  if (_autoInitialized) return;
  if (typeof window === "undefined") return;
  _autoInitialized = true;
  startFlushTimer();
  installBeforeUnload();
  loadConfig().catch(() => {});
}

// --- Публичный API ---

/**
 * Отправить trace-событие.
 *
 * Fire-and-forget: событие помещается в batch-буфер.
 * При первом вызове автоматически запускает flush-таймер и beforeunload.
 * Никогда не бросает исключений.
 */
export function trace(
  path: string,
  data?: TraceData,
  sessionIdOverride?: string,
): void {
  if (!enabled) return;

  if (!_autoInitialized) autoInit();
  if (!_autoInitialized) return;  // всё ещё не инициализирован (серверный рендеринг)
  try {
    if (!validatePath(path)) return;
    if (!shouldTrace(path)) return;

    const sanitized = data ? (deepSanitize(data) as TraceData) : undefined;
    const effectiveSid = sessionIdOverride ?? sessionId;
    const entry = {
      path,
      data: sanitized,
      session_id: effectiveSid,
    };

    enqueue(entry);
  } catch {
    // Never-throw
  }
}

/**
 * Начать замер длительности операции.
 *
 * Отправляет .enter. Возвращает end(data?), которая отправляет .success
 * с автоматически вычисленным duration_ms.
 */
export function traceStart(
  path: string,
  sessionId?: string,
): TraceEndFn {
  const startTime = performance.now();
  trace(`${path}.enter`, undefined, sessionId);

  return (data?: TraceData) => {
    const durationMs = Math.round(performance.now() - startTime);
    const merged: TraceData = { ...(data || {}), duration_ms: durationMs };
    trace(`${path}.success`, merged, sessionId);
  };
}

/**
 * Инициализация SDK с нестандартными настройками.
 * Не обязательна — trace() сам запустит таймер при первом вызове.
 * Вызывайте только если нужно переопределить apiBaseUrl, bufferSize и т.д.
 * Должна вызываться ДО первого trace(), иначе настройки не применятся.
 */
export function initTrace(options: TraceInitOptions): void {
  const {
    apiBaseUrl: url,
    apiKey: key,
    sessionId: sid,
    bufferSize: bufSize,
    flushIntervalMs: flushMs,
  } = options;

  if (url) setApiBaseUrl(url);
  if (key !== undefined) setApiKey(key);
  if (sid !== undefined) setSessionId(sid);
  if (bufSize !== undefined) setBufferSize(bufSize);
  if (flushMs !== undefined) setFlushIntervalMs(flushMs);

  // Принудительно инициализируем даже если trace() ещё не вызывался
  autoInit();
}

/**
 * @deprecated Используйте initTrace(). Сохранён для обратной совместимости.
 */
export const configureTraceClient = initTrace;

/**
 * Принудительно сбросить batch-буфер (для тестов).
 */
export function flushTrace(): void {
  if (!_autoInitialized) return;
  flush();
}
