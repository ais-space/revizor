"""
Двухслойная санитизация PII для Ревизора.

Порт из ais_products/revizor/internal/trace/sanitize.go.
Маскирует чувствительные данные ДО отправки по сети.

Слой 1: фильтрация по именам ключей (case-insensitive).
Слой 2: regex-поиск чувствительных паттернов в строковых значениях.
"""

import re

# Максимальная глубина рекурсивного обхода
MAX_DEPTH = 5

# Маска для замаскированных значений
MASK = "***"

# --- Слой 1: Чувствительные ключи (case-insensitive) ---

SENSITIVE_FIELDS: set[str] = {
    "token", "password", "secret", "api_key",
    "access_token", "refresh_token", "authorization",
    "cookie", "set_cookie", "credential",
    "private_key", "client_secret", "session_key",
    "csrf_token", "jwt", "bearer",
    "simca_session", "simca_admin_session", "auth_token",
}

SENSITIVE_PREFIXES: tuple[str, ...] = ("pii_", "violator_")

# --- Слой 2: Regex-паттерны ---

_SENSITIVE_REGEXPS: list[re.Pattern[str]] = [
    re.compile(r'eyJ[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+'),  # JWT
    re.compile(r'simca_session_[a-zA-Z0-9_-]+'),
    re.compile(r'simca_admin_session_[a-zA-Z0-9_-]+'),
    re.compile(r'\b[\w.+-]+@[\w-]+\.[\w.]+\b'),                         # email
    re.compile(r'[A-Za-z0-9+/=]{40,}'),                                  # base64/crypto
    re.compile(r'(?:sk|pk|key|api)-[a-zA-Z0-9]{20,}'),                   # API keys
]


def _is_sensitive_key(key: str) -> bool:
    """Проверяет, является ли ключ чувствительным (Слой 1)."""
    lower = key.lower()
    if lower in SENSITIVE_FIELDS:
        return True
    for prefix in SENSITIVE_PREFIXES:
        if lower.startswith(prefix):
            return True
    return False


def _mask_match(matched: str) -> str:
    """Маскирует найденное совпадение: префикс + *** + суффикс."""
    if len(matched) > 8:
        return matched[:4] + MASK + matched[-4:]
    return matched[:min(4, len(matched))] + MASK


def _sanitize_string(value: str) -> str:
    """Применяет все regex-паттерны к строке (Слой 2)."""
    for pattern in _SENSITIVE_REGEXPS:
        value = pattern.sub(lambda m: _mask_match(m.group()), value)
    return value


def _truncate(obj: object) -> object:
    """Обрезает объект при превышении максимальной глубины."""
    if isinstance(obj, dict):
        keys = list(obj.keys())
        if len(keys) > 10:
            keys = keys[:10]
        return {"_truncated": True, "keys": keys}
    if isinstance(obj, list):
        return {"_truncated": True, "length": len(obj)}
    if isinstance(obj, str):
        if len(obj) > 100:
            return obj[:100] + "..."
        return obj
    return obj


def deep_sanitize(obj: object, depth: int = 0) -> object:
    """
    Двухслойная санитизация данных.

    Рекурсивно обходит словари и списки. Маскирует:
    - Ключи из SENSITIVE_FIELDS (Слой 1)
    - Значения, совпадающие с regex-паттернами (Слой 2)

    При превышении глубины MAX_DEPTH — обрезает объект.
    Безопасно обрабатывает любые типы данных.
    """
    if depth >= MAX_DEPTH:
        return _truncate(obj)

    if isinstance(obj, dict):
        result: dict[str, object] = {}
        for key, val in obj.items():
            if _is_sensitive_key(key):
                result[key] = MASK
            else:
                result[key] = deep_sanitize(val, depth + 1)
        return result

    if isinstance(obj, list):
        return [deep_sanitize(val, depth + 1) for val in obj]

    if isinstance(obj, str):
        return _sanitize_string(obj)

    return obj
