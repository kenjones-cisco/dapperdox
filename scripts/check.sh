#!/bin/bash

set -o errexit
set -o pipefail

case "$1" in
"")
    golangci-lint -v run --color=always --print-resources-usage -c .golangci.yml --fix
    ;;
--ci)
    golangci-lint -v run --print-resources-usage -c .golangci.yml
    ;;
*)
    echo >&2 "error: invalid option: $1"; exit 1 ;;
esac
