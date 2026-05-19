#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
RESULTS_DIR="${SCRIPT_DIR}/results"
SCENARIO="${SCRIPT_DIR}/scenarios/realistic.sh"

API_GATEWAY_BASE="${API_GATEWAY_BASE:-http://localhost:8081}"
ORDER_SERVICE_BASE="${ORDER_SERVICE_BASE:-http://localhost:8082}"
PAYMENT_SERVICE_BASE="${PAYMENT_SERVICE_BASE:-http://localhost:8083}"

mkdir -p "${RESULTS_DIR}"
STAMP="$(date -u +"%Y%m%dT%H%M%SZ")"
LOG_FILE="${RESULTS_DIR}/${STAMP}_run-all.log"

require_health() {
	local name="$1"
	local url="$2"
	if ! curl -fsS --max-time 2 "${url}" >/dev/null; then
		echo "[run-all] ERROR: ${name} is unreachable at ${url}"
		echo "[run-all] Hint: start service port-forwarding first (make port-forward-services)."
		exit 1
	fi
}

echo "[run-all] starting realistic scenario"
echo "[run-all] writing combined log to ${LOG_FILE}"
echo "[run-all] target endpoints:"
echo "  - api-gateway: ${API_GATEWAY_BASE}"
echo "  - order-service: ${ORDER_SERVICE_BASE}"
echo "  - payment-service: ${PAYMENT_SERVICE_BASE}"

require_health "api-gateway" "${API_GATEWAY_BASE}/health"
require_health "order-service" "${ORDER_SERVICE_BASE}/health"
require_health "payment-service" "${PAYMENT_SERVICE_BASE}/health"

echo "[run-all] load profile summary (realistic):"
echo "  - api-gateway: ramp 10->200 rps over 10m, then 200 rps for 2m, then 80 rps for 4m"
echo "  - order-service: 50 rps for 4m, spike 400 rps for 2m, then 50 rps for 6m"
echo "  - payment-service: 35 rps for 6m, 70 rps for 2m, 35 rps for 6m, 70 rps for 2m"

API_GATEWAY_BASE="${API_GATEWAY_BASE}" \
ORDER_SERVICE_BASE="${ORDER_SERVICE_BASE}" \
PAYMENT_SERVICE_BASE="${PAYMENT_SERVICE_BASE}" \
bash "${SCENARIO}" 2>&1 | tee "${LOG_FILE}"

echo "[run-all] finished"
