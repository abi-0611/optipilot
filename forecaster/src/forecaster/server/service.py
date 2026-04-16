"""
OptiPilot gRPC service implementation.

All RPCs except IngestMetrics are stubs that return plausible placeholder
responses. IngestMetrics is fully implemented: it converts incoming proto
metrics to internal dataclasses and persists them to SQLite.
"""

from __future__ import annotations

import logging
from datetime import datetime, timezone
from typing import Optional

import grpc
from google.protobuf import timestamp_pb2

from forecaster.config import Config
from forecaster.ml.trainer import InsufficientDataError, Trainer
from forecaster.models import ModelStatus, ServiceMetric
from optipilot.v1 import prediction_pb2, prediction_pb2_grpc
from forecaster.storage.metrics import MetricsStore
from forecaster.storage.registry import ModelRegistry


# ---------------------------------------------------------------------------
# Proto <-> internal converters
# ---------------------------------------------------------------------------

def _ts_from_proto(ts: Optional[timestamp_pb2.Timestamp]) -> datetime:
    """Convert a protobuf Timestamp to a UTC-aware datetime."""
    if ts is None or (ts.seconds == 0 and ts.nanos == 0):
        return datetime.now(tz=timezone.utc)
    return datetime.fromtimestamp(ts.seconds + ts.nanos / 1e9, tz=timezone.utc)


def _ts_to_proto(dt: Optional[datetime]) -> timestamp_pb2.Timestamp:
    """Convert a datetime to a protobuf Timestamp."""
    ts = timestamp_pb2.Timestamp()
    if dt is not None:
        if dt.tzinfo is None:
            dt = dt.replace(tzinfo=timezone.utc)
        ts.seconds = int(dt.timestamp())
        ts.nanos = int((dt.timestamp() % 1) * 1e9)
    return ts


def _proto_to_metric(m: prediction_pb2.ServiceMetric) -> ServiceMetric:
    return ServiceMetric(
        service_name=m.service_name,
        rps=m.rps,
        avg_latency_ms=m.avg_latency_ms,
        p99_latency_ms=m.p99_latency_ms,
        active_connections=m.active_connections,
        cpu_usage_percent=m.cpu_usage_percent,
        memory_usage_mb=m.memory_usage_mb,
        error_rate=m.error_rate,
        timestamp=_ts_from_proto(m.timestamp),
    )


def _metric_to_proto(m: ServiceMetric) -> prediction_pb2.ServiceMetric:
    return prediction_pb2.ServiceMetric(
        service_name=m.service_name,
        rps=m.rps,
        avg_latency_ms=m.avg_latency_ms,
        p99_latency_ms=m.p99_latency_ms,
        active_connections=m.active_connections,
        cpu_usage_percent=m.cpu_usage_percent,
        memory_usage_mb=m.memory_usage_mb,
        error_rate=m.error_rate,
        timestamp=_ts_to_proto(m.timestamp),
    )


def _status_to_proto(s: ModelStatus) -> prediction_pb2.GetModelStatusResponse:
    _mode_map = {
        "PREDICTIVE": prediction_pb2.SCALING_MODE_PREDICTIVE,
        "CONSERVATIVE": prediction_pb2.SCALING_MODE_CONSERVATIVE,
        "REACTIVE": prediction_pb2.SCALING_MODE_REACTIVE,
    }
    return prediction_pb2.GetModelStatusResponse(
        service_name=s.service_name,
        model_version=s.model_version,
        current_mape=s.current_mape,
        scaling_mode=_mode_map.get(s.scaling_mode, prediction_pb2.SCALING_MODE_REACTIVE),
        last_trained_at=_ts_to_proto(s.last_trained_at),
        last_recalibrated_at=_ts_to_proto(s.last_recalibrated_at),
        training_data_points=s.training_data_points,
    )


# ---------------------------------------------------------------------------
# Service implementation
# ---------------------------------------------------------------------------

