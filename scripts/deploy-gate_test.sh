#!/bin/sh
# Tests scripts/deploy-gate.sh. `make test-scripts` runs it.
set -eu

gate="$(dirname "$0")/deploy-gate.sh"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
failures=0

# check <name> <want output> <want exit> <command>...
check() {
	name=$1 want_output=$2 want_exit=$3
	shift 3
	: >"$tmp/output"
	set +e
	GITHUB_OUTPUT="$tmp/output" "$@" >"$tmp/log" 2>&1
	got_exit=$?
	set -e
	got_output=$(cat "$tmp/output")
	if [ "$got_exit" -ne "$want_exit" ] || [ "$got_output" != "$want_output" ]; then
		echo "FAIL $name: exit $got_exit, output '$got_output'; want exit $want_exit, output '$want_output'"
		sed 's/^/    /' "$tmp/log"
		failures=$((failures + 1))
	else
		echo "ok   $name"
	fi
}

check "every secret set" "ready=true" 0 env A=x B=y "$gate" web A B
check "one secret missing" "ready=false" 0 env A=x "$gate" web A B
check "one secret empty" "ready=false" 0 env A=x B= "$gate" web A B
check "not a variable name" "" 2 "$gate" web 'A;rm'
check "no variable" "" 2 "$gate" web

if ! env A=x "$gate" server A B 2>&1 | grep -q 'These secrets are not set: B\.'; then
	echo "FAIL the notice names the missing secret"
	failures=$((failures + 1))
else
	echo "ok   the notice names the missing secret"
fi

[ "$failures" -eq 0 ]
