# JGO

English | [简体中文](README.zh-CN.md)

JGO is a standalone Go service framework and project scaffold for RPC-style HTTP/JSON APIs and gRPC/protobuf. It has no mandatory private-infrastructure dependency; databases, Redis, messaging, discovery, and identity systems integrate through standard extension points.

Module: `github.com/eyesofblue/jgo`. The current main branch targets `v0.5.0`.

## Requirements and installation

- Go `1.24.0` or newer for every project.
- Buf `1.46.0`, `protoc-gen-go` `1.36.7`, and `protoc-gen-go-grpc` `1.5.1` only when authoring or generating local protobuf contracts.
- An external-only gRPC project does not need Buf.

```bash
go install github.com/eyesofblue/jgo/cmd/jgo@v0.5.0
jgo --version

# Once per Go environment that develops local protobuf contracts
jgo tools install
jgo tools check
```

JGO uses `GOTOOLCHAIN=local`; it never silently downloads or switches Go. `jgo new` runs `go mod tidy` and creates `go.sum` unless `--skip-tidy` is explicitly used.

## Empty project skeletons

```bash
jgo new <name> --module <module> --type <web|grpc|mixed|proto>
```

- `web`: runnable HTTP service with an empty OpenAPI contract.
- `grpc`: runnable empty gRPC server with no Echo and no local proto.
- `mixed`: empty HTTP plus empty gRPC server.
- `proto`: valid empty public protocol module with no server and no JGO runtime dependency.

Project type and protobuf source are independent. A grpc/mixed service may use only shared protocols, own local protocols, or use both.

```bash
jgo generate # safe no-op when no contract or binding exists
jgo list
jgo doctor
go test ./...
```

## HTTP APIs

JGO keeps RPC-style routes such as `GET /get_user?uid=12345` and `POST /update_user`. Complex requests and responses use Go structs under `api/http/model`:

```bash
jgo api add UpdateUser --method POST --path /update_user \
  --request-params UpdateUserRequest \
  --response-data UserInfo
jgo api generate
```

Responses always use the envelope:

```json
{"code": 0, "msg": "", "data": {"uid": 12345}}
```

HTTP status and business code are separate. Business success is always code `0`.

## Local protobuf contracts

```bash
jgo pb service add UserService
jgo pb method add GetUser --service UserService
# Add request fields and response business fields from field number 3.
jgo pb generate
```

The first Service creates `api/proto/<project>/v1/service.proto` and defaults to protobuf package `<project>.v1`. Choose or create a domain/version explicitly:

```bash
jgo pb service add OrderService --package company.order.v1
jgo pb service add UserService --package company.user.v2
```

If one package spans multiple proto files, select the destination explicitly with `--file`; JGO never guesses between them. Symlinks under `api/proto` are rejected so authoring commands cannot modify contracts outside the project tree.

Every response contains non-optional `int32 code = 1` and `string msg = 2`. Protocol checks:

```bash
jgo pb lint
jgo pb breaking --against '.git#branch=main'
```

Generated proto/grpc/mixed projects include a pull-request workflow that compares contracts with the PR base branch. Breaking changes require a new protobuf package such as `company.user.v2`.

## Shared protocol Service bindings

Create and publish a shared protocol module:

```bash
jgo new company-api --module example.com/company-api --type proto
cd company-api
jgo pb service add UserService --package company.user.v1
jgo pb method add GetUser --service UserService
jgo pb generate
```

Bind an entire Service on a server or client:

```bash
jgo rpc server bind UserService --module example.com/company-api@v0.1.0
jgo rpc client bind UserService \
  --module example.com/company-api@v0.1.0 \
  --name user --address 127.0.0.1:9090
```

`--name` is the stable client config/code identifier. Address, timeout, TLS, and readiness remain editable in YAML. Repeating `bind` is idempotent and updates compatible module versions without overwriting runtime configuration.

Server bindings are identified by `package + Service`. Each binding receives its own user-owned handler; `UserService.GetUser` defaults to `UserHandler.GetUser`, so protobuf RPC names stay short and do not collide on the application `Service`. If same-named Services coexist, give later bindings distinct names such as `--handler-name UserV2`, and unbind one precisely with `--package`.

`jgo doctor` validates the complete Handler RPC signature and reports expected/actual forms on mismatch. `jgo list` displays the concrete `UserHandler`, `UserV2Handler`, or other custom Handler entry point. v0.5 changes external servers only; local protobuf implementations remain `Service.<Service><Method>` rather than expanding this release into a unified Handler migration.

The v0.5 CLI uses RPC manifest version 2. A v0.4 manifest is rejected instead of rewriting user implementations: back up and remove `.jgo/rpc.json`, recreate the bindings, then move each old `Service.<generated-name>` body into `<Handler>.<RPC>`.

Permanent role/dependency cleanup is Service-grained:

```bash
jgo rpc server unbind UserService
jgo rpc client unbind user
```

`unbind` never edits the shared contract or deletes user-owned implementations. A failed post-unbind compile check rolls the mutation back.

## Unpublished modules and go.work

Use Go's standard workspace support while changing a protocol, server, and caller together:

```bash
go work init
go work use ./company-api ./user-service ./web-gateway
jgo rpc server bind UserService --module example.com/company-api
```

