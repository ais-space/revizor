"""
Глобальная конфигурация Revizor SDK.

Потокобезопасное хранение настроек: endpoint, api_key, session_id, параметры буфера.
"""

import threading
from typing import Optional

_LOCK = threading.Lock()

_state: dict = {
    "endpoint": "http://localhost:9876",
    "api_key": None,           # str | None — ключ API (sk-revizor-...)
    "session_id": None,        # str | None — ID сессии по умолчанию
    "buffer_size": 50,         # int — размер batch-буфера
    "flush_interval_ms": 100,  # int — интервал сброса в мс
    "enabled": True,           # bool — глобальное вкл/выкл
}


def configure(
    endpoint: Optional[str] = None,
    api_key: Optional[str] = None,
    session_id: Optional[str] = None,
    buffer_size: Optional[int] = None,
    flush_interval_ms: Optional[int] = None,
    enabled: Optional[bool] = None,
) -> None:
    """
    Настроить Revizor SDK.

    Все параметры опциональны — передаются только те, которые нужно изменить.

    Args:
        endpoint: URL Go-бинарника Ревизора (по умолчанию http://localhost:9876)
        api_key: Ключ API в формате sk-revizor-...
        session_id: ID сессии трассировки по умолчанию
        buffer_size: Размер batch-буфера (событий)
        flush_interval_ms: Интервал сброса буфера (мс)
        enabled: Глобальное включение/выключение SDK
    """
    with _LOCK:
        if endpoint is not None:
            _state["endpoint"] = endpoint
        if api_key is not None:
            _state["api_key"] = api_key
        if session_id is not None:
            _state["session_id"] = session_id
        if buffer_size is not None:
            _state["buffer_size"] = max(10, min(buffer_size, 200))
        if flush_interval_ms is not None:
            _state["flush_interval_ms"] = max(50, min(flush_interval_ms, 5000))
        if enabled is not None:
            _state["enabled"] = enabled

    # Проверка связи с сервером (если SDK включен)
    if _state["enabled"] and _state["endpoint"]:
        _check_connectivity(_state["endpoint"])


def _check_connectivity(endpoint: str) -> None:
    """Проверяет доступность сервера Ревизора. При недоступности — предупреждает."""
    import json as _json
    from urllib.request import Request, urlopen
    try:
        req = Request(
            f"{endpoint.rstrip('/')}/mcp",
            data=_json.dumps({
                "jsonrpc": "2.0", "id": 1, "method": "tools/call",
                "params": {"name": "trace_ping", "arguments": {}}
            }).encode("utf-8"),
            headers={"Content-Type": "application/json"},
        )
        resp = urlopen(req, timeout=5)
        data = _json.loads(resp.read())
        text = data.get("result", {}).get("content", [{}])[0].get("text", "")
        if "pong" in text:
            import logging
            logging.getLogger("revizor_sdk").info(f"Revizor connected: {text.split(chr(10))[0]}")
            return
    except Exception:
        pass
    import logging
    logging.getLogger("revizor_sdk").warning(
        f"Revizor server unreachable at {endpoint}. "
        f"Trace events will be buffered but not delivered. "
        f"Start the server with: /usr/local/bin/revizor serve"
    )


def get_config() -> dict:
    """Вернуть копию текущей конфигурации (потокобезопасно)."""
    with _LOCK:
        return dict(_state)


def reset_config() -> None:
    """Сбросить конфигурацию к значениям по умолчанию."""
    with _LOCK:
        _state["endpoint"] = "http://localhost:9876"
        _state["api_key"] = None
        _state["session_id"] = None
        _state["buffer_size"] = 50
        _state["flush_interval_ms"] = 100
        _state["enabled"] = True
