#!/usr/bin/env bash
# Start the Compose stack from the images that `make images` built, run the
# Playwright suite against it, and stop the stack again, also when a step
# fails. On a failure, the stack's logs go to e2e/compose.log.
set -euo pipefail

compose=${COMPOSE:-docker compose}
pnpm=${PNPM:-pnpm}

teardown() {
	status=$?
	if [ "$status" -ne 0 ]; then
		echo "e2e: failed with exit code $status; writing the stack's logs to e2e/compose.log" >&2
		$compose logs --no-color >e2e/compose.log 2>&1 || true
	fi
	echo "+ $compose down --volumes"
	$compose down --volumes
	exit "$status"
}
trap teardown EXIT

echo "+ $compose up --detach --no-build --wait"
$compose up --detach --no-build --wait

echo "+ $pnpm --filter @pricedin/e2e test (web $PRICEDIN_E2E_BASE_URL, api $PRICEDIN_E2E_API_URL)"
$pnpm --filter @pricedin/e2e test
