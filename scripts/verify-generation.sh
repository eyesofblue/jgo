#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temporary_root="$(mktemp -d)"
server_pid=""
caller_pid=""
cleanup() {
  local status=$?
  if [[ -n "${caller_pid}" ]]; then kill "${caller_pid}" 2>/dev/null || true; fi
  if [[ -n "${server_pid}" ]]; then kill "${server_pid}" 2>/dev/null || true; fi
  if [[ "${status}" -ne 0 ]]; then
    for diagnostic in grpc-server.log web-caller.log success.headers success.json timeout.json unavailable.json; do
      if [[ -f "${temporary_root}/${diagnostic}" ]]; then
        echo "===== ${diagnostic} =====" >&2
        sed -n '1,200p' "${temporary_root}/${diagnostic}" >&2
      fi
    done
  fi
  rm -rf "${temporary_root}"
  return "${status}"
}
trap cleanup EXIT

for tool in go buf protoc-gen-go protoc-gen-go-grpc shasum curl; do
  if ! command -v "${tool}" >/dev/null 2>&1; then
    echo "required tool not found: ${tool}; run 'jgo tools install'" >&2
    exit 1
  fi
done

cd "${repository_root}"
go build -trimpath -o "${temporary_root}/jgo" ./cmd/jgo

snapshot() {
  local project_root="$1"
  find "${project_root}" -type f \
    -not -path '*/bin/*' \
    -exec shasum -a 256 {} \; | sort | shasum -a 256
}

for project_type in web grpc mixed proto; do
  project_name="demo-${project_type}"
  project_root="${temporary_root}/${project_name}"

  "${temporary_root}/jgo" new "${project_name}" \
    --module "example.com/${project_name}" \
    --type "${project_type}" \
    --output "${project_root}" \
    --jgo-replace "${repository_root}"

  test -f "${project_root}/go.sum"
  if [[ "${project_type}" == "proto" ]]; then
    ! grep -q 'github.com/eyesofblue/jgo' "${project_root}/go.mod"
  else
    grep -q 'github.com/eyesofblue/jgo v0.4.0' "${project_root}/go.mod"
  fi

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

  if [[ "${project_type}" == "grpc" || "${project_type}" == "mixed" || "${project_type}" == "proto" ]]; then
    if [[ "${project_type}" == "grpc" ]]; then
      service_name="DemoGrpcService"
    elif [[ "${project_type}" == "proto" ]]; then
      service_name="DemoProtoService"
    else
      service_name="DemoMixedService"
    fi
    "${temporary_root}/jgo" pb service add "${service_name}" \
      --root "${project_root}"
    "${temporary_root}/jgo" pb method add GetUser \
      --service "${service_name}" \
      --root "${project_root}"
    proto_file="${project_root}/api/proto/${project_name//-/_}/v1/service.proto"
    grep -q 'int32 code = 1;' "${proto_file}"
    grep -q 'string msg = 2;' "${proto_file}"
  fi

  "${temporary_root}/jgo" generate --root "${project_root}"
  if [[ "${project_type}" == "grpc" || "${project_type}" == "mixed" ]]; then
    grep -q 'stderrors.As(err, &businessError)' "${project_root}/internal/transport/grpc/register.gen.go"
    grep -q 'Msg: businessError.Message()' "${project_root}/internal/transport/grpc/register.gen.go"
  elif [[ "${project_type}" == "proto" ]]; then
    test -f "${project_root}/gen/pb/demo_proto/v1/service.pb.go"
    test -f "${project_root}/gen/pb/demo_proto/v1/service_grpc.pb.go"
    test ! -e "${project_root}/cmd/server/main.go"
    test ! -e "${project_root}/internal/service/service.go"
    test ! -e "${project_root}/internal/transport/grpc/register.gen.go"
    ! grep -q 'github.com/eyesofblue/jgo' "${project_root}/go.mod"
  fi
  "${temporary_root}/jgo" doctor --root "${project_root}"

  before="$(snapshot "${project_root}")"
  "${temporary_root}/jgo" generate --root "${project_root}"
  after="$(snapshot "${project_root}")"
  test "${before}" = "${after}"

  if [[ "${project_type}" == "grpc" ]]; then
    # Removing a contract must not leave a deleted RPC registered. The first
    # attempt deliberately keeps the user-owned implementation, so generation
    # must fail and roll managed outputs back. After the owner removes that
    # implementation, generation cleans the stale protobuf and transport files.
    rm "${proto_file}"
    if "${temporary_root}/jgo" pb generate --root "${project_root}"; then
      echo "protobuf removal unexpectedly succeeded with a stale user implementation" >&2
      exit 1
    fi
    test -f "${project_root}/gen/pb/demo_grpc/v1/service.pb.go"
    grep -q 'RegisterDemoGrpcServiceServer' "${project_root}/internal/transport/grpc/register.gen.go"
    rm "${project_root}/internal/service/demo_grpc_service_get_user.go"
    "${temporary_root}/jgo" pb generate --root "${project_root}"
    test ! -e "${project_root}/gen/pb/demo_grpc/v1/service.pb.go"
    test ! -e "${project_root}/gen/pb/demo_grpc/v1/service_grpc.pb.go"
    ! grep -q 'RegisterDemoGrpcServiceServer' "${project_root}/internal/transport/grpc/register.gen.go"
  fi

  (
    cd "${project_root}"
    go test ./...
    go build ./...
  )
