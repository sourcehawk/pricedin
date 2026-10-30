#!/usr/bin/env bash
# Runs the end-to-end tests against a Compose stack of their own:
#
#   scripts/e2e.sh        start and seed the stack, run Playwright, remove the stack
#   scripts/e2e.sh up     start and seed the stack, and keep it
#   scripts/e2e.sh down   remove the stack of `up`
#
# The stack uses the images that `make images` built. On a failure, the
# stack's logs go to e2e/compose.log.
#
# The host ports come from PRICEDIN_WEB_PORT, PRICEDIN_API_PORT,
# PRICEDIN_METRICS_PORT and PRICEDIN_DB_PORT; the Makefile sets them apart
# from the ports of `make up`. The owner account comes from
# PRICEDIN_E2E_OWNER_EMAIL and PRICEDIN_E2E_OWNER_PASSWORD, which the tests
# read too.
set -euo pipefail

project=${PRICEDIN_E2E_PROJECT:?set PRICEDIN_E2E_PROJECT to the Compose project of the test stack}
# The teardown removes volumes, so it must never run on the stack of `make up`.
if [ "$project" = pricedin ]; then
	echo "e2e: PRICEDIN_E2E_PROJECT must not be pricedin, the project of make up" >&2
	exit 2
fi
# The stack reads no .env: a value there is for the stack of `make up`, and
# the tests must not depend on the machine that runs them.
export PRICEDIN_ENV_FILE=/dev/null
compose="${COMPOSE:-docker compose} --env-file /dev/null -f docker-compose.e2e.yml --project-name $project"
docker=${DOCKER:-docker}
pnpm=${PNPM:-pnpm}
server_image=${PRICEDIN_SERVER_IMAGE:?set PRICEDIN_SERVER_IMAGE to the server image}
: "${PRICEDIN_E2E_OWNER_EMAIL:?set PRICEDIN_E2E_OWNER_EMAIL to the owner email of the test stack}"
: "${PRICEDIN_E2E_OWNER_PASSWORD:?set PRICEDIN_E2E_OWNER_PASSWORD to the owner password of the test stack}"
# The jobs that load the fixture companies, in order; the same as `make seed`.
seed_jobs=(sec-tickers sec-bulk price-reference prices-backfill "fundamentals-rebuild all")

down() {
	echo "+ $compose down --volumes"
	$compose down --volumes
}

logs() {
	echo "e2e: writing the stack's logs to e2e/compose.log" >&2
	$compose logs --no-color >e2e/compose.log 2>&1 || true
}

up() {
	export PRICEDIN_OWNER_EMAIL=$PRICEDIN_E2E_OWNER_EMAIL
	PRICEDIN_OWNER_PASSWORD_HASH=$(printf '%s\n' "$PRICEDIN_E2E_OWNER_PASSWORD" |
		$docker run --rm -i "$server_image" auth hash-password --stdin)
	export PRICEDIN_OWNER_PASSWORD_HASH
	# A new secret for each stack: the tests never need to know it.
	PRICEDIN_PROXY_SECRET=$(od -An -N32 -tx1 /dev/urandom | tr -d ' \n')
	export PRICEDIN_PROXY_SECRET

	# Not the scheduler: on an empty database it runs the daily job at once,
	# under the job names of the seed, while the seed and the tests run.
	echo "+ $compose up --detach --no-build --wait fixtures server web"
	$compose up --detach --no-build --wait fixtures server web
	for job in "${seed_jobs[@]}"; do
		echo "+ $compose exec -T server /pricedin jobs run --wait $job"
		# shellcheck disable=SC2086 # a job and its arguments are separate words
		$compose exec -T server /pricedin jobs run --wait $job
	done
}

run_tests() {
	echo "+ $pnpm --filter @pricedin/e2e test (web $PRICEDIN_E2E_BASE_URL, api $PRICEDIN_E2E_API_URL)"
	$pnpm --filter @pricedin/e2e test
}

case "${1:-all}" in
up)
	on_exit() {
		status=$?
		if [ "$status" -ne 0 ]; then
			logs
		fi
		exit "$status"
	}
	trap on_exit EXIT
	up
	;;
down)
	down
	;;
all)
	teardown() {
		status=$?
		if [ "$status" -ne 0 ]; then
			echo "e2e: failed with exit code $status" >&2
			logs
		fi
		down
		exit "$status"
	}
	trap teardown EXIT
	up
	run_tests
	;;
*)
	echo "usage: scripts/e2e.sh [up|down|all]" >&2
	exit 2
	;;
esac