A module without `@version` must exist in the active `go.work`; otherwise binding fails. An explicit `@version` never scans unpublished workspace source, but does honor a user-managed `go.mod replace` that matches that version. Other standard Go operations still respect the active workspace. Only `jgo new` uses `GOWORK=off` internally while validating its temporary atomic staging directory.

Verify released dependencies independently with:

```bash
GOWORK=off go test ./...
```

## Generation and development commands

```bash
jgo api generate   # HTTP only
jgo pb generate    # local protobuf only
jgo generate       # HTTP + local protobuf + external bindings
jgo list           # HTTP, local gRPC, external servers and clients
jgo doctor         # tools plus external manifest/module/config and workspace diagnostics
jgo run --config configs/local.yaml
jgo build
```

`jgo run` forwards service flags directly; the extra `--` separator is no longer required.

## Business errors

RPC business errors use response `code/msg` with gRPC status `OK`. Network failures, panics, cancellation, and timeouts use non-OK gRPC status.

Business codes are declared in Go:

```go
var UserNotFound = jgoerrors.Define(
    40401, "USER_NOT_FOUND", "user not found", http.StatusNotFound,
)
var Catalog = jgoerrors.MustCatalog(UserNotFound)
```

The Catalog rejects duplicate codes/names and owns RPC-code-to-HTTP-status mapping. Unknown downstream business codes retain their public code/message, map to HTTP 500, and use the bounded Metrics label `unknown`.

For cross-service governance, publish shared definitions from a dedicated Go module and merge them during process initialization. Conflicts originating in different repositories then fail immediately:

```go
var Catalog = jgoerrors.MustMergeCatalogs(
    sharedcodes.Catalog,
    jgoerrors.MustCatalog(UserNotFound),
)
```

Web callers convert RPC business responses through that same Catalog instead of maintaining per-handler status switches:

```go
rpcResponse, err := service.RPC.User.GetUser(ctx, request)
if err != nil {
    return nil, err
}
if rpcResponse.GetCode() != 0 {
    return nil, errcode.Catalog.FromCode(int(rpcResponse.GetCode()), rpcResponse.GetMsg())
}
return rpcResponse.GetUser(), nil
```

## Production safety

HTTP request bodies are limited to 4 MiB by default and can be adjusted with `service.max_body_bytes`. Generated JSON handlers accept exactly one JSON document and validate OpenAPI required, type, format, and range constraints before business code runs; oversized bodies return 413 and contract violations return 400.

The framework default for gRPC Reflection is off; `configs/local.yaml` explicitly enables it for local debugging. Server TLS/mTLS is configuration-driven:

```yaml
grpc:
  reflection:
    enabled: true
  tls:
    enabled: false
    cert_file: ""
    key_file: ""
    client_auth: none # or require_and_verify
    client_ca_file: ""
```

Invalid certificates or incomplete TLS/mTLS configuration fail startup and never downgrade to plaintext. `internal/securityx/security.go` is the user-owned HTTP/gRPC integration hook for JGO's infrastructure-neutral `Authenticator` and `Authorizer`. HTTP authentication/authorization failures return 401/403; gRPC returns `Unauthenticated`/`PermissionDenied`.

YAML decoding is strict: unknown fields fail startup instead of silently using defaults.

## Management, Metrics, and Readiness

Every service has a separate management listener (`:9091` locally):

```text
GET /healthz  current-process liveness only
GET /readyz   required/optional dependency readiness
GET /metrics  Prometheus RED and Go runtime metrics
```

RPC clients default to strict readiness. A bound dependency is `required`; explicitly relax non-critical dependencies to `optional`:

```yaml
rpc_client:
  user:
    address: dns:///user-service:9090
    timeout: 3s
    readiness: optional
```

Unavailable dependencies do not prevent process startup. A required dependency makes `/readyz` return 503; the real RPC returns `Unavailable`, and JGO performs no automatic retry. The process remains not-ready until HTTP, gRPC, and Management listeners are bound, and returns to not-ready before shutdown draining begins. The registry enforces its deadline at collection time and isolates checker panics as `NOT_READY`, so a third-party checker cannot hang or crash `/readyz`. Database, Redis, MQ, and private components can implement the same interface.

Prometheus is enabled locally; OTLP Metrics is optional and may run simultaneously:

```yaml
telemetry:
  metrics:
    otlp:
      enabled: false
      endpoint: 127.0.0.1:4317
      insecure: true
```

JGO exposes HTTP/gRPC request rate, errors, latency, bounded business codes, and Go runtime metrics. IDs, trace IDs, and error messages are never metric labels.

## Tracing and structured logs

HTTP and gRPC propagate W3C `traceparent`; HTTP returns `X-Trace-ID`. OTLP trace export is disabled until configured.

```go
logx.InfoCtx(ctx, "get user completed", "uid", uid, "user", userInfo)
logx.ErrorCtx(ctx, "get user failed", "uid", uid, "err", err)
```

`DebugCtx`, `InfoCtx`, `WarnCtx`, and `ErrorCtx` attach `trace_id` and `span_id` from context.

## Verification and docs

```bash
gofmt -w .
go test ./...
go vet ./...
go build ./cmd/jgo
```

See the [documentation index](docs/README.md), [getting started guide](docs/getting-started.md), [CLI reference](docs/command-reference.md), [dependencies](docs/dependencies.md), and [architecture knowledge base](docs/architecture-and-roadmap.md).

## License

Apache License 2.0.