done

# Verify one shared proto module can be consumed independently by a server and
# a caller. Local replace directives keep this check offline and deterministic.
protocol_root="${temporary_root}/demo-proto"
server_root="${temporary_root}/demo-grpc"
caller_root="${temporary_root}/demo-web"
grpc_address="127.0.0.1:$((20000 + RANDOM % 10000))"
http_address="127.0.0.1:$((30000 + RANDOM % 10000))"
server_management_address="127.0.0.1:$((40000 + RANDOM % 5000))"
caller_management_address="127.0.0.1:$((45000 + RANDOM % 5000))"

cp "${repository_root}/scripts/testdata/e2e_service.proto" \
  "${protocol_root}/api/proto/demo_proto/v1/service.proto"
"${temporary_root}/jgo" pb generate --root "${protocol_root}"

(
  cd "${server_root}"
  go mod edit -replace=example.com/demo-proto="${protocol_root}"
  "${temporary_root}/jgo" rpc server bind DemoProtoService \
    --module example.com/demo-proto@v0.1.0
  "${temporary_root}/jgo" rpc server bind AdminService \
    --module example.com/demo-proto@v0.1.0
  "${temporary_root}/jgo" doctor
  server_bindings="$("${temporary_root}/jgo" list)"
  [[ "${server_bindings}" == *external-server*DemoProtoService* ]]
	  cp "${repository_root}/scripts/testdata/e2e_grpc_get_user.go" \
	    internal/service/demo_proto_v1_demo_proto_service_get_user.go
  grep -q 'RegisterDemoProtoServiceServer' internal/transport/grpc/external.gen.go
  grep -q 'RegisterAdminServiceServer' internal/transport/grpc/external.gen.go
  test "$(grep -c 'example.com/demo-proto/gen/pb/demo_proto/v1' internal/transport/grpc/external.gen.go)" = "1"
  grep -q 'DemoProtoV1DemoProtoServiceGetUser' internal/service/demo_proto_v1_demo_proto_service_get_user.go
  go test ./...
  go build ./...
)

(
  cd "${caller_root}"
  go mod edit -replace=example.com/demo-proto="${protocol_root}"
  "${temporary_root}/jgo" rpc client bind DemoProtoService \
    --module example.com/demo-proto@v0.1.0 \
    --name demo_proto \
    --address "${grpc_address}"
  "${temporary_root}/jgo" rpc client bind DemoProtoService \
    --module example.com/demo-proto@v0.1.0 \
    --name demo_proto_backup \
    --address "${grpc_address}"
  "${temporary_root}/jgo" doctor
  client_bindings="$("${temporary_root}/jgo" list)"
  [[ "${client_bindings}" == *external-client*demo_proto* ]]
  cp "${repository_root}/scripts/testdata/e2e_web_get_user.go" \
    internal/service/get_user.go
  grep -q 'DemoProto .*DemoProtoServiceClient' internal/rpcclient/clients.gen.go
  grep -q 'demo_proto:' configs/local.yaml
  grep -q 'demo_proto_backup:' configs/local.yaml
  test "$(grep -c 'readiness: required' configs/local.yaml)" = "2"
  test "$(grep -c 'example.com/demo-proto/gen/pb/demo_proto/v1' internal/rpcclient/clients.gen.go)" = "1"
  go test ./...
  go build ./...
)

