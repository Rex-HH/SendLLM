#!/bin/bash
set -euo pipefail

echo "=== SendLLM Harness Initialization ==="

if [[ ! -f go.mod ]]; then
  echo "Go module not initialized; verification starts with feat-001."
  echo "Harness files are ready."
  exit 0
fi

echo "=== formatting ==="
unformatted_files="$(gofmt -l .)"
if [[ -n "${unformatted_files}" ]]; then
  echo "The following Go files need gofmt:"
  echo "${unformatted_files}"
  exit 1
fi

echo "=== go test ./... ==="
go test ./...

echo "=== go test -race ./... ==="
go test -race ./...

echo "=== go vet ./... ==="
go vet ./...

echo "=== Verification Complete ==="
echo "Read feature_list.json, select one unblocked feature, and update progress.md."
