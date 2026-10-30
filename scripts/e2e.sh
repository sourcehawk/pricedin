#!/usr/bin/env bash
# Start a Compose stack from the images that `make images` built, run the
# Playwright suite against it, and remove the stack with its volumes, also
# when a step fails. On a failure, the stack's logs go to e2e/compose.log.
#
# The stack runs as the Compose project PRICEDIN_E2E_PROJECT. The host ports
# come from PRICEDIN_WEB_PORT, PRICEDIN_API_PORT, PRICEDIN_METRICS_PORT and
# PRICEDIN_DB_PORT; the Makefile sets them apart from the ports of `make up`.
set -euo pipefail

project=${PRICEDIN_E2E_PROJECT:?set PRICEDIN_E2E_PROJECT to the Compose project of the test stack}
# The teardown removes volumes, so it must never run on the stack of `make up`.
if [ "$project" = pricedin ]; then
	echo "e2e: PRICEDIN_E2E_PROJECT must not be pricedin, the project of make up" >&2
	exit 2
fi
compose="${COMPOSE:-docker compose} --project-name $project"
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
