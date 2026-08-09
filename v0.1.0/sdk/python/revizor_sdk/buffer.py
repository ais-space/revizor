"""
Batch-буфер Revizor SDK.

Накапливает trace-события и отправляет их пакетами через HTTP POST
в Go-бинарник Ревизора. Работает в фоновом daemon-потоке.

Принципы:
- Fire-and-forget: ответ сервера не читается
- Never-throw: ошибки сети молча глотаются
- Принудительный flush при завершении процесса (atexit)
"""

import json
import logging
import threading
import atexit
import urllib.request
from typing import Optional

from revizor_sdk.config import get_config

logger = logging.getLogger("revizor_sdk.buffer")


class BatchBuffer:
    """
    Потокобезопасный batch-буфер trace-событий.

    Накапливает события и отправляет их на сервер при:
    - достижении размера буфера (buffer_size)
    - истечении интервала (flush_interval_ms)
    - завершении процесса (atexit)
    """

    def __init__(self) -> None:
        self._buffer: list[dict] = []
        self._lock = threading.Lock()
        self._stop_event = threading.Event()
        self._worker: Optional[threading.Thread] = None
        self._started = False

    def start(self) -> None:
        """Запустить фоновый поток-воркер (однократно)."""
        if self._started:
            return
        self._started = True
        self._worker = threading.Thread(target=self._flush_worker, daemon=True)
        self._worker.start()
        atexit.register(self.shutdown)

    def shutdown(self) -> None:
        """Финальный сброс буфера и остановка воркера."""
        self._stop_event.set()
        self._flush()

    def enqueue(self, entry: dict) -> None:
        """
        Добавить событие в буфер.

        Если буфер заполнен — немедленный сброс.
        Потокобезопасно.
        """
        cfg = get_config()
        if not cfg["enabled"]:
            return

        with self._lock:
            self._buffer.append(entry)
            should_flush = len(self._buffer) >= cfg["buffer_size"]

        if should_flush:
            self._flush()

    def _flush(self) -> None:
        """Отправить накопленные события на сервер."""
        cfg = get_config()
        with self._lock:
            if not self._buffer:
                return
            batch = self._buffer
            self._buffer = []

        try:
            self._send_batch(batch, cfg)
        except Exception:
            # Never-throw: ошибки сети глотаются
            logger.debug("revizor_sdk: batch send failed, %d events dropped", len(batch))

    def _flush_worker(self) -> None:
        """Фоновый поток: принудительный сброс каждые flush_interval_ms."""
        while not self._stop_event.wait(timeout=_get_flush_interval_sec()):
            self._flush()

    @staticmethod
    def _send_batch(batch: list[dict], cfg: dict) -> None:
        """
        Отправить пакет событий через HTTP POST /api/v1/trace/log/batch.

        Fire-and-forget: ответ сервера не читается.
        """
        payload = json.dumps({"events": batch}).encode("utf-8")
        req = urllib.request.Request(
            cfg["endpoint"].rstrip("/") + "/api/v1/trace/log/batch",
            data=payload,
            headers={
                "Content-Type": "application/json",
            },
            method="POST",
        )

        # Передать session_id из первого события пакета (REV-016)
        if batch:
            sid = batch[0].get("session_id")
            if sid:
                req.add_header("X-Revizor-Session", sid)

        if cfg["api_key"]:
            req.add_header("Authorization", f"Bearer {cfg['api_key']}")

        # Fire-and-forget: ответ не читаем
        try:
            urllib.request.urlopen(req, timeout=5)
        except Exception:
            pass  # Never-throw


def _get_flush_interval_sec() -> float:
    """Интервал сброса в секундах (из конфига)."""
    cfg = get_config()
    return cfg["flush_interval_ms"] / 1000.0


# Глобальный синглтон буфера
_buffer: Optional[BatchBuffer] = None
_buffer_lock = threading.Lock()


def get_buffer() -> BatchBuffer:
    """Вернуть глобальный экземпляр BatchBuffer (ленивая инициализация)."""
    global _buffer
    if _buffer is None:
        with _buffer_lock:
            if _buffer is None:
                _buffer = BatchBuffer()
                _buffer.start()
    return _buffer


def flush() -> None:
    """Принудительно сбросить буфер (для тестов)."""
    buf = get_buffer()
    buf._flush()
