# JGO

[English](README.md) | [简体中文](README.zh-CN.md)

JGO is a standalone Go service framework and project scaffolding tool for HTTP/JSON and gRPC/protobuf services.

Runtime support, project scaffolding, contract generation, unified debugging, and developer workflow commands are available. Start with the [documentation index](docs/README.md) or the [quick-start guide](docs/getting-started.md).

## Module

```text
github.com/eyesofblue/jgo
```

The current release is `v0.2.0`; see [CHANGELOG.md](CHANGELOG.md).

## Prerequisites

| Project type | Required software |
| --- | --- |
| `web` | Go `1.24.0` or later |
| `grpc` | Go `1.24.0` or later, Buf `1.46.0`, `protoc-gen-go` `1.36.7`, `protoc-gen-go-grpc` `1.5.1` |
| `mixed` | The same Go and protobuf toolchain as `grpc` |

JGO has no required database, Redis, message queue, service registry, configuration center, or other private infrastructure. Private infrastructure can be integrated later through application components and HTTP/gRPC middleware.

Make sure the Go binary directory is in `PATH`:

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

## Install

Install a published version:

```bash
go install github.com/eyesofblue/jgo/cmd/jgo@v0.2.0
jgo --version
```

Use `@latest` when you intentionally want the newest published release.

For JGO framework development, you can also clone the repository and build from source:

```bash
go build -trimpath -o bin/jgo ./cmd/jgo
./bin/jgo --version
```

For gRPC and mixed projects, install the locked generators once per Go development environment:

```bash
jgo tools install
jgo tools check
```

JGO uses `GOTOOLCHAIN=local` and never downloads or switches Go toolchains silently. Exact module and tool versions are documented in [docs/dependencies.md](docs/dependencies.md).

## New project: create a service and its first API

Generated projects default to `github.com/eyesofblue/jgo v0.2.0` and the active `go env GOVERSION`; use `--go-version` to override it. Project creation runs `go mod tidy` and writes `go.sum`; offline environments can use `--skip-tidy`. During local framework development, add `--jgo-replace /absolute/path/to/jgo`; do not commit a machine-specific replacement for normal users.

### Web service

Create the project, define the request/response models, add the API, and then generate code:

```bash
jgo new user-web \
  --module example.com/user-web \
  --type web
cd user-web

# Define the UpdateUserRequest and UserInfo Go structs under api/http/model/.
jgo api add UpdateUser \
  --method POST \
  --path /update_user \
  --request-params UpdateUserRequest \
  --response-data UserInfo

jgo api generate
# Implement the newly generated business method under internal/service/.
go test ./...
jgo run
```

A new Web project includes `/hello` and health checks, but `api/http/openapi.yaml` initially has no business operations. Normally, run `jgo api add` before generation instead of running `jgo generate` immediately after `jgo new`. Web services listen on `:8080` by default.

HTTP responses use the stable `{"code":0,"msg":"","data":...}` envelope:

```json
{"code": 0, "msg": "", "data": {"uid": 12345, "name": "Albert"}}
```

Business success is `code: 0`; HTTP status represents the transport result, while the integer `code` represents the business result.

### gRPC service

`jgo tools install` installs the locked Buf and protobuf generators into the current Go development environment. Run it once per environment, not once per project:

```bash
jgo new user-rpc \
  --module example.com/user-rpc \
  --type grpc
cd user-rpc

jgo tools install # Run the first time this environment uses JGO gRPC.
jgo doctor
jgo rpc add GetUser --service UserRpcService
# Edit GetUserRequest; GetUserResponse already has code/msg, so add business fields from number 3.
jgo rpc generate
# Implement the generated UserRpcServiceGetUser method under internal/service/.
go test ./...
jgo run
```

The initial protobuf service name is derived from the project name; for example, `user-rpc` becomes `UserRpcService` with a sample `Echo` RPC. Keep or remove the sample when establishing the real contract. The gRPC Health service is always registered, and its address is read from `configs/local.yaml`.

JGO locks Buf `1.46.0`, `protoc-gen-go` `1.36.7`, and `protoc-gen-go-grpc` `1.5.1`. gRPC business methods use `<Service><RPC>` names, such as `UserRpcServiceGetUser`; public protobuf service and RPC names remain unchanged.

