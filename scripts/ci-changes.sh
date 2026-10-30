#!/bin/sh
# Usage: ci-changes.sh [all] <changed-paths
#
# Decides which test jobs of the CI workflow a change needs. Reads the
# changed paths, one per line, relative to the root of the repository.
# Writes go=, web= and e2e=, each true or false, to $GITHUB_OUTPUT:
#
#   go   the Go unit and integration tests (test-server, test-integration)
#   web  the web unit tests (test-web)
#   e2e  the images and the end-to-end tests (e2e)
#
# A path that this script does not know turns on every job, so a new file
# outside the known folders never skips a test. With the argument all, it
# reads nothing and turns on every job. The lint job always runs and is not
# in the output.
set -eu

go=false web=false e2e=false
everything() { go=true web=true e2e=true; }

if [ "$#" -gt 1 ] || { [ "$#" -eq 1 ] && [ "$1" != all ]; }; then
	echo "usage: $0 [all] <changed-paths" >&2
	exit 2
fi

if [ "$#" -eq 1 ]; then
	everything
else
	while IFS= read -r path || [ -n "$path" ]; do
		case $path in
		"") ;;
		# A change to the tools, the build or CI itself can break any job.
		.tool-versions | Makefile | renovate.json | .github/workflows/* | scripts/ci-changes.sh | scripts/ci-ok.sh)
			everything
			;;
		# The Go engine and the web engine both test against these cases.
		testdata/engine/*) go=true web=true e2e=true ;;
		# The fixture server of the end-to-end stack serves testdata.
		server/* | testdata/*) go=true e2e=true ;;
		# The web image installs from the workspace lockfile.
		web/* | pnpm-lock.yaml | pnpm-workspace.yaml | package.json) web=true e2e=true ;;
		# A Go test compares its Postgres image with this file.
		docker-compose.yml) go=true e2e=true ;;
		e2e/* | docker-compose.*.yml | scripts/e2e.sh) e2e=true ;;
		# The integration tests run the SQL of deploy/sql.
		deploy/sql/* | tools/price-fixtures/* | tools/sec-fixtures/*) go=true ;;
		# Only the lint job reads these.
		*.md | docs/* | prototype/* | .claude/* | .github/* | deploy/fly/* | tools/engine-oracle/* | scripts/deploy-gate*.sh | scripts/ci-*_test.sh | .editorconfig | .env.example | .gitattributes | .gitignore | .prettierignore | .prettierrc.json) ;;
		*) everything ;;
		esac
	done
fi

out=${GITHUB_OUTPUT:-/dev/stdout}
{
	echo "go=$go"
	echo "web=$web"
	echo "e2e=$e2e"
} >>"$out"
echo "Jobs for this change: go=$go web=$web e2e=$e2e"
