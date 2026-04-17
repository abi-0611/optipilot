#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
RESULTS_DIR="${ROOT_DIR}/results"
TARGET_FILE="${TARGET_FILE:-${ROOT_DIR}/targets/api-gateway.txt}"

mkdir -p "${RESULTS_DIR}"

timestamp() {
  date -u +"%Y%m%dT%H%M%SZ"
}

run_phase() {
  local name="$1"
  local rate="$2"
  local duration="$3"
  local stamp
  stamp="$(timestamp)"
  local bin_file="${RESULTS_DIR}/${stamp}_${name}_${rate}rps.bin"
  local txt_file="${RESULTS_DIR}/${stamp}_${name}_${rate}rps.txt"

  echo "[ramp] phase=${name} rate=${rate}/s duration=${duration} target=${TARGET_FILE}"
  vegeta attack -rate="${rate}" -duration="${duration}" -targets="${TARGET_FILE}" \
    | tee "${bin_file}" \
    | vegeta report \
    | tee "${txt_file}"
}

START_RPS=10
END_RPS=200
RAMP_UP_MIN=10
STEADY_SEC=120
RAMP_DOWN_STEPS=10
RAMP_DOWN_STEP_SEC=30

for ((i=0; i<RAMP_UP_MIN; i++)); do
  rate=$((START_RPS + (END_RPS - START_RPS) * i / (RAMP_UP_MIN - 1)))
  run_phase "ramp_up_${i}" "${rate}" "60s"
done

run_phase "steady" "${END_RPS}" "${STEADY_SEC}s"

for ((i=RAMP_DOWN_STEPS-1; i>=0; i--)); do
  rate=$((START_RPS + (END_RPS - START_RPS) * i / (RAMP_DOWN_STEPS - 1)))
  run_phase "ramp_down_${i}" "${rate}" "${RAMP_DOWN_STEP_SEC}s"
done

echo "[ramp] complete"

