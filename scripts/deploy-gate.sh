#!/bin/sh
# Usage: deploy-gate.sh <target> <variable>...
#
# Checks that each named environment variable is set and not empty. Writes
# ready=true to $GITHUB_OUTPUT when all are, and ready=false when one is
# missing. A missing variable is not an error: the script names it and
# exits 0, so that the deploy workflow passes without a deploy.
set -eu

if [ "$#" -lt 2 ]; then
	echo "usage: $0 <target> <variable>..." >&2
	exit 2
fi
target=$1
shift

missing=""
for name in "$@"; do
	case $name in
	"" | [0-9]* | *[!A-Za-z0-9_]*)
		echo "$0: not a variable name: $name" >&2
		exit 2
		;;
	esac
	eval "value=\${$name:-}"
	if [ -z "$value" ]; then
		missing="$missing $name"
	fi
done

out=${GITHUB_OUTPUT:-/dev/stdout}
if [ -n "$missing" ]; then
	echo "::notice title=No $target deploy::These secrets are not set:$missing. Nothing is deployed. docs/runbook.md says how to set them."
	echo "ready=false" >>"$out"
else
	echo "Every secret of the $target deploy is set."
	echo "ready=true" >>"$out"
fi
