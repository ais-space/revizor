/**
 * Двухслойная санитизация PII для Ревизора.
 *
 * Порт из revizor-go/internal/trace/sanitize.go.
 * Маскирует чувствительные данные ДО отправки по сети.
 *
 * Слой 1: фильтрация по именам ключей (case-insensitive).
 * Слой 2: regex-поиск чувствительных паттернов в строковых значениях.
 */

// Максимальная глубина рекурсивного обхода
const MAX_DEPTH = 5;

// Маска для замаскированных значений
const MASK = "***";

// --- Слой 1: чувствительные ключи (case-insensitive) ---

const SENSITIVE_FIELDS = new Set([
  "token", "password", "secret", "api_key",
  "access_token", "refresh_token", "authorization",
  "cookie", "set_cookie", "credential",
  "private_key", "client_secret", "session_key",
  "csrf_token", "jwt", "bearer",
  "simca_session", "simca_admin_session", "auth_token",
]);

const SENSITIVE_PREFIXES = ["pii_", "violator_"];

// --- Слой 2: regex-паттерны ---

const SENSITIVE_REGEXPS: RegExp[] = [
  /eyJ[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+/,   // JWT
  /simca_session_[a-zA-Z0-9_-]+/,
  /simca_admin_session_[a-zA-Z0-9_-]+/,
  /\b[\w.+-]+@[\w-]+\.[\w.]+\b/,                           // email
  /[A-Za-z0-9+/=]{40,}/,                                   // base64/crypto
  /(?:sk|pk|key|api)-[a-zA-Z0-9]{20,}/,                    // API keys
];

function isSensitiveKey(key: string): boolean {
  const lower = key.toLowerCase();
  if (SENSITIVE_FIELDS.has(lower)) return true;
  for (const prefix of SENSITIVE_PREFIXES) {
    if (lower.startsWith(prefix)) return true;
  }
  return false;
}

function maskMatch(matched: string): string {
  if (matched.length > 8) {
    return matched.slice(0, 4) + MASK + matched.slice(-4);
  }
  return matched.slice(0, Math.min(4, matched.length)) + MASK;
}

function sanitizeString(value: string): string {
  for (const re of SENSITIVE_REGEXPS) {
    value = value.replace(re, maskMatch);
  }
  return value;
}

function truncate(obj: unknown): unknown {
  if (typeof obj === "object" && obj !== null) {
    if (Array.isArray(obj)) {
      return { _truncated: true, length: obj.length };
    }
    const keys = Object.keys(obj as Record<string, unknown>);
    if (keys.length > 10) {
      return { _truncated: true, keys: keys.slice(0, 10) };
    }
    return obj;
  }
  if (typeof obj === "string") {
    if (obj.length > 100) {
      return obj.slice(0, 100) + "...";
    }
    return obj;
  }
  return obj;
}

/**
 * Двухслойная санитизация данных.
 *
 * Рекурсивно обходит объекты и массивы. Маскирует:
 * - Ключи из SENSITIVE_FIELDS (Слой 1)
 * - Значения, совпадающие с regex-паттернами (Слой 2)
 *
 * При превышении глубины MAX_DEPTH — обрезает объект.
 */
export function deepSanitize(obj: unknown, depth: number = 0): unknown {
  if (depth >= MAX_DEPTH) {
    return truncate(obj);
  }

  if (typeof obj === "object" && obj !== null) {
    if (Array.isArray(obj)) {
      return obj.map((val) => deepSanitize(val, depth + 1));
    }

    const result: Record<string, unknown> = {};
    for (const [key, val] of Object.entries(obj as Record<string, unknown>)) {
      if (isSensitiveKey(key)) {
        result[key] = MASK;
      } else {
        result[key] = deepSanitize(val, depth + 1);
      }
    }
    return result;
  }

  if (typeof obj === "string") {
    return sanitizeString(obj);
  }

  return obj;
}
