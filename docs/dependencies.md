# JGO 依赖说明

JGO 的目标是不依赖任何私有基础设施，可以作为独立的通用框架使用。日志、配置中心、注册发现和监控等私有能力不属于必需依赖，后续可通过组件和中间件扩展接入。

## 基础环境

| 依赖 | 要求 | 用途 |
| --- | --- | --- |
| Go | `1.22.0` 或更高 | 编译 JGO、CLI 和生成的项目 |
| Git | 非强制 | 获取源码和版本管理 |
| Bash | 仅开发验收脚本需要 | 执行 `scripts/verify-generation.sh` |

纯 Web 项目只需要 Go。创建 gRPC 或 mixed 项目时，还需要下列代码生成工具。

## gRPC/Buf 工具链

| 工具 | 锁定版本 | 用途 |
| --- | --- | --- |
| `buf` | `1.46.0` | protobuf lint 和生成编排 |
| `protoc-gen-go` | `1.36.7` | 生成 protobuf Go 类型 |
| `protoc-gen-go-grpc` | `1.5.1` | 生成 gRPC Go 接口 |

在 JGO 仓库或生成的项目中执行：

```bash
make tools
```

`go install` 默认把工具安装到 `$(go env GOPATH)/bin`；该目录必须位于 `PATH`。可通过下面的命令检查：

```bash
command -v buf
command -v protoc-gen-go
command -v protoc-gen-go-grpc
jgo doctor
```

JGO 不会静默下载或安装这些工具。`jgo doctor` 和 `jgo generate` 在缺少工具时会明确报错。

## Go module 依赖

依赖由根目录的 `go.mod` 和 `go.sum` 锁定。主要直接依赖及职责如下：

| 模块 | 当前版本 | 用途 |
| --- | --- | --- |
| `github.com/spf13/cobra` | `v1.10.2` | CLI 命令、参数和补全 |
| `github.com/getkin/kin-openapi` | `v0.127.0` | 读取和校验 OpenAPI 契约 |
| `github.com/oapi-codegen/oapi-codegen/v2` | `v2.4.1` | OpenAPI Go 代码生成 |
| `github.com/bufbuild/protocompile` | `v0.14.1` | 读取 protobuf 描述符 |
| `google.golang.org/grpc` | `v1.71.3` | gRPC 服务端、客户端和 Reflection |
| `google.golang.org/protobuf` | `v1.36.7` | protobuf 运行时和动态消息 |
| `google.golang.org/genproto/googleapis/rpc` | `v0.0.0-20250115164207-1a7da9e5054f` | Google RPC 错误详情 |
| `gopkg.in/yaml.v3` | `v3.0.1` | YAML 读写 |
| `golang.org/x/mod` | `v0.17.0` | 读取和检查 `go.mod` |

间接依赖以 `go.mod` 为准，不应手工单独安装。执行以下命令可校验依赖完整性并检查 `go.mod/go.sum` 是否需要整理：

```bash
go mod verify
go mod tidy -diff
```

## 新项目中的 JGO 版本

新项目默认依赖：

```text
github.com/eyesofblue/jgo v0.1.0
```

- 正常使用：通过 `--jgo-version` 指定已发布版本。
- 开发框架：通过 `--jgo-replace /absolute/path/to/jgo` 增加本地 `replace`。
- `replace` 包含本机绝对路径，不适合提交给其他开发者或用于正式发布。

## 运行时端口与协议

- Web：HTTP/JSON，默认监听 `:8080`。
- gRPC：gRPC/protobuf，默认监听 `:9090`。
- mixed：同一应用生命周期同时启动两个服务。

框架运行不要求数据库、Redis、消息队列、服务注册中心或配置中心。

## mixed 项目的业务方法命名

HTTP 业务方法使用 OpenAPI `operationId`，例如 `GetUser`。gRPC 业务方法使用 `<Service><RPC>`，例如 `GreeterServiceGetUser`，从而避免 mixed 项目中 HTTP 与 gRPC 同名接口发生 Go 方法冲突，也避免不同 gRPC service 复用同一个 RPC 名时冲突。
