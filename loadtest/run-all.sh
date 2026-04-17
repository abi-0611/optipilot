#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
RESULTS_DIR="${SCRIPT_DIR}/results"
SCENARIO="${SCRIPT_DIR}/scenarios/realistic.sh"

mkdir -p "${RESULTS_DIR}"
STAMP="$(date -u +"%Y%m%dT%H%M%SZ")"
LOG_FILE="${RESULTS_DIR}/${STAMP}_run-all.log"

echo "[run-all] starting realistic scenario"
echo "[run-all] writing combined log to ${LOG_FILE}"

bash "${SCENARIO}" 2>&1 | tee "${LOG_FILE}"

echo "[run-all] finished"

