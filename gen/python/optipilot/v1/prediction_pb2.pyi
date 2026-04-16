import datetime

from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class ScalingMode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    SCALING_MODE_UNSPECIFIED: _ClassVar[ScalingMode]
    SCALING_MODE_PREDICTIVE: _ClassVar[ScalingMode]
    SCALING_MODE_CONSERVATIVE: _ClassVar[ScalingMode]
    SCALING_MODE_REACTIVE: _ClassVar[ScalingMode]
SCALING_MODE_UNSPECIFIED: ScalingMode
SCALING_MODE_PREDICTIVE: ScalingMode
SCALING_MODE_CONSERVATIVE: ScalingMode
SCALING_MODE_REACTIVE: ScalingMode

class GetPredictionRequest(_message.Message):
    __slots__ = ("service_name", "recent_rps", "timestamp")
    SERVICE_NAME_FIELD_NUMBER: _ClassVar[int]
    RECENT_RPS_FIELD_NUMBER: _ClassVar[int]
    TIMESTAMP_FIELD_NUMBER: _ClassVar[int]
    service_name: str
    recent_rps: _containers.RepeatedScalarFieldContainer[float]
    timestamp: _timestamp_pb2.Timestamp
    def __init__(self, service_name: _Optional[str] = ..., recent_rps: _Optional[_Iterable[float]] = ..., timestamp: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class GetPredictionResponse(_message.Message):
    __slots__ = ("service_name", "rps_p50", "rps_p90", "recommended_replicas", "scaling_mode", "confidence_score", "reason", "model_version")
    SERVICE_NAME_FIELD_NUMBER: _ClassVar[int]
    RPS_P50_FIELD_NUMBER: _ClassVar[int]
    RPS_P90_FIELD_NUMBER: _ClassVar[int]
    RECOMMENDED_REPLICAS_FIELD_NUMBER: _ClassVar[int]
    SCALING_MODE_FIELD_NUMBER: _ClassVar[int]
    CONFIDENCE_SCORE_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    MODEL_VERSION_FIELD_NUMBER: _ClassVar[int]
    service_name: str
    rps_p50: float
    rps_p90: float
    recommended_replicas: int
    scaling_mode: ScalingMode
    confidence_score: float
    reason: str
    model_version: str
    def __init__(self, service_name: _Optional[str] = ..., rps_p50: _Optional[float] = ..., rps_p90: _Optional[float] = ..., recommended_replicas: _Optional[int] = ..., scaling_mode: _Optional[_Union[ScalingMode, str]] = ..., confidence_score: _Optional[float] = ..., reason: _Optional[str] = ..., model_version: _Optional[str] = ...) -> None: ...

class GetModelStatusRequest(_message.Message):
    __slots__ = ("service_name",)
    SERVICE_NAME_FIELD_NUMBER: _ClassVar[int]
    service_name: str
    def __init__(self, service_name: _Optional[str] = ...) -> None: ...

class GetModelStatusResponse(_message.Message):
    __slots__ = ("service_name", "model_version", "current_mape", "scaling_mode", "last_trained_at", "last_recalibrated_at", "training_data_points")
    SERVICE_NAME_FIELD_NUMBER: _ClassVar[int]
    MODEL_VERSION_FIELD_NUMBER: _ClassVar[int]
    CURRENT_MAPE_FIELD_NUMBER: _ClassVar[int]
    SCALING_MODE_FIELD_NUMBER: _ClassVar[int]
    LAST_TRAINED_AT_FIELD_NUMBER: _ClassVar[int]
    LAST_RECALIBRATED_AT_FIELD_NUMBER: _ClassVar[int]
    TRAINING_DATA_POINTS_FIELD_NUMBER: _ClassVar[int]
    service_name: str
    model_version: str
    current_mape: float
    scaling_mode: ScalingMode
    last_trained_at: _timestamp_pb2.Timestamp
    last_recalibrated_at: _timestamp_pb2.Timestamp
    training_data_points: int
    def __init__(self, service_name: _Optional[str] = ..., model_version: _Optional[str] = ..., current_mape: _Optional[float] = ..., scaling_mode: _Optional[_Union[ScalingMode, str]] = ..., last_trained_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., last_recalibrated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., training_data_points: _Optional[int] = ...) -> None: ...

class AllServicesStatusRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class AllServicesStatusResponse(_message.Message):
    __slots__ = ("services",)
    SERVICES_FIELD_NUMBER: _ClassVar[int]
    services: _containers.RepeatedCompositeFieldContainer[GetModelStatusResponse]
    def __init__(self, services: _Optional[_Iterable[_Union[GetModelStatusResponse, _Mapping]]] = ...) -> None: ...

class ServiceMetric(_message.Message):
    __slots__ = ("service_name", "rps", "avg_latency_ms", "p99_latency_ms", "active_connections", "cpu_usage_percent", "memory_usage_mb", "error_rate", "timestamp")
    SERVICE_NAME_FIELD_NUMBER: _ClassVar[int]
    RPS_FIELD_NUMBER: _ClassVar[int]
    AVG_LATENCY_MS_FIELD_NUMBER: _ClassVar[int]
    P99_LATENCY_MS_FIELD_NUMBER: _ClassVar[int]
    ACTIVE_CONNECTIONS_FIELD_NUMBER: _ClassVar[int]
    CPU_USAGE_PERCENT_FIELD_NUMBER: _ClassVar[int]
    MEMORY_USAGE_MB_FIELD_NUMBER: _ClassVar[int]
    ERROR_RATE_FIELD_NUMBER: _ClassVar[int]
    TIMESTAMP_FIELD_NUMBER: _ClassVar[int]
    service_name: str
    rps: float
    avg_latency_ms: float
    p99_latency_ms: float
    active_connections: int
    cpu_usage_percent: float
    memory_usage_mb: float
    error_rate: float
    timestamp: _timestamp_pb2.Timestamp
    def __init__(self, service_name: _Optional[str] = ..., rps: _Optional[float] = ..., avg_latency_ms: _Optional[float] = ..., p99_latency_ms: _Optional[float] = ..., active_connections: _Optional[int] = ..., cpu_usage_percent: _Optional[float] = ..., memory_usage_mb: _Optional[float] = ..., error_rate: _Optional[float] = ..., timestamp: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class MetricsHistoryRequest(_message.Message):
    __slots__ = ("service_name", "minutes")
    SERVICE_NAME_FIELD_NUMBER: _ClassVar[int]
    MINUTES_FIELD_NUMBER: _ClassVar[int]
    service_name: str
    minutes: int
    def __init__(self, service_name: _Optional[str] = ..., minutes: _Optional[int] = ...) -> None: ...

class MetricsHistoryResponse(_message.Message):
    __slots__ = ("service_name", "data_points")
    SERVICE_NAME_FIELD_NUMBER: _ClassVar[int]
    DATA_POINTS_FIELD_NUMBER: _ClassVar[int]
    service_name: str
    data_points: _containers.RepeatedCompositeFieldContainer[ServiceMetric]
    def __init__(self, service_name: _Optional[str] = ..., data_points: _Optional[_Iterable[_Union[ServiceMetric, _Mapping]]] = ...) -> None: ...

class IngestMetricsRequest(_message.Message):
    __slots__ = ("metrics",)
    METRICS_FIELD_NUMBER: _ClassVar[int]
    metrics: _containers.RepeatedCompositeFieldContainer[ServiceMetric]
    def __init__(self, metrics: _Optional[_Iterable[_Union[ServiceMetric, _Mapping]]] = ...) -> None: ...

class IngestMetricsResponse(_message.Message):
    __slots__ = ("accepted_count", "message")
    ACCEPTED_COUNT_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    accepted_count: int
    message: str
    def __init__(self, accepted_count: _Optional[int] = ..., message: _Optional[str] = ...) -> None: ...

class TriggerRetrainRequest(_message.Message):
    __slots__ = ("service_name",)
    SERVICE_NAME_FIELD_NUMBER: _ClassVar[int]
    service_name: str
    def __init__(self, service_name: _Optional[str] = ...) -> None: ...

class TriggerRetrainResponse(_message.Message):
    __slots__ = ("success", "new_model_version", "new_mape", "message")
    SUCCESS_FIELD_NUMBER: _ClassVar[int]
    NEW_MODEL_VERSION_FIELD_NUMBER: _ClassVar[int]
    NEW_MAPE_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    success: bool
    new_model_version: str
    new_mape: float
    message: str
    def __init__(self, success: bool = ..., new_model_version: _Optional[str] = ..., new_mape: _Optional[float] = ..., message: _Optional[str] = ...) -> None: ...
