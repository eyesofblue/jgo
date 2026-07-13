# JGO

[English](README.md) | [简体中文](README.zh-CN.md)

JGO is a standalone Go service framework and project scaffolding tool for HTTP/JSON and gRPC/protobuf services.

Runtime support, project scaffolding, contract generation, unified debugging, and developer workflow commands are available. Start with the [documentation index](docs/README.md) or the [quick-start guide](docs/getting-started.md).

## Module

```text
github.com/eyesofblue/jgo
```

The first release line is `v0.1.x`; see [CHANGELOG.md](CHANGELOG.md).

## Prerequisites

| Project type | Required software |
| --- | --- |
| `web` | Go `1.22.0` or later |
| `grpc` | Go `1.22.0` or later, Buf `1.46.0`, `protoc-gen-go` `1.36.7`, `protoc-gen-go-grpc` `1.5.1` |
| `mixed` | The same Go and protobuf toolchain as `grpc` |

JGO has no required database, Redis, message queue, service registry, configuration center, or other private infrastructure. Private infrastructure can be integrated later through application components and HTTP/gRPC middleware.

Make sure the Go binary directory is in `PATH`:

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

## Install

Install a published version:

```bash
go install github.com/eyesofblue/jgo/cmd/jgo@latest
jgo --version
```

Before the first release tag is available, build from source after cloning the repository:

```bash
go build -trimpath -o bin/jgo ./cmd/jgo
./bin/jgo --version
```

For gRPC and mixed projects, install the locked generators from the JGO repository or from a generated project:

```bash
make tools
command -v buf protoc-gen-go protoc-gen-go-grpc
```

JGO never installs these tools silently. Exact module and tool versions are documented in [docs/dependencies.md](docs/dependencies.md).

## Quick start

Create one of the three supported project types:

```bash
jgo new demo-web \
  --module example.com/demo-web \
  --type web

jgo new demo-grpc \
  --module example.com/demo-grpc \
  --type grpc

jgo new demo-mixed \
  --module example.com/demo-mixed \
  --type mixed
```

The generated project defaults to `github.com/eyesofblue/jgo v0.1.0`. During local framework development, add `--jgo-replace /absolute/path/to/jgo`; do not commit a machine-specific replacement for normal users.

Enter the generated project and verify the environment:

```bash
cd demo-web
jgo doctor
jgo generate
jgo run
```

Web services listen on `:8080` by default. gRPC services listen on `:9090`; mixed projects start both under one application lifecycle.

## Add an HTTP API

Define complex request and response models as Go structs in `api/http/model/`, then add and generate the contract. JGO intentionally supports RPC-style HTTP paths such as `GET /get_user?uid=12345` and `POST /update_user`:

```bash
jgo api add UpdateUser \
  --method POST \
  --path /update_user \
  --request-params UpdateUserRequest \
  --response-data UserInfo

jgo api generate
```

HTTP responses use the stable `{"code":0,"msg":"","data":...}` envelope. HTTP status and integer business error codes are managed separately.

```json
{"code": 0, "msg": "", "data": {"uid": 12345, "name": "Albert"}}
```

Business success is `code: 0`; transport failures use the appropriate HTTP status without reusing the business code field.

## Add a gRPC API

gRPC contracts use protobuf and the locked Buf toolchain:

```bash
make tools
jgo rpc add GetUser --service GreeterService
# Edit the generated GetUserRequest and GetUserResponse messages.
jgo rpc generate
```

JGO locks Buf `1.46.0`, `protoc-gen-go` `1.36.7`, and `protoc-gen-go-grpc` `1.5.1`, all compatible with the Go 1.22.0 baseline. Generated protobuf and transport files are replaceable; existing business service methods are preserved.

gRPC business methods use `<Service><RPC>` names, for example `GreeterServiceGetUser`, which prevents HTTP/gRPC method collisions in mixed projects. The public protobuf service and RPC names remain unchanged.

## Debug APIs

Use the same JSON input for both protocols. JGO reads OpenAPI or protobuf descriptors and does not generate one-off debug programs:

```bash
jgo list
jgo call http GetUser --addr http://127.0.0.1:8080 --data '{"uid":12345}'
jgo call grpc GreeterService.Echo --addr 127.0.0.1:9090 --data '{"message":"hello"}'
```

Both call commands support repeatable `--header 'Name: Value'` metadata and `--timeout`. gRPC prefers server Reflection and automatically falls back to protobuf files under `api/proto/`.

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
