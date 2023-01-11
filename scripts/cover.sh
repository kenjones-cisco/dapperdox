#!/bin/bash
# Generate test coverage statistics for Go packages.
#
# Works around the fact that `go test -coverprofile` currently does not work
# with multiple packages, see https://github.com/golang/go/issues/6909
#

set -e

workdir=cover
profile="$workdir/cover.out"
mode=count
results=test.out


show_html_report() {
    go tool cover -html="$profile" -o="$workdir"/coverage.html
}

show_ci_report() {
    show_html_report
}

_done() {
    local error_code="$?"

    # display actual test results
    if [ -f "$results" ]; then
      cat "$results"
    fi

    return $error_code
}

trap "_done" EXIT

rm -f "$results"


case "$1" in
"")
    gotestsum --no-color=false --junitfile test.xml -- -covermode="$mode" -coverprofile="$profile" ./... > "$results"
    show_html_report ;;
--ci)
    gotestsum --junitfile test.xml -- -covermode="$mode" -coverprofile="$profile" ./... > "$results"
    show_ci_report ;;
*)
    echo >&2 "error: invalid option: $1"; exit 1 ;;
esac
