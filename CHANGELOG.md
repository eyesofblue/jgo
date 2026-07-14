# Changelog

All notable JGO changes are documented here. JGO follows Semantic Versioning.

## Unreleased

No changes yet.

## v0.3.0 - 2026-07-14

### Changed

- Replaced the public `middleware/requestid` API and `X-Request-ID` convention with OpenTelemetry Trace Context, `middleware/traceid`, W3C `traceparent`, and the `X-Trace-ID` response header. This is a pre-1.0 breaking change.
- HTTP and gRPC servers now use official OpenTelemetry instrumentation. Trace contexts are active by default in generated projects while OTLP export remains disabled unless configured.
- Generated applications own the tracer provider lifecycle and flush it after servers stop.
- Generated configuration now supports named outbound gRPC dependencies under `rpc_client`, including per-client timeout and TLS settings.
- Replaced `jgo rpc add` with the explicit `jgo rpc pbapi add` command without a compatibility alias.
- RPC server/client binding updates are transactional: generation or `go mod tidy` failures restore `go.mod`, `go.sum`, generated code, YAML, stubs, and the binding manifest.

### Added

- Structured `logx.DebugCtx`, `InfoCtx`, `WarnCtx`, and `ErrorCtx` APIs with automatic `trace_id` and `span_id` fields.
- Optional OTLP/gRPC exporter configuration with sampling, endpoint, and transport-security settings.
- `client/grpcx` application component for lazy named connections, default 3-second unary timeouts, earliest-deadline precedence, TLS, trace propagation, structured transport-error logging, and no configured business-call retries.
- Standalone `proto` project scaffolding for shared, versioned protobuf modules without a JGO runtime or server process.
- Protocol-aware protobuf generation that writes public `gen/pb` packages without service stubs or transport adapters in proto projects.
- `jgo rpc pbservice add` for adding additional protobuf Service definitions to an existing contract repository.
- `jgo rpc server add` for discovering and implementing a Service from a versioned shared protobuf module.
- `jgo rpc client add` for generated `rpc_client` configuration, managed connections, and direct typed protobuf client injection into business services.
- Runtime E2E coverage that starts a generated shared proto module, gRPC server, and Web caller and verifies trace propagation, timeout, `Unavailable`, and process-only health behavior.
- Deduplicated generated protobuf imports for multiple Services from one package and multiple named instances of the same Service.

## v0.2.0 - 2026-07-14

### Changed

- Raised the minimum Go version to 1.24.0 so macOS binaries contain Mach-O `LC_UUID` metadata without external linking.
- Generated projects default to the active Go version, support `--go-version`, run `go mod tidy` transactionally, and include `go.sum` immediately.
- Initial protobuf service names are derived from the project name instead of always using `GreeterService`.
- Generated server entrypoints load addresses and shutdown timeout from YAML, environment variables, or command-line flags.
- Generated RPC responses reserve non-optional `code = 1` and `msg = 2` fields; business fields start at field number 3.
- `jgo call grpc` emits default values for non-presence protobuf fields while preserving optional-field presence semantics.
- Generated unary transports return explicit JGO business errors through Response `code/msg` with gRPC status `OK`; transport and system failures remain non-`OK` statuses.

### Added

- `jgo tools install` and `jgo tools check` for locked protobuf tool installation and diagnostics without implicit Go toolchain switching.
- `--skip-tidy` for explicitly creating projects in offline environments.
- Mandatory cross-file validation that blocks generation when any RPC response does not follow the JGO response convention.
- Precise reporting of newly created service stubs and methods after generation.

### Compatibility

- Module: `github.com/eyesofblue/jgo`
- Minimum Go version: `1.24.0`
- Buf: `1.46.0`
- `protoc-gen-go`: `1.36.7`
- `protoc-gen-go-grpc`: `1.5.1`

## v0.1.0 - 2026-07-13

First public framework release candidate.

### Added

- Standalone application lifecycle with ordered startup and graceful shutdown.
- HTTP runtime with request IDs, access logs, recovery, timeouts, health checks, and standardized `{code,msg,data}` responses.
- gRPC runtime with request IDs, error mapping, recovery, Reflection, health service support, and graceful draining.
- `jgo new` scaffolding for `web`, `grpc`, and `mixed` projects.
- OpenAPI-driven `jgo api add` and `jgo api generate`, including complex Go struct request/response models.
- Buf/protobuf-driven `jgo rpc add` and `jgo rpc generate` with non-overwriting business stubs.
- Collision-safe gRPC business method names (`<Service><RPC>`) for mixed projects.
- Contract-driven `jgo call http`, `jgo call grpc`, and `jgo list` debugging commands.
- Developer workflow commands: `jgo doctor`, `jgo generate`, `jgo run`, and `jgo build`.
- Bash/Zsh completion, macOS/Linux CI, real generation consistency checks, and tag-based release archives.
- Real compilation checks for web, gRPC, and mixed generated projects, including complex HTTP struct bodies and object/list responses.
- macOS CI linking compatible with current runners while retaining the Go 1.22 minimum, and Go 1.24 release builds with Mach-O `LC_UUID` metadata.

### Compatibility

- Module: `github.com/eyesofblue/jgo`
- Minimum Go version: `1.22.0`
- Buf: `1.46.0`
- `protoc-gen-go`: `1.36.7`
- `protoc-gen-go-grpc`: `1.5.1`
