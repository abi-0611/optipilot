"""
Entry point: python -m forecaster   or   uv run forecaster

Loads config, initialises storage, starts the gRPC server, and blocks
until SIGINT or SIGTERM, then shuts down gracefully.
"""

from __future__ import annotations

import asyncio
import logging
import os
import signal
import sys

# Add gen/python to path so we can import optipilot.v1
sys.path.insert(0, os.path.join(os.path.dirname(__file__), '../../..', 'gen', 'python'))

from pathlib import Path

from forecaster.config import load_config
from forecaster.ml.inference import InferenceEngine
from forecaster.ml.scheduler import ForecasterScheduler
from forecaster.ml.trainer import Trainer
from forecaster.server.grpc_server import serve
from forecaster.storage.metrics import MetricsStore
from forecaster.storage.registry import ModelRegistry


# ---------------------------------------------------------------------------
# Structured logging
# ---------------------------------------------------------------------------

class _JsonFormatter(logging.Formatter):
    """
    Minimal JSON log formatter — one JSON object per line, no extra deps.
    Handles the 'extra' dict fields passed to log calls.
    """

    def format(self, record: logging.LogRecord) -> str:
        import json
        import traceback

        payload: dict = {
            "ts": self.formatTime(record, datefmt="%Y-%m-%dT%H:%M:%S"),
            "level": record.levelname,
            "logger": record.name,
            "msg": record.getMessage(),
        }

        # Merge any extra fields attached via logger.info("msg", extra={...})
        skip = logging.LogRecord.__dict__.keys() | {
            "message", "asctime", "args", "msg",
            "levelname", "levelno", "pathname", "filename",
            "module", "exc_info", "exc_text", "stack_info",
            "lineno", "funcName", "created", "msecs",
            "relativeCreated", "thread", "threadName",
            "processName", "process", "taskName", "name",
        }
        for key, val in record.__dict__.items():
            if key not in skip:
                payload[key] = val

        if record.exc_info:
            payload["exc"] = traceback.format_exception(*record.exc_info)

        return json.dumps(payload, default=str)


def _setup_logging(level: str, fmt: str) -> None:
    handler = logging.StreamHandler(sys.stdout)
    if fmt == "json":
        handler.setFormatter(_JsonFormatter())
    else:
        handler.setFormatter(
            logging.Formatter("%(asctime)s %(levelname)s %(name)s %(message)s")
        )
    root = logging.getLogger()
    root.setLevel(getattr(logging, level, logging.INFO))
    root.addHandler(handler)


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

async def main_async() -> None:
    config_path = os.getenv("OPTIPILOT_FC_CONFIG", "forecaster.yaml")
    config = load_config(config_path)

    _setup_logging(config.logging.level, config.logging.format)
    logger = logging.getLogger("forecaster")

    registry = ModelRegistry(config.storage.registry_db)
    await registry.initialize()

    metrics = MetricsStore(config.storage.metrics_db)
    await metrics.initialize()

    models_dir = Path(config.training.models_dir)
    models_dir.mkdir(parents=True, exist_ok=True)
    trainer = Trainer(config, registry, models_dir, logger)
    inference = InferenceEngine(registry, models_dir, config, logger)
    scheduler = ForecasterScheduler(
        config=config,
        metrics_store=metrics,
        registry=registry,
        trainer=trainer,
        inference=inference,
        logger=logger,
    )

    logger.info(
        "forecaster starting",
        extra={
            "grpc_port": config.server.port,
            "registry_db": config.storage.registry_db,
            "metrics_db": config.storage.metrics_db,
            "models_dir": str(models_dir.resolve()),
        },
    )

    stop_event = asyncio.Event()
    loop = asyncio.get_running_loop()

    def _handle_signal() -> None:
        logger.info("shutdown signal received")
        stop_event.set()

    for sig in (signal.SIGINT, signal.SIGTERM):
        loop.add_signal_handler(sig, _handle_signal)

    await scheduler.start()
    server = await serve(
        config,
        registry,
        metrics,
        trainer,
        inference,
        logger,
        scheduler=scheduler,
    )

    await stop_event.wait()

    logger.info("shutting down")
    # Give in-flight RPCs up to 5 seconds to complete
    await server.stop(grace=5)
    await scheduler.shutdown()
    await registry.close()
    await metrics.close()
    logger.info("shutdown complete")


def main() -> None:
    asyncio.run(main_async())


if __name__ == "__main__":
    main()