# Start both processes and verify HTTP -> gRPC behavior with a fixed W3C trace.
server_log="${temporary_root}/grpc-server.log"
caller_log="${temporary_root}/web-caller.log"
(
  cd "${server_root}"
  go build -trimpath -o "${temporary_root}/grpc-server" ./cmd/server
)
(
  cd "${caller_root}"
  go build -trimpath -o "${temporary_root}/web-caller" ./cmd/server
)

(
  cd "${server_root}"
  exec "${temporary_root}/grpc-server" --config configs/local.yaml --grpc-address "${grpc_address}" --management-address "${server_management_address}"
) >"${server_log}" 2>&1 &
server_pid=$!

(
  cd "${caller_root}"
  exec "${temporary_root}/web-caller" --config configs/local.yaml --http-address "${http_address}" --management-address "${caller_management_address}"
) >"${caller_log}" 2>&1 &
caller_pid=$!

health_status=""
for _ in $(seq 1 60); do
  health_status="$(curl -sS -o /dev/null -w '%{http_code}' "http://${caller_management_address}/healthz" 2>/dev/null || true)"
  if [[ "${health_status}" == "200" ]]; then break; fi
  sleep 0.1
done
test "${health_status}" = "200"

trace_id="0123456789abcdef0123456789abcdef"
span_id="0123456789abcdef"
success_headers="${temporary_root}/success.headers"
success_body="${temporary_root}/success.json"
success_status=""
for _ in $(seq 1 60); do
  success_status="$(curl -sS -D "${success_headers}" -o "${success_body}" -w '%{http_code}' \
    -H "traceparent: 00-${trace_id}-${span_id}-01" \
    "http://${http_address}/get_user?uid=12345" 2>/dev/null || true)"
  if [[ "${success_status}" == "200" ]]; then break; fi
  sleep 0.1
done
test "${success_status}" = "200"
grep -qi "^X-Trace-ID: ${trace_id}" "${success_headers}"
grep -q "\"name\":\"${trace_id}\"" "${success_body}"
grep -q "\"trace_id\":\"${trace_id}\"" "${server_log}"

# The generated client default is three seconds and an existing caller
# deadline has priority. A five-second handler must be canceled near 3s.
timeout_start="$(date +%s)"
timeout_status="$(curl -sS -o "${temporary_root}/timeout.json" -w '%{http_code}' \
  "http://${http_address}/get_user?uid=999" 2>/dev/null || true)"
timeout_elapsed=$(( $(date +%s) - timeout_start ))
test "${timeout_status}" != "200"
test "${timeout_elapsed}" -ge 2
test "${timeout_elapsed}" -le 5
grep -q 'DeadlineExceeded' "${caller_log}"

# Stopping the remote service does not affect caller process health. The next
# business call reports gRPC Unavailable through the HTTP system-error path.
kill "${server_pid}"
wait "${server_pid}" 2>/dev/null || true
server_pid=""
health_status="$(curl -sS -o /dev/null -w '%{http_code}' "http://${caller_management_address}/healthz")"
test "${health_status}" = "200"
unavailable_status="$(curl -sS -o "${temporary_root}/unavailable.json" -w '%{http_code}' \
  "http://${http_address}/get_user?uid=12345" 2>/dev/null || true)"
test "${unavailable_status}" != "200"
grep -q 'Unavailable' "${caller_log}"

kill "${caller_pid}"
wait "${caller_pid}" 2>/dev/null || true
caller_pid=""
