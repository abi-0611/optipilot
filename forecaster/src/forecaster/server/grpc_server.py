"""
gRPC server lifecycle: create, start, return handle for graceful shutdown.
"""

from __future__ import annotations

import logging
from concurrent import futures

import grpc
import grpc.aio

from forecaster.config import Config
from forecaster.ml.inference import InferenceEngine
from forecaster.ml.scheduler import ForecasterScheduler
from forecaster.ml.trainer import Trainer
from optipilot.v1 import prediction_pb2_grpc
from forecaster.server.service import OptiPilotServiceImpl
from forecaster.storage.metrics import MetricsStore
from forecaster.storage.registry import ModelRegistry


async def serve(
    config: Config,
    registry: ModelRegistry,
    metrics: MetricsStore,
    trainer: Trainer,
    inference: InferenceEngine,
    logger: logging.Logger,
    scheduler: ForecasterScheduler | None = None,
) -> grpc.aio.Server:
    """
    Create, configure, and start the gRPC server.
    Returns the server object so the caller can stop it on shutdown.
    """
    server = grpc.aio.server(
        futures.ThreadPoolExecutor(max_workers=config.server.max_workers)
    )

    service = OptiPilotServiceImpl(
        registry, metrics, trainer, inference, config, logger, scheduler=scheduler
    )
    prediction_pb2_grpc.add_OptiPilotServiceServicer_to_server(service, server)

    address = f"{config.server.host}:{config.server.port}"
    server.add_insecure_port(address)

    await server.start()
    logger.info("grpc server listening", extra={"address": address})

    return server
