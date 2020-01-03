#!/bin/bash

set -e

REPODIR=$(git rev-parse --show-toplevel)

COVERPROFILE=
if [ -n "${COVERALLS_TOKEN}" ]; then
    echo "Installing goveralls..."
    COVERPROFILE=profile.cov
    go get github.com/mattn/goveralls@v0.0.4
fi

echo "Running unit tests..."
make -C "${REPODIR}" COVERPROFILE="${COVERPROFILE}" test

if [ -n "${COVERALLS_TOKEN}" ]; then
    echo "Pushing code coverage..."
    "$(go env GOPATH)"/bin/goveralls -service=travis-ci
fi
