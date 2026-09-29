#!/usr/bin/env bash

set -euo pipefail

readonly manifest="scripts/safety-review-protected-go.sha256"
readonly manifest_sha256="e688620ce8876434508f2b7c085d6b18b78c1e43e67904539a70006b1e4d37e3"
readonly allowed_preexisting_go=(
  cmd/advertisement-clean-prep/apply.go
  cmd/advertisement-clean-prep/main.go
  cmd/advertisement-clean-prep/main_test.go
  cmd/advertisement-clean-prep/model.go
  cmd/advertisement-clean-prep/prepare.go
  cmd/advertisement-clean-prep/route.go
  cmd/advertisement-full-clean/adjudicate.go
  cmd/advertisement-full-clean/adjudicate_test.go
  cmd/advertisement-full-clean/apply.go
  cmd/advertisement-full-clean/apply_test.go
  cmd/advertisement-full-clean/calibration.go
  cmd/advertisement-full-clean/calibration_test.go
  cmd/advertisement-full-clean/contracts_test.go
  cmd/advertisement-full-clean/evaluate.go
  cmd/advertisement-full-clean/evaluate_test.go
  cmd/advertisement-full-clean/main.go
  cmd/advertisement-full-clean/main_test.go
  cmd/advertisement-full-clean/model.go
  cmd/advertisement-full-clean/replace.go
  cmd/advertisement-full-clean/replace_test.go
  cmd/advertisement-full-clean/risk_select.go
  cmd/advertisement-full-clean/risk_select_test.go
  cmd/advertisement-full-clean/route.go
  cmd/advertisement-full-clean/route_test.go
  cmd/merge-failed/main.go
  cmd/merge-failed/main_test.go
  cmd/prepare-xguard-v1/main.go
  cmd/prepare-xguard-v1/main_test.go
  internal/service/advertisement_full_review.go
  internal/service/advertisement_full_review_test.go
  internal/service/advertisement_review.go
  internal/service/advertisement_review_export.go
  internal/service/advertisement_review_export_test.go
  internal/service/advertisement_review_test.go
  internal/service/label_review.go
  internal/service/label_review_test.go
  internal/service/reconcile.go
  internal/service/reconcile_batch.go
  internal/service/reconcile_internal_test.go
  internal/service/reconcile_test.go
)

# is_allowed_preexisting_go 判断路径是否属于 Safety Review 启动前已批准的历史文件。
is_allowed_preexisting_go() {
  local path="$1"
  local existing
  for existing in "${allowed_preexisting_go[@]}"; do
    if [[ "$path" == "$existing" ]]; then
      return 0
    fi
  done
  return 1
}

# is_allowed_workstream_go 判断路径是否属于已批准的并行 workstream。
is_allowed_workstream_go() {
  local basename
  basename="$(basename "$1")"
  case "$basename" in
    safety_review_*|policy_optimizer_*)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

if command -v sha256sum >/dev/null 2>&1; then
  readonly hash_command=(sha256sum)
elif [[ -x /sbin/sha256sum ]]; then
  readonly hash_command=(/sbin/sha256sum)
elif command -v shasum >/dev/null 2>&1; then
  readonly hash_command=(shasum -a 256)
elif command -v openssl >/dev/null 2>&1; then
  readonly hash_command=(openssl dgst -sha256 -r)
else
  printf '%s\n' "no SHA-256 command is available" >&2
  exit 1
fi

if [[ ! -f "$manifest" ]]; then
  printf '%s\n' "safety review scope manifest is missing: $manifest" >&2
  exit 1
fi

actual_manifest_sha256="$("${hash_command[@]}" -- "$manifest" | cut -d ' ' -f 1)"
if [[ "$actual_manifest_sha256" != "$manifest_sha256" ]]; then
  printf '%s\n' "safety review scope manifest was modified" >&2
  exit 1
fi

while IFS=' ' read -r expected path; do
	if [[ -z "$expected" || -z "$path" ]]; then
		continue
	fi
	if [[ ! -f "$path" || -z "$(git ls-files -- "$path")" ]]; then
		printf '%s\n' "protected Go file is missing or untracked: $path" >&2
		exit 1
	fi
	actual="$("${hash_command[@]}" -- "$path" | cut -d ' ' -f 1)"
  if [[ "$actual" != "$expected" ]]; then
    printf '%s\n' "protected Go file changed: $path" >&2
    exit 1
  fi
done < "$manifest"

while IFS= read -r path; do
  if [[ "$path" == "main.go" ]] ||
    grep -Eq "^[0-9a-f]{64}  ${path//./\\.}$" "$manifest" ||
    is_allowed_preexisting_go "$path"; then
    continue
  fi
  if ! is_allowed_workstream_go "$path"; then
    printf '%s\n' "new Go basename must start with safety_review_ or policy_optimizer_: $path" >&2
    exit 1
  fi
done < <(git ls-files -- '*.go')

while IFS= read -r path; do
  if is_allowed_preexisting_go "$path"; then
    continue
  fi
  if ! is_allowed_workstream_go "$path"; then
    printf '%s\n' "new untracked Go basename must start with safety_review_ or policy_optimizer_: $path" >&2
    exit 1
  fi
done < <(git ls-files --others --exclude-standard -- '*.go')

printf '%s\n' "safety review scope: PASS"