class OptiPilotServiceImpl(prediction_pb2_grpc.OptiPilotServiceServicer):
    def __init__(
        self,
        registry: ModelRegistry,
        metrics: MetricsStore,
        trainer: Trainer,
        config: Config,
        logger: logging.Logger,
    ) -> None:
        self._registry = registry
        self._metrics = metrics
        self._trainer = trainer
        self._config = config
        self._log = logger

    async def GetPrediction(
        self, request: prediction_pb2.GetPredictionRequest, context: grpc.aio.ServicerContext
    ) -> prediction_pb2.GetPredictionResponse:
        try:
            recent_rps = list(request.recent_rps)
            last_rps = recent_rps[-1] if recent_rps else 0.0
            rps_p90 = last_rps * 1.2
            recommended = max(2, int(rps_p90 / 100) + 1)

            self._log.info(
                "GetPrediction",
                extra={
                    "service": request.service_name,
                    "recent_rps_count": len(recent_rps),
                    "last_rps": last_rps,
                },
            )

            return prediction_pb2.GetPredictionResponse(
                service_name=request.service_name,
                rps_p50=last_rps,
                rps_p90=rps_p90,
                recommended_replicas=recommended,
                scaling_mode=prediction_pb2.SCALING_MODE_REACTIVE,
                confidence_score=0.0,
                reason="stub: ML model not implemented yet",
                model_version="stub-v0",
            )
        except Exception as exc:
            self._log.exception("GetPrediction failed", exc_info=exc)
            await context.abort(grpc.StatusCode.INTERNAL, str(exc))

    async def GetModelStatus(
        self, request: prediction_pb2.GetModelStatusRequest, context: grpc.aio.ServicerContext
    ) -> prediction_pb2.GetModelStatusResponse:
        try:
            count = await self._metrics.get_count(request.service_name)
            self._log.info(
                "GetModelStatus",
                extra={"service": request.service_name, "data_points": count},
            )
            return prediction_pb2.GetModelStatusResponse(
                service_name=request.service_name,
                model_version="stub-v0",
                current_mape=0.0,
                scaling_mode=prediction_pb2.SCALING_MODE_REACTIVE,
                training_data_points=count,
            )
        except Exception as exc:
            self._log.exception("GetModelStatus failed", exc_info=exc)
            await context.abort(grpc.StatusCode.INTERNAL, str(exc))

    async def IngestMetrics(
        self, request: prediction_pb2.IngestMetricsRequest, context: grpc.aio.ServicerContext
    ) -> prediction_pb2.IngestMetricsResponse:
        """Fully implemented: converts and persists all incoming metrics."""
        try:
            internal = [_proto_to_metric(m) for m in request.metrics]
            count = await self._metrics.insert_batch(internal)
            self._log.info(
                "IngestMetrics",
                extra={"received": len(request.metrics), "stored": count},
            )
            return prediction_pb2.IngestMetricsResponse(
                accepted_count=count,
                message=f"stored {count} metric points",
            )
        except Exception as exc:
            self._log.exception("IngestMetrics failed", exc_info=exc)
            await context.abort(grpc.StatusCode.INTERNAL, str(exc))

    async def GetAllServicesStatus(
        self,
        request: prediction_pb2.AllServicesStatusRequest,
        context: grpc.aio.ServicerContext,
    ) -> prediction_pb2.AllServicesStatusResponse:
        try:
            statuses = await self._registry.get_all_statuses()
            self._log.info(
                "GetAllServicesStatus", extra={"services_count": len(statuses)}
            )
            return prediction_pb2.AllServicesStatusResponse(
                services=[_status_to_proto(s) for s in statuses]
            )
        except Exception as exc:
            self._log.exception("GetAllServicesStatus failed", exc_info=exc)
            await context.abort(grpc.StatusCode.INTERNAL, str(exc))

    async def GetServiceMetricsHistory(
        self, request: prediction_pb2.MetricsHistoryRequest, context: grpc.aio.ServicerContext
    ) -> prediction_pb2.MetricsHistoryResponse:
        try:
            minutes = request.minutes if request.minutes > 0 else 60
            metrics_list = await self._metrics.get_recent(request.service_name, minutes)
            self._log.info(
                "GetServiceMetricsHistory",
                extra={
                    "service": request.service_name,
                    "minutes": minutes,
                    "points": len(metrics_list),
                },
            )
            return prediction_pb2.MetricsHistoryResponse(
                service_name=request.service_name,
                data_points=[_metric_to_proto(m) for m in metrics_list],
            )
        except Exception as exc:
            self._log.exception("GetServiceMetricsHistory failed", exc_info=exc)
            await context.abort(grpc.StatusCode.INTERNAL, str(exc))

    async def TriggerRetrain(
        self, request: prediction_pb2.TriggerRetrainRequest, context: grpc.aio.ServicerContext
    ) -> prediction_pb2.TriggerRetrainResponse:
        service_name = request.service_name
        if not service_name:
            return prediction_pb2.TriggerRetrainResponse(
                success=False,
                message="service_name is required",
            )

        self._log.info(
            "TriggerRetrain requested", extra={"service": service_name}
        )

        # Pull plenty of history. min_data_points is a *post-resample* count
        # at 1-minute resolution; fetch 2x the raw minutes so we still have
        # enough after feature engineering drops NaN rows.
        min_points = self._config.training.min_data_points
        fetch_minutes = max(min_points * 2, min_points + 120)
        metrics = await self._metrics.get_recent(service_name, minutes=fetch_minutes)

        if len(metrics) < min_points:
            msg = (
                f"insufficient data: have {len(metrics)}, "
                f"need at least {min_points}"
            )
            self._log.warning(
                "TriggerRetrain rejected", extra={"service": service_name, "have": len(metrics), "need": min_points}
            )
            return prediction_pb2.TriggerRetrainResponse(
                success=False, message=msg
            )

        try:
            result = await self._trainer.train_service(service_name, metrics)
        except InsufficientDataError as exc:
            self._log.warning(
                "TriggerRetrain insufficient data",
                extra={"service": service_name, "reason": str(exc)},
            )
            return prediction_pb2.TriggerRetrainResponse(
                success=False, message=f"insufficient data: {exc}"
            )
        except Exception as exc:
            self._log.exception("training failed", exc_info=exc)
            context.set_code(grpc.StatusCode.INTERNAL)
            context.set_details(f"training error: {exc}")
            return prediction_pb2.TriggerRetrainResponse(
                success=False, message=f"training error: {exc}"
            )

        return prediction_pb2.TriggerRetrainResponse(
            # success = "new model is now serving". Training without promotion
            # still counts as not-successful from the caller's perspective.
            success=result.promoted,
            new_model_version=result.version,
            new_mape=result.validation_mape_p50,
            message=(
                f"trained {result.version}: "
                f"p50 MAPE={result.validation_mape_p50:.3f}, "
                f"p90 MAPE={result.validation_mape_p90:.3f}, "
                f"previous MAPE={result.previous_mape:.3f}, "
                f"{'promoted' if result.promoted else 'not promoted (kept previous model)'}"
            ),
        )
