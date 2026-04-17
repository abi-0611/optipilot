#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
RESULTS_DIR="${ROOT_DIR}/results"
TMP_TARGET_DIR="$(mktemp -d)"

API_GATEWAY_BASE="${API_GATEWAY_BASE:-http://localhost:8081}"
ORDER_SERVICE_BASE="${ORDER_SERVICE_BASE:-http://localhost:8082}"
PAYMENT_SERVICE_BASE="${PAYMENT_SERVICE_BASE:-http://localhost:8083}"

mkdir -p "${RESULTS_DIR}"

cleanup() {
  rm -rf "${TMP_TARGET_DIR}"
}
trap cleanup EXIT

cat > "${TMP_TARGET_DIR}/api-gateway.txt" <<EOF
GET ${API_GATEWAY_BASE}/api/v1/products
GET ${API_GATEWAY_BASE}/api/v1/products/p-001
GET ${API_GATEWAY_BASE}/api/v1/search?q=electronics
EOF

cat > "${TMP_TARGET_DIR}/order-service.txt" <<EOF
POST ${ORDER_SERVICE_BASE}/api/v1/orders
Content-Type: application/json

{"customer_id":"cust-realistic","items":[{"product_id":"p-001","quantity":1,"unit_price":249.99}]}

GET ${ORDER_SERVICE_BASE}/api/v1/orders
EOF

cat > "${TMP_TARGET_DIR}/payment-service.txt" <<EOF
POST ${PAYMENT_SERVICE_BASE}/api/v1/payments/process
Content-Type: application/json

{"order_id":"ord-realistic","amount":99.99,"currency":"USD","method":"card"}

GET ${PAYMENT_SERVICE_BASE}/health
EOF

timestamp() {
  date -u +"%Y%m%dT%H%M%SZ"
}

run_attack() {
  local label="$1"
  local rate="$2"
  local duration="$3"
  local target_file="$4"
  local delay_sec="$5"

  (
    sleep "${delay_sec}"
    local stamp
    stamp="$(timestamp)"
    local bin_file="${RESULTS_DIR}/${stamp}_${label}_${rate}rps.bin"
    local txt_file="${RESULTS_DIR}/${stamp}_${label}_${rate}rps.txt"

    echo "[realistic] start label=${label} rate=${rate}/s duration=${duration} delay=${delay_sec}s targets=${target_file}"
    vegeta attack -rate="${rate}" -duration="${duration}" -targets="${target_file}" \
      | tee "${bin_file}" \
      | vegeta report \
      | tee "${txt_file}"
  ) &
}

echo "[realistic] running composite workload across api/order/payment"

# API Gateway: gradual demand climb, short peak, then cooldown.
for ((i=0; i<10; i++)); do
  rate=$((10 + (200 - 10) * i / 9))
  run_attack "api_ramp_up_${i}" "${rate}" "60s" "${TMP_TARGET_DIR}/api-gateway.txt" "$((i * 60))"
done
run_attack "api_peak_hold" "200" "120s" "${TMP_TARGET_DIR}/api-gateway.txt" 600
run_attack "api_cooldown" "80" "240s" "${TMP_TARGET_DIR}/api-gateway.txt" 720

# Order Service: baseline with a sudden spike shortly after ramp begins.
run_attack "order_baseline_pre" "50" "240s" "${TMP_TARGET_DIR}/order-service.txt" 120
run_attack "order_spike" "400" "120s" "${TMP_TARGET_DIR}/order-service.txt" 360
run_attack "order_baseline_post" "50" "360s" "${TMP_TARGET_DIR}/order-service.txt" 480

# Payment Service: periodic bursts every 8 minutes.
run_attack "payment_cycle_1_baseline" "35" "360s" "${TMP_TARGET_DIR}/payment-service.txt" 60
run_attack "payment_cycle_1_pulse" "70" "120s" "${TMP_TARGET_DIR}/payment-service.txt" 420
run_attack "payment_cycle_2_baseline" "35" "360s" "${TMP_TARGET_DIR}/payment-service.txt" 540
run_attack "payment_cycle_2_pulse" "70" "120s" "${TMP_TARGET_DIR}/payment-service.txt" 900

wait

echo "[realistic] complete"

