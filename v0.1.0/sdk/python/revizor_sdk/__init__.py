"""
Клиентский Python SDK для подсистемы Ревизор.

Отправляет trace-события в Go-бинарник Ревизора по HTTP.

Использование:
    from revizor_sdk import configure, trace, trace_start

    configure(endpoint="http://localhost:9876", session_id="my-session")

    trace("auth.login.enter", {"provider": "google"})

    end = trace_start("auth.login.validate")
    # ... код ...
    end({"user_id": user.id})
"""

from revizor_sdk._trace import trace, trace_start, load_config, flush
from revizor_sdk.config import configure, get_config, reset_config

__all__ = [
    "configure",
    "get_config",
    "reset_config",
    "trace",
    "trace_start",
    "load_config",
    "flush",
]
