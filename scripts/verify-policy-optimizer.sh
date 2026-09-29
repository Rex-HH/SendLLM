#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

go test ./internal/dto ./internal/dao ./internal/service ./internal/api/cli . \
  -run '^TestPolicyOptimizer' -count=1
go test -race ./internal/service ./internal/api/cli . \
  -run '^TestPolicyOptimizer' -count=1
git diff --check

echo "Policy Optimizer verification: PASS"
