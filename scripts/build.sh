#!/bin/bash

PROJECT_FILE="${PROJECT_FILE:-project.yml}"


get_targets() {
    local data

    data=$(yq '.metadata.build[].target' "${PROJECT_FILE}")
    echo "$data"
}

build() {
    local project
    local import_path
    local ldflags
    local binary

    project=$(yq '.metadata.name' "${PROJECT_FILE}")
    import_path=$(yq '.metadata.import' "${PROJECT_FILE}")

    ldflags="-X ${import_path}/version.GitCommit=$(git rev-parse --short HEAD)"
    ldflags="${ldflags} -X ${import_path}/version.GitDescribe=$(git describe --tags --always)"

    mapfile -t targets < <(get_targets)
    for target in "${targets[@]}"; do
        if [[ "$target" == "." ]]; then
          binary="$project"
        else
          binary=$(basename "$target")
        fi
        echo "building: $target ==> bin/$binary"
        go build -ldflags "${ldflags}" -o "bin/$binary" "${import_path}/$target"
    done
}

build
