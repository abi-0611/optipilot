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

  echo "[periodic] phase=${name} rate=${rate}/s duration=${duration} target=${TARGET_FILE}"
  vegeta attack -rate="${rate}" -duration="${duration}" -targets="${TARGET_FILE}" \
    | tee "${bin_file}" \
    | vegeta report \
    | tee "${txt_file}"
}

BASELINE_RPS=60
PULSE_RPS=120
CYCLES=3

for ((i=1; i<=CYCLES; i++)); do
  run_phase "cycle_${i}_baseline" "${BASELINE_RPS}" "6m"
  run_phase "cycle_${i}_pulse" "${PULSE_RPS}" "2m"
done

echo "[periodic] complete"

