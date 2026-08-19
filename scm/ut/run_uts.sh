#!/usr/bin/env bash

function run_uts {
  echo "" > coverage.out
  for d in $(go list ./... | grep -v vendor | grep -v crd); do
      go test -covermode=count -v -coverprofile=profile.out -coverpkg=github.com/danm-cni/danm/pkg/... "${d}"
      if [ -f profile.out ]; then
          cat profile.out >> coverage.out
          rm profile.out
      fi
  done
  awk '! a[$0]++' coverage.out > coverage2.out
  awk 'NF' coverage2.out > coverage3.out
}

set -e
export CGO_ENABLED=0
cd ${GOPATH}/src/github.com/danm-cni/danm

run_uts

cp ${GOPATH}/src/github.com/danm-cni/danm/coverage*.out /coverage/
