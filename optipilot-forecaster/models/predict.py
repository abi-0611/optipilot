from pydantic import BaseModel, Field

class PredictRequest(BaseModel):
    """Incoming payload for one service inference call."""

    service_name: str = Field(min_length=1)
    recent_rps: list[float] | None = Field(
        default=None,
        description="Last N minute-level RPS values (oldest first, newest last).",
    )


class PredictResponse(BaseModel):
    """Response payload returned to the frontend."""

    service_name: str
    rps_p50: float
    rps_p90: float
    recommended_replicas: int
    scaling_mode: str
    reason: str