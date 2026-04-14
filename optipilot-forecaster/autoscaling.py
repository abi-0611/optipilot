import math
import time
from typing import Any

# Capacity assumptions per pod.
POD_RPS_CAPACITY = 100
TARGET_UTILIZATION = 0.70
HEADROOM_RATIO = 0.20

# Replica boundaries.
MIN_REPLICAS = 2
MAX_REPLICAS = 20

# Cooldown windows in seconds.
SCALE_UP_COOLDOWN_SECONDS = 2 * 60
SCALE_DOWN_COOLDOWN_SECONDS = 10 * 60

# In-memory autoscaler state keyed by service name.
_SCALER_STATE: dict[str, dict[str, Any]] = {}


def compute_replicas(rps_p90: float, service_name: str) -> int:
    """Compute recommended replicas from p90 forecast with cooldown-aware scaling."""
    if not service_name:
        raise ValueError("service_name must be a non-empty string.")

    # Initialize state for unseen services.
    state = _SCALER_STATE.setdefault(
        service_name,
        {
            "current_replicas": MIN_REPLICAS,
            "last_scale_at": None,
        },
    )

    # Convert predicted demand into required pods at target utilization.
    predicted_rps = max(0.0, float(rps_p90))
    effective_rps_per_pod = POD_RPS_CAPACITY * TARGET_UTILIZATION
    raw_replicas = math.ceil(predicted_rps / effective_rps_per_pod) if predicted_rps > 0 else 0

    # Add safety headroom, then clamp to allowed min/max range.
    with_headroom = math.ceil(raw_replicas * (1 + HEADROOM_RATIO))
    desired_replicas = max(MIN_REPLICAS, min(MAX_REPLICAS, with_headroom))

    # Apply cooldown behavior using last successful scaling timestamp.
    current_replicas = int(state["current_replicas"])
    last_scale_at = state["last_scale_at"]
    now = time.time()
    elapsed = None if last_scale_at is None else now - float(last_scale_at)

    final_replicas = desired_replicas
    if desired_replicas > current_replicas:
        if elapsed is not None and elapsed < SCALE_UP_COOLDOWN_SECONDS:
            final_replicas = current_replicas
    elif desired_replicas < current_replicas:
        if elapsed is not None and elapsed < SCALE_DOWN_COOLDOWN_SECONDS:
            final_replicas = current_replicas

    # Persist state only when a scale action actually changes replicas.
    if final_replicas != current_replicas:
        state["current_replicas"] = final_replicas
        state["last_scale_at"] = now

    print(
        f"Service {service_name}: "
        f"rps_p90={predicted_rps:.2f} → raw_replicas={raw_replicas} "
        f"→ with_headroom={with_headroom} → final={final_replicas}"
    )

    return int(final_replicas)


def get_confidence_mode(rps_p50: float, rps_p90: float, recent_mape: float) -> tuple[str, str]:
    """Choose scaling mode based on prediction uncertainty and recent error."""
    # Relative spread between p50 and p90. Higher spread means lower confidence.
    interval_width = (float(rps_p90) - float(rps_p50)) / max(float(rps_p50), 1.0)
    recent_mape = float(recent_mape)

    # Lowest-confidence case: fall back to reactive behavior.
    if recent_mape > 0.30 or interval_width > 0.60:
        reasons = []
        if recent_mape > 0.30:
            reasons.append(f"recent_mape={recent_mape:.2f} > 0.30")
        if interval_width > 0.60:
            reasons.append(f"interval_width={interval_width:.2f} > 0.60")
        reason = " OR ".join(reasons)
        mode = "REACTIVE"

    # Medium-confidence case: be careful with step size.
    elif recent_mape > 0.20 or interval_width > 0.40:
        reasons = []
        if recent_mape > 0.20:
            reasons.append(f"recent_mape={recent_mape:.2f} > 0.20")
        if interval_width > 0.40:
            reasons.append(f"interval_width={interval_width:.2f} > 0.40")
        reason = " OR ".join(reasons)
        mode = "CONSERVATIVE"

    # High-confidence case: use full predictive scaling.
    else:
        reason = (
            f"recent_mape={recent_mape:.2f} <= 0.20 and "
            f"interval_width={interval_width:.2f} <= 0.40"
        )
        mode = "PREDICTIVE"

    print(f"Mode {mode} triggered by: {reason}")
    return mode, reason


def _run_confidence_mode_tests() -> None:
    """Minimal inline tests that cover all confidence modes."""
    mode, reason = get_confidence_mode(rps_p50=100, rps_p90=120, recent_mape=0.10)
    assert mode == "PREDICTIVE", f"Expected PREDICTIVE, got {mode} ({reason})"

    mode, reason = get_confidence_mode(rps_p50=100, rps_p90=145, recent_mape=0.18)
    assert mode == "CONSERVATIVE", f"Expected CONSERVATIVE, got {mode} ({reason})"

    mode, reason = get_confidence_mode(rps_p50=100, rps_p90=130, recent_mape=0.35)
    assert mode == "REACTIVE", f"Expected REACTIVE, got {mode} ({reason})"

    print("Confidence mode tests passed.")


if __name__ == "__main__":
    _run_confidence_mode_tests()
