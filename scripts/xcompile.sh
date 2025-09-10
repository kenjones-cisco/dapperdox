#!/bin/bash

PROJECT_FILE="${PROJECT_FILE:-project.yml}"
VERSION=$(yq '.metadata.version' "${PROJECT_FILE}")

get_targets() {
    local data

    data=$(yq '.metadata.build[].target' "${PROJECT_FILE}")
    echo "$data"
}

build() {
    local project
    local import_path
    local ldflags

    project=$(yq '.metadata.name' "${PROJECT_FILE}")
    import_path=$(yq '.metadata.import' "${PROJECT_FILE}")

    ldflags="-X ${import_path}/version.GitCommit=$(git rev-parse --short HEAD)"
    ldflags="${ldflags} -X ${import_path}/version.GitDescribe=$(git describe --tags --always)"

    mapfile -t targets < <(get_targets)
    for pkg in "${targets[@]}"; do
        if [[ "$pkg" == "." ]]; then
          binary="$project"
        else
          binary=$(basename "${pkg}")
        fi
        gox \
            -ldflags "${ldflags}" \
            -arch="amd64" \
            -os="darwin" \
            -os="linux" \
            -os="windows" \
            -output="build/${project}_${VERSION}_{{.OS}}_{{.Arch}}/${binary}" "${import_path}/${pkg}"
    done
}

build
