/**
 * Batch-буфер Revizor SDK.
 *
 * Накапливает trace-события и отправляет их пакетами через HTTP POST
 * в Go-бинарник Ревизора.
 *
 * Принципы:
 * - Fire-and-forget: ответ сервера не обрабатывается
 * - Never-throw: ошибки сети молча глотаются
 * - sendBeacon при beforeunload (если доступен)
 */

import type { TraceEvent, BatchResponse } from "./types";
import {
  apiBaseUrl,
  apiKey,
  bufferSize,
  flushIntervalMs,
} from "./config";

/** Буфер накопленных событий */
let _buffer: TraceEvent[] = [];

/** ID setInterval для фонового сброса */
let _flushTimer: ReturnType<typeof setInterval> | null = null;

/** Флаг: beforeunload уже установлен */
let _beforeUnloadInstalled = false;

export function getBufferSize(): number {
  return _buffer.length;
}

export function enqueue(entry: TraceEvent): void {
  _buffer.push(entry);
  if (_buffer.length >= bufferSize) {
    flush();
  }
}

/** Очистить буфер без отправки (при деактивации всех точек). */
export function clearBuffer(): void {
  _buffer = [];
}

export function flush(): void {
  if (_buffer.length === 0) return;

  const batch = _buffer;
  _buffer = [];

  try {
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
    };
    // Передать session_id из первого события пакета (REV-016)
    if (batch.length > 0 && batch[0].session_id) {
      headers["X-Revizor-Session"] = batch[0].session_id;
    }
    if (apiKey) {
      headers["Authorization"] = `Bearer ${apiKey}`;
    }

    fetch(`${apiBaseUrl}/api/v1/trace/log/batch`, {
      method: "POST",
      headers,
      body: JSON.stringify({ events: batch }),
    }).catch(() => {
      // Never-throw: ошибки сети глотаются
    });
  } catch {
    // Never-throw
  }
}

export function startFlushTimer(): void {
  if (_flushTimer !== null || typeof window === "undefined") return;
  _flushTimer = setInterval(flush, flushIntervalMs);
}

export function stopFlushTimer(): void {
  if (_flushTimer !== null) {
    clearInterval(_flushTimer);
    _flushTimer = null;
  }
}

export function installBeforeUnload(): void {
  if (_beforeUnloadInstalled || typeof window === "undefined") return;
  _beforeUnloadInstalled = true;

  window.addEventListener("beforeunload", () => {
    if (_buffer.length === 0) return;
    if (typeof navigator !== "undefined" && navigator.sendBeacon) {
      const headers: Record<string, string> = {
        "Content-Type": "application/json",
      };
      if (apiKey) {
        headers["Authorization"] = `Bearer ${apiKey}`;
      }
      // sendBeacon не поддерживает кастомные заголовки, поэтому используем Blob
      const blob = new Blob(
        [JSON.stringify({ events: _buffer })],
        { type: "application/json" }
      );
      navigator.sendBeacon(`${apiBaseUrl}/api/v1/trace/log/batch`, blob);
      _buffer = [];
    }
  });
}
