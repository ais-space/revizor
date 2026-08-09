"""
Публичный API Revizor SDK.

Предоставляет:
- trace(path, data?, session_id?) — отправить trace-событие
- trace_start(path, session_id?) → end(data?) — замер длительности
- load_config() — загрузить конфиг точек с сервера
- flush() — принудительный сброс буфера

Принципы:
- Fire-and-forget: trace() никогда не ждёт ответа сервера
- Never-throw: исключения глотаются, приложение не роняется
- Zero-overhead: если точка выключена — O(1) проверка локального кэша
- Санитизация: PII маскируется на клиенте до отправки
"""

import re
import time
import logging
from typing import Callable, Optional
from urllib.request import Request, urlopen

from revizor_sdk.config import get_config
from revizor_sdk.sanitize import deep_sanitize
from revizor_sdk.buffer import get_buffer

logger = logging.getLogger("revizor_sdk.trace")

# --- Валидация пути ---

_MAX_PATH_LENGTH = 120
_PATH_PATTERN = re.compile(r"^[a-z0-9_.*]+$")


def _validate_path(path: str) -> bool:
    """Проверить формат trace-пути."""
    if not path or len(path) > _MAX_PATH_LENGTH:
        return False
    return bool(_PATH_PATTERN.match(path))


# --- Кэш конфига ---

_config_cache: dict[str, bool] = {}
_config_fetched_at: float = 0.0
_config_loaded: bool = False
_CONFIG_TTL_SEC = 30.0


def _match_glob(pattern: str, path: str) -> bool:
    """
    Проверить соответствие пути glob-паттерну.

    ** — greedy (любая глубина), * — ровно один сегмент.
    Специальный случай: одиночная * = match all.
    """
    if pattern == path or pattern == "*":
        return True

    path_parts = path.split(".")
    pattern_parts = pattern.split(".")

    pi, pp = 0, 0
    while pi < len(path_parts) and pp < len(pattern_parts):
        if pattern_parts[pp] == "**":
            return True  # greedy
        if pattern_parts[pp] == "*":
            pi += 1
            pp += 1
        elif pattern_parts[pp] == path_parts[pi]:
            pi += 1
            pp += 1
        else:
            return False

    return pi == len(path_parts) and pp == len(pattern_parts)


def _should_trace(path: str) -> bool:
    """
    Проверить, включена ли точка.

    Использует локальный кэш конфига. При первом вызове или истечении TTL —
    загружает конфиг с сервера. Если сервер недоступен — разрешает все точки.
    """
    global _config_cache, _config_fetched_at, _config_loaded

    now = time.monotonic()
    if not _config_loaded or (now - _config_fetched_at) > _CONFIG_TTL_SEC:
        load_config()

    if not _config_loaded or not _config_cache:
        # Конфиг не загружен — разрешаем всё (default allow)
        return True

    # Точное совпадение
    if path in _config_cache:
        return _config_cache[path]

    # Glob-матчинг: наиболее специфичный паттерн
    best_match: Optional[bool] = None
    best_specificity = -1
    for pattern, enabled in _config_cache.items():
        if not _match_glob(pattern, path):
            continue
        specificity = pattern.replace("**", "").count(".")
        if specificity > best_specificity:
            best_specificity = specificity
            best_match = enabled

    if best_match is not None:
        return best_match

    # Нет совпадений — точка не сконфигурирована
    return False


def load_config() -> dict[str, bool]:
    """
    Загрузить конфиг trace-точек с сервера.

    GET /api/v1/trace/config с API-key в заголовке Authorization.
    Возвращает словарь {trace_path: enabled}.

    При ошибке сети — кэш не обновляется, возвращается пустой словарь.
    """
    global _config_cache, _config_fetched_at, _config_loaded

    cfg = get_config()
    url = cfg["endpoint"].rstrip("/") + "/api/v1/trace/config"

    sess_id = cfg["session_id"]
    if sess_id:
        url += f"?session_id={sess_id}"

    try:
        req = Request(url, method="GET")
        if cfg["api_key"]:
            req.add_header("Authorization", f"Bearer {cfg['api_key']}")

        with urlopen(req, timeout=5) as resp:
            import json
            data = json.loads(resp.read().decode("utf-8"))

        _config_cache.clear()
        if "config" in data and isinstance(data["config"], dict):
            for path, enabled in data["config"].items():
                _config_cache[path] = bool(enabled)

        _config_fetched_at = time.monotonic()
        _config_loaded = True
        return dict(_config_cache)

    except Exception:
        # Ошибка загрузки — оставляем старый кэш
        return dict(_config_cache)


# --- Публичный API ---

def trace(
    path: str,
    data: Optional[dict] = None,
    session_id: Optional[str] = None,
) -> bool:
    """
    Отправить trace-событие.

    Fire-and-forget: событие помещается в batch-буфер и отправляется фоновым потоком.
    Никогда не бросает исключений.

    Args:
        path: Путь trace-точки (например, "auth.login.enter")
        data: Данные для логирования (будут санитизированы)
        session_id: ID сессии (если None — из конфигурации)

    Returns:
        True если событие принято в буфер, False если точка выключена
    """
    cfg = get_config()

    try:
        if not cfg["enabled"]:
            return False

        if not _validate_path(path):
            return False

        if not _should_trace(path):
            return False

        sid = session_id or cfg["session_id"]
        sanitized = deep_sanitize(data) if data else None

        entry = {
            "path": path,
            "data": sanitized,
            "session_id": sid,
        }

        get_buffer().enqueue(entry)
        return True

    except Exception:
        # Never-throw
        return False


def trace_start(
    path: str,
    session_id: Optional[str] = None,
) -> Callable[[Optional[dict]], bool]:
    """
    Начать замер длительности операции.

    Отправляет trace-событие с суффиксом .enter.
    Возвращает end(data?), которая отправляет .success с duration_ms.

    Args:
        path: Базовый путь trace-точки (без суффикса)
        session_id: ID сессии

    Returns:
        Функция end(data?) → bool

    Пример:
        end = trace_start("auth.login.validate")
        # ... код ...
        end({"user_id": user.id})
        # Отправлено: auth.login.validate.enter + .success (duration_ms)
    """
    start_time = time.monotonic()
    trace(f"{path}.enter", None, session_id)

    def end(data: Optional[dict] = None) -> bool:
        duration_ms = (time.monotonic() - start_time) * 1000.0
        merged = {"duration_ms": round(duration_ms, 3)}
        if data:
            merged.update(data)
        return trace(f"{path}.success", merged, session_id)

    return end


def flush() -> None:
    """Принудительно сбросить batch-буфер (для тестов)."""
    from revizor_sdk.buffer import flush as _flush_buf
    _flush_buf()
