#!/bin/bash
set -euo pipefail

case "${1:-}" in
""|--clean|--fix|--check|--ci) ;;
*)
  echo >&2 "error: invalid option: ${1:-}"; exit 1 ;;
esac

mkdir -p .tmp/
export PROJECT="${PROJECT:-dapperdox}"

# Use the image baseline when available; the tracked snapshot supports local runs.
base_config="${GOLANGCI_BASE_CONFIG:-/usr/local/etc/golangci.yml}"
if [[ -z "${GOLANGCI_BASE_CONFIG:-}" && ! -f "$base_config" ]]; then
  base_config="scripts/golangci-base.yml"
fi

yq '. *+ load(".golangci.yml")' "$base_config" > .tmp/golangci.yml
yq -i '(.. | select(tag == "!!str")) |= envsubst' .tmp/golangci.yml

opts=("--color=always" "--config=.tmp/golangci.yml")
[[ -n "${DEBUG:-}" ]] && opts+=("--verbose")

case "${1:-}" in
--check|--ci)
  golangci-lint fmt "${opts[@]}" --diff
  ;;
*)
  golangci-lint fmt "${opts[@]}"
  opts+=("--fix")
  ;;
esac

golangci-lint run "${opts[@]}"