Every JGO RPC response uses non-optional `int32 code = 1` and `string msg = 2`; business success is `0`. User-defined business fields start at number `3` and use `optional` only when they need to distinguish absence from an explicit zero value. `jgo call grpc` displays zero values such as `0`, `""`, and `false` for ordinary fields while still omitting unset optional/message fields.

`jgo doctor` and generation commands enforce this convention for local and cross-file RPC responses and fail when the standard `code/msg` fields are missing.

When a business method explicitly returns `jgo/errors.Error`, the generated gRPC transport builds a Response containing its `code/msg` and keeps the gRPC status `OK`. Unknown errors, panics, cancellation, and timeouts that cannot produce a valid business Response use a non-`OK` gRPC status. Business codes are not duplicated in gRPC status details.

### Mixed service

A mixed project maintains both OpenAPI and protobuf contracts while sharing one business layer and application lifecycle:

```bash
jgo new user-service \
  --module example.com/user-service \
  --type mixed
cd user-service

jgo tools install # Skip if the locked tools are already installed in this environment.
jgo doctor
# Add the required APIs and complete their structs/proto messages as shown above.
jgo generate     # Generate both HTTP and gRPC code.
go test ./...
jgo run
```

Mixed projects listen on HTTP `:8080` and gRPC `:9090` by default; YAML, environment variables, or command-line flags can override both addresses.

## Existing service: add an API

### Add an HTTP API

```bash
# 1. Add or reuse request and response Go structs under api/http/model/.
# 2. Add the operation to the OpenAPI contract.
jgo api add GetUser \
  --method GET \
  --path /get_user \
  --request uid:int64:required:query \
  --response-data UserInfo

# 3. Regenerate only the HTTP code.
jgo api generate
# 4. Implement the new business method; existing methods are preserved.
go test ./...
```

### Add a gRPC API

For example, add `GetUser` to an existing `UserService` that already has other RPCs:

```bash
jgo doctor       # Verify the locked tool versions in the current environment.
jgo rpc add GetUser --service UserService
# Edit the request; the response already has code=1/msg=2, so add business fields from number 3.
jgo rpc generate
# Implement the generated UserServiceGetUser method.
go test ./...
```

If the same service name occurs in multiple proto files, select one with `--file api/proto/.../service.proto`. `jgo rpc generate` regenerates protobuf and transport code but never overwrites existing business methods.

Choose a generation command according to the changed contracts:

| Command | Scope |
| --- | --- |
| `jgo api generate` | HTTP/OpenAPI code only |
| `jgo rpc generate` | gRPC/protobuf code only |
| `jgo generate` | All HTTP and gRPC code in the project; useful for mixed projects and CI |

## Debug APIs

Use the same JSON input for both protocols. JGO reads OpenAPI or protobuf descriptors and does not generate one-off debug programs:

```bash
jgo list
jgo call http GetUser --addr http://127.0.0.1:8080 --data '{"uid":12345}'
jgo call grpc UserRpcService.Echo --addr 127.0.0.1:9090 --data '{"message":"hello"}'
```

Both call commands support repeatable `--header 'Name: Value'` metadata and `--timeout`. gRPC prefers server Reflection and automatically falls back to protobuf files under `api/proto/`.

A generated project's README documents the stable workflow instead of duplicating an API inventory that can become stale. OpenAPI/proto files are the contract source of truth; use `jgo list` to inspect current HTTP and gRPC interfaces.

## Developer workflow

```bash
jgo doctor
jgo generate
jgo list
jgo run
jgo build
```

Generate Bash or Zsh completion with `jgo completion bash` and `jgo completion zsh`. The versioning and release checklist is documented in [docs/releasing.md](docs/releasing.md).

```bash
jgo --version
```

## Development verification

Run the complete local quality gate from the repository root:

```bash
make tools
make ci
```

This runs formatting checks, unit tests, race tests, `go vet`, CLI build, and real generation/build checks for web, gRPC, and mixed projects.

## Documentation

- [Documentation index](docs/README.md)
- [Installation and quick start](docs/getting-started.md)
- [CLI command reference](docs/command-reference.md)
- [Dependencies and locked versions](docs/dependencies.md)
- [Web, gRPC, and mixed examples](docs/examples.md)
- [Architecture and implementation record](docs/architecture-and-roadmap.md)
- [Release process](docs/releasing.md)

## License

Apache License 2.0.
