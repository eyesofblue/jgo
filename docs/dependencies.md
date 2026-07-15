# JGO 依赖说明

JGO 的目标是不依赖任何私有基础设施，可以作为独立的通用框架使用。日志、配置中心、注册发现和监控等私有能力不属于必需依赖，后续可通过组件和中间件扩展接入。

## 基础环境

| 依赖 | 要求 | 用途 |
| --- | --- | --- |
| Go | `1.24.0` 或更高 | 编译 JGO、CLI、生成项目和 protobuf 工具 |
| Git | 非强制 | 获取源码和版本管理 |
| Bash | 仅开发验收脚本需要 | 执行 `scripts/verify-generation.sh` |

只有项目实际包含本地 `.proto` 契约时才需要下列代码生成工具。只通过 `rpc server/client bind` 使用公共协议的 external-only 服务不需要 Buf。

## gRPC/Buf 工具链

| 工具 | 锁定版本 | 用途 |
| --- | --- | --- |
| `buf` | `1.46.0` | protobuf lint 和生成编排 |
| `protoc-gen-go` | `1.36.7` | 生成 protobuf Go 类型 |
| `protoc-gen-go-grpc` | `1.5.1` | 生成 gRPC Go 接口 |

在 JGO 仓库或生成的项目中执行：

```bash
jgo tools install
jgo tools check
```

`jgo tools install` 使用 `GOTOOLCHAIN=local`，不会自动下载其他 Go 工具链。工具默认安装到 `GOBIN`，未设置时使用 `$(go env GOPATH)/bin`；该目录必须位于 `PATH`。可通过下面的命令检查：

```bash
jgo tools check
jgo doctor
```

JGO 不会静默下载或切换 Go 工具链。`jgo doctor`、`jgo generate` 和 `jgo pb generate` 只在发现本地 proto 后检查工具；空协议和 external-only 项目不会报缺少 Buf。

## Go module 依赖

依赖由根目录的 `go.mod` 和 `go.sum` 锁定。主要直接依赖及职责如下：

| 模块 | 当前版本 | 用途 |
| --- | --- | --- |
| `github.com/spf13/cobra` | `v1.10.2` | CLI 命令、参数和补全 |
| `github.com/getkin/kin-openapi` | `v0.127.0` | 读取和校验 OpenAPI 契约 |
| `github.com/oapi-codegen/oapi-codegen/v2` | `v2.4.1` | OpenAPI Go 代码生成 |
| `github.com/bufbuild/protocompile` | `v0.14.1` | 读取 protobuf 描述符 |
| `google.golang.org/grpc` | `v1.79.1` | gRPC 服务端、客户端和 Reflection |
| `google.golang.org/protobuf` | `v1.36.11` | protobuf 运行时和动态消息 |
| `gopkg.in/yaml.v3` | `v3.0.1` | YAML 读写 |
| `golang.org/x/mod` | `v0.32.0` | 读取和检查 `go.mod` |
| `go.opentelemetry.io/otel` | `v1.41.0` | Trace Context、Tracer Provider 和标准传播器 |
| `go.opentelemetry.io/otel/sdk` | `v1.41.0` | Trace 采样、批处理和生命周期 |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc` | `v1.41.0` | 可选 OTLP/gRPC trace 上报 |
| `go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc` | `v1.41.0` | 可选 OTLP/gRPC Metrics 上报 |
| `github.com/prometheus/client_golang` | `v1.23.2` | Prometheus exporter、RED 与 Go runtime Metrics |
| `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` | `v0.66.0` | HTTP server instrumentation |
| `go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc` | `v0.66.0` | gRPC server/client instrumentation |

OpenTelemetry `v1.41.0`/`v0.66.0` 是当前采用的 Go 1.24 兼容组合；更新的 `v1.42.0+`/`v0.67.0+` 要求 Go 1.25，因此在 JGO 提升最低 Go 版本前不升级。OpenTelemetry 全部属于 Go module 依赖，不要求用户额外安装二进制工具。

间接依赖以 `go.mod` 为准，不应手工单独安装。执行以下命令可校验依赖完整性并检查 `go.mod/go.sum` 是否需要整理：

```bash
go mod verify
go mod tidy -diff
```

## 新项目中的 JGO 版本

新项目默认依赖：

```text
github.com/eyesofblue/jgo v0.4.1
```

- 正常使用：通过 `--jgo-version` 指定已发布版本。
- 开发框架：通过 `--jgo-replace /absolute/path/to/jgo` 增加本地 `replace`。
- `replace` 包含本机绝对路径，不适合提交给其他开发者或用于正式发布。
- proto 项目是独立的公共协议 module，不依赖 JGO 运行时，因此其 `go.mod` 不包含 JGO require/replace。

## 运行时端口与协议

- Web：HTTP/JSON，默认监听 `:8080`。
- gRPC：gRPC/protobuf，默认监听 `:9090`。
- mixed：同一应用生命周期同时启动两个服务。
- proto：没有运行进程，只提供 `.proto` 和生成的公共 Go 包。
- Management：所有服务默认监听 `:9091`，提供 `/healthz`、`/readyz` 和 `/metrics`。

框架运行不要求数据库、Redis、消息队列、服务注册中心或配置中心。

## mixed 项目的业务方法命名

HTTP 业务方法使用 OpenAPI `operationId`，例如 `GetUser`。项目自身 protobuf 业务方法使用 `<Service><RPC>`，例如 `UserServiceGetUser`；外部 server binding 从完整 import path 提取稳定前缀，使用 `<PackagePath><Service><RPC>`，例如 `CompanyUserV2UserServiceGetUser`。因此，即使 v1/v2 的 Go package 都显式命名为 `user`，也可以同时绑定；规范化碰撞时为后绑定项增加稳定 path 摘要。旧 manifest 尚未记录 `business` 且旧业务方法仍存在时，doctor/generate 会要求先显式重命名，随后复用该实现而不会创建重复桩。
