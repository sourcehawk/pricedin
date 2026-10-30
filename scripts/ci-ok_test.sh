#!/bin/sh
# Tests scripts/ci-ok.sh. `make test-scripts` runs it.
set -eu

ok="$(dirname "$0")/ci-ok.sh"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
failures=0

# check <name> <want exit> <argument>...
check() {
	name=$1 want_exit=$2
	shift 2
	set +e
	"$ok" "$@" >"$tmp/log" 2>&1
	got_exit=$?
	set -e
	if [ "$got_exit" -ne "$want_exit" ]; then
		echo "FAIL $name: exit $got_exit; want exit $want_exit"
		sed 's/^/    /' "$tmp/log"
		failures=$((failures + 1))
	else
		echo "ok   $name"
	fi
}

check "every job succeeded" 0 changes=success lint=success test-web=success:true
check "a job that must run failed" 1 changes=success lint=failure
check "a job that must run was skipped" 1 changes=success lint=skipped
check "a job that must run was cancelled" 1 changes=success lint=cancelled
check "a filtered job that was turned off was skipped" 0 lint=success test-web=skipped:false
check "a filtered job that was turned on was skipped" 1 lint=success test-web=skipped:true
check "a filtered job that was turned on failed" 1 lint=success test-web=failure:true
check "a filtered job that was turned off failed" 1 lint=success test-web=failure:false
check "a filtered job that was turned off was cancelled" 1 lint=success test-web=cancelled:false
check "a filtered job with no decision must succeed" 1 lint=success test-web=skipped:
check "a filtered job that succeeded although turned off" 0 lint=success test-web=success:false
check "no job" 2
check "not job=result" 2 lint
check "an empty job name" 2 =success
check "a decision other than true or false" 2 test-web=skipped:maybe

if ! "$ok" lint=success test-web=failure:true 2>&1 | grep -q 'test-web: failure'; then
	echo "FAIL the output names the job that failed"
	failures=$((failures + 1))
else
	echo "ok   the output names the job that failed"
fi

[ "$failures" -eq 0 ]
