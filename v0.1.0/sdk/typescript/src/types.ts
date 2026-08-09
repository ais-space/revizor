/** Данные для отправки trace-события */
export interface TraceData {
  [key: string]: unknown;
}

/** Функция завершения замера длительности (возвращается traceStart) */
export type TraceEndFn = (data?: TraceData) => void;

/** Параметры инициализации SDK */
export interface TraceInitOptions {
  /** URL Go-бинарника Ревизора */
  apiBaseUrl: string;
  /** Ключ API в формате sk-revizor-... */
  apiKey?: string;
  /** ID сессии по умолчанию */
  sessionId?: string;
  /** Размер batch-буфера (10-200, по умолчанию 50) */
  bufferSize?: number;
  /** Интервал сброса буфера в мс (50-5000, по умолчанию 100) */
  flushIntervalMs?: number;
}

/** Элемент в batch-буфере */
export interface TraceEvent {
  path: string;
  data?: TraceData;
  session_id?: string;
}

/** Ответ сервера на batch-запрос */
export interface BatchResponse {
  status: string;
  accepted: number;
  skipped: number;
  rejected: number;
}
