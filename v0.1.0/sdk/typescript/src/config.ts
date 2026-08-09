/**
 * Глобальная конфигурация Revizor SDK.
 */

/** URL Go-бинарника Ревизора */
export let apiBaseUrl = "http://localhost:9876";

/** Ключ API (sk-revizor-...) */
export let apiKey: string | undefined;

/** ID сессии по умолчанию */
export let sessionId: string | undefined;

/** Размер batch-буфера */
export let bufferSize = 50;

/** Интервал сброса буфера в мс */
export let flushIntervalMs = 100;

/** Кэш конфига: {path: enabled} */
export let configCache: Map<string, boolean> | null = null;

/** Время последней загрузки конфига (timestamp) */
export let configFetchedAt = 0;

/** Время последней загрузки конфига (Date.now()) */
export let configFetchedAtDate = 0;

/** TTL кэша конфига в мс */
export const CONFIG_TTL_MS = 30_000;

/** Глобальное вкл/выкл */
export let enabled = true;

export function setApiBaseUrl(url: string): void {
  apiBaseUrl = url;
}

export function setApiKey(key: string | undefined): void {
  apiKey = key;
}

export function setSessionId(id: string | undefined): void {
  sessionId = id;
}

export function setBufferSize(size: number): void {
  bufferSize = Math.max(10, Math.min(size, 200));
}

export function setFlushIntervalMs(ms: number): void {
  flushIntervalMs = Math.max(50, Math.min(ms, 5000));
}

export function setEnabled(value: boolean): void {
  enabled = value;
}
