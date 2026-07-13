#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temporary_root="$(mktemp -d)"
trap 'rm -rf "${temporary_root}"' EXIT

for tool in go buf protoc-gen-go protoc-gen-go-grpc shasum; do
  if ! command -v "${tool}" >/dev/null 2>&1; then
    echo "required tool not found: ${tool}; install the locked protobuf tools with 'make tools'" >&2
    exit 1
  fi
done

cd "${repository_root}"
go build -trimpath -o "${temporary_root}/jgo" ./cmd/jgo

snapshot() {
  local project_root="$1"
  find "${project_root}" -type f \
    -not -path '*/bin/*' \
    -not -name go.sum \
    -exec shasum -a 256 {} \; | sort | shasum -a 256
}

for project_type in web grpc mixed; do
  project_name="demo-${project_type}"
  project_root="${temporary_root}/${project_name}"

  "${temporary_root}/jgo" new "${project_name}" \
    --module "example.com/${project_name}" \
    --type "${project_type}" \
    --output "${project_root}" \
    --jgo-replace "${repository_root}"

  if [[ "${project_type}" == "web" || "${project_type}" == "mixed" ]]; then
    cp "${repository_root}/scripts/testdata/http_models.go" "${project_root}/api/http/model/user.go"
    "${temporary_root}/jgo" api add GetUser \
      --method GET \
      --path /get_user \
      --request uid:int64:required \
      --response-data UserInfo \
      --root "${project_root}"
    "${temporary_root}/jgo" api add UpdateUser \
      --method POST \
      --path /update_user \
      --request-params UpdateUserRequest \
      --response-data UserInfo \
      --root "${project_root}"
    "${temporary_root}/jgo" api add ListUsers \
      --method GET \
      --path /list_users \
      --response-data UserInfo \
      --response-list \
      --root "${project_root}"
  fi

  if [[ "${project_type}" == "grpc" || "${project_type}" == "mixed" ]]; then
    "${temporary_root}/jgo" rpc add GetUser \
      --service GreeterService \
      --root "${project_root}"
  fi

  "${temporary_root}/jgo" generate --root "${project_root}"
  "${temporary_root}/jgo" doctor --root "${project_root}"

  before="$(snapshot "${project_root}")"
  "${temporary_root}/jgo" generate --root "${project_root}"
  after="$(snapshot "${project_root}")"
  test "${before}" = "${after}"

  (
    cd "${project_root}"
    go mod tidy
    go test ./...
    go build ./...
  )
done
