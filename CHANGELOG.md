# Changelog

All notable JGO changes are documented here. JGO follows Semantic Versioning.

## v0.1.0 - Unreleased

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

### Compatibility

- Module: `github.com/eyesofblue/jgo`
- Minimum Go version: `1.22.0`
- Buf: `1.46.0`
- `protoc-gen-go`: `1.36.7`
- `protoc-gen-go-grpc`: `1.5.1`
