#!/usr/bin/env bash

set -euo pipefail

readonly root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

echo "=== Safety Review Formatting ==="
unformatted_files="$(gofmt -l .)"
if [[ -n "$unformatted_files" ]]; then
  echo "$unformatted_files"
  exit 1
fi

echo "=== Safety Review Focused Tests ==="
go test ./internal/lib/configs ./internal/service ./internal/api/cli \
  -run '^TestSafetyReview(Eval|Acceptance|Live|Rotation|SingleProfile)' \
  -count=1 -v

echo "=== Safety Review All Tests ==="
go test ./...

echo "=== Safety Review Repeated Focused Tests ==="
go test ./internal/lib/configs ./internal/service ./internal/api/cli \
  -run '^TestSafetyReview(Eval|Acceptance|Live|Rotation|SingleProfile)' \
  -count=50

echo "=== Safety Review Race Tests ==="
go test -race ./internal/lib/configs ./internal/service ./internal/api/cli \
  -run '^TestSafetyReview(Eval|Acceptance|Live|Rotation|SingleProfile|Run|Status|Export)' \
  -count=20

echo "=== Safety Review Vet ==="
go vet ./...

echo "=== Safety Review Scope ==="
./scripts/verify-safety-review-scope.sh

echo "=== Safety Review Security ==="
if rg -n 'AI_GATEWAY_API_KEY.*=' --glob '*.go' --glob '*.yaml' . >/dev/null; then
  echo "API key assignment detected"
  exit 1
fi
if rg -n 'Authorization:' --glob '*.go' . >/dev/null; then
  echo "Authorization header logging detected"
  exit 1
fi

echo "=== Safety Review Diff Check ==="
git diff --check

echo "Safety Review verification: PASS"
