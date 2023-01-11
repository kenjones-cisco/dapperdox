#!/bin/bash

set -o pipefail

options=()
[[ -n "$TEST_NAME" ]] && options=(-run "$TEST_NAME")

echo "options = ${options[@]}"

pkgs=()
if [[ -n "$TEST_PKG" ]]; then
  pkgs=("$TEST_PKG")
else
  pkgs=('./...')
fi

# Color is auto-enabled based on the TERM env or if it's a tty.  We have neither inside the Docker container, so
# it's explicitly enabled by setting the --no-color flag to false (yuck, a double negative).
case "$1" in
"")
    gotestsum --no-color=false -- "${pkgs[@]}" -bench . "${options[@]}"
    ;;
--race)
    # Race condition detector has libc dependencies, and requires CGO.
    # See:
    # https://github.com/golang/go/issues/9918
    # https://github.com/golang/go/issues/6508
    # https://github.com/golang/go/issues/27089
    CGO_ENABLED=1 gotestsum --no-color=false -- "${pkgs[@]}" -bench . "${options[@]}" -race
    ;;
*)
    echo >&2 "error: invalid option: $1"; exit 1 ;;
esac
