#!/usr/bin/env bash
# Regenerate Python proto stubs into the shared repository output:
#   ../gen/python/optipilot/v1/
#
# Forecaster imports these directly via `from optipilot.v1 import ...`.
#
# Usage: bash scripts/sync_proto.sh   (from the forecaster/ directory)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

echo "Regenerating Python proto stubs in ${REPO_ROOT}/gen/python ..."
make -C "${REPO_ROOT}/proto" python
echo "Done. Stubs available at: ${REPO_ROOT}/gen/python/optipilot/v1"
