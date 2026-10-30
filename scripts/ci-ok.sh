#!/bin/sh
# Usage: ci-ok.sh <job>=<result>[:<decision>]...
#
# The verdict of the CI workflow. Each argument gives the result of one job
# (success, failure, cancelled or skipped). A job without a decision must
# succeed. A job with a decision is one that scripts/ci-changes.sh can turn
# off: with the decision false, it may also be skipped; with true or with an
# empty decision, it must succeed. Exits 0 when every job passes, 1 when one
# does not, and 2 on a malformed argument.
set -eu

if [ "$#" -eq 0 ]; then
	echo "usage: $0 <job>=<result>[:<decision>]..." >&2
	exit 2
fi

failed=0
for arg in "$@"; do
	case $arg in
	?*=*) ;;
	*)
		echo "$0: not <job>=<result>[:<decision>]: $arg" >&2
		exit 2
		;;
	esac
	job=${arg%%=*}
	result=${arg#*=}
	decision=true
	case $result in
	*:*)
		decision=${result#*:}
		result=${result%%:*}
		;;
	esac
	case $decision in
	true | false) ;;
	# The decision is empty when the job that decides did not finish.
	"") decision=true ;;
	*)
		echo "$0: $job: the decision is not true or false: $decision" >&2
		exit 2
		;;
	esac

	if [ "$result" = success ] || { [ "$result" = skipped ] && [ "$decision" = false ]; }; then
		echo "$job: $result"
	else
		echo "$job: $result, want success"
		failed=1
	fi
done
exit "$failed"
