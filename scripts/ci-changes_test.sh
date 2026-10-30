#!/bin/sh
# Tests scripts/ci-changes.sh. `make test-scripts` runs it.
set -eu

changes="$(dirname "$0")/ci-changes.sh"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
failures=0

# check <name> <want output> <changed paths, one per line> [argument]
check() {
	name=$1 want=$2 paths=$3
	shift 3
	: >"$tmp/output"
	set +e
	printf '%s' "$paths" | GITHUB_OUTPUT="$tmp/output" "$changes" "$@" >"$tmp/log" 2>&1
	got_exit=$?
	set -e
	got=$(tr '\n' ' ' <"$tmp/output" | sed 's/ $//')
	if [ "$got_exit" -ne 0 ] || [ "$got" != "$want" ]; then
		echo "FAIL $name: exit $got_exit, output '$got'; want exit 0, output '$want'"
		sed 's/^/    /' "$tmp/log"
		failures=$((failures + 1))
	else
		echo "ok   $name"
	fi
}

none="go=false web=false e2e=false"
every="go=true web=true e2e=true"
go_e2e="go=true web=false e2e=true"
web_e2e="go=false web=true e2e=true"

check "docs only run lint only" "$none" "README.md
docs/development.md
docs/images/panel.png
prototype/pricedin.html
"
check "no changed path runs lint only" "$none" ""
check "lint settings run lint only" "$none" ".prettierrc.json
.editorconfig
.github/pull_request_template.md
tools/engine-oracle/oracle.mjs
scripts/deploy-gate.sh
"
check "server code runs the Go tests and e2e" "$go_e2e" "server/internal/engine/engine.go
"
check "a Markdown file in server is server code" "$go_e2e" "server/internal/engine/README.md
"
check "SEC and price fixtures run the Go tests and e2e" "$go_e2e" "testdata/sec/x.json
testdata/prices/massive/y.json
"
check "engine cases run every test" "$every" "testdata/engine/cases.json
"
check "web code runs the web tests and e2e" "$web_e2e" "web/src/app/page.tsx
"
check "the pnpm lockfile runs the web tests and e2e" "$web_e2e" "pnpm-lock.yaml
"
check "the root package.json runs the web tests and e2e" "$web_e2e" "package.json
"
check "end-to-end tests run e2e only" "go=false web=false e2e=true" "e2e/tests/login.spec.ts
"
check "the e2e script runs e2e only" "go=false web=false e2e=true" "scripts/e2e.sh
"
check "the e2e Compose file runs e2e only" "go=false web=false e2e=true" "docker-compose.e2e.yml
"
check "the base Compose file runs the Go tests and e2e" "$go_e2e" "docker-compose.yml
"
check "the SQL of the deploy runs the Go tests" "go=true web=false e2e=false" "deploy/sql/roles.sql
"
check "the price fixture tool runs the Go tests" "go=true web=false e2e=false" "tools/price-fixtures/generate.go
"
check "web and server together run every test" "$every" "web/src/app/page.tsx
server/cmd/pricedin/main.go
"
check ".tool-versions runs everything" "$every" ".tool-versions
"
check "the Makefile runs everything" "$every" "Makefile
"
check "a workflow runs everything" "$every" ".github/workflows/deploy.yml
"
check "renovate.json runs everything" "$every" "renovate.json
"
check "this script runs everything" "$every" "scripts/ci-changes.sh
"
check "the CI verdict script runs everything" "$every" "scripts/ci-ok.sh
"
check "an unknown path runs everything" "$every" "go.work
"
check "a local action runs everything" "$every" ".github/actions/setup/action.yml
"
check "the code owners file runs lint only" "$none" ".github/CODEOWNERS
"
check ".gitattributes runs everything" "$every" ".gitattributes
"
check "a last line without a newline counts" "$web_e2e" "docs/a.md
web/package.json"
check "all runs everything" "$every" "docs/a.md
" all

set +e
"$changes" every </dev/null >/dev/null 2>&1
got_exit=$?
set -e
if [ "$got_exit" -ne 2 ]; then
	echo "FAIL an unknown argument: exit $got_exit; want exit 2"
	failures=$((failures + 1))
else
	echo "ok   an unknown argument"
fi

[ "$failures" -eq 0 ]
