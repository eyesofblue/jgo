# JGO

[English](README.md) | [简体中文](README.zh-CN.md)

JGO 是一个可独立使用的 Go 服务框架和项目脚手架，支持 HTTP/JSON 与 gRPC/protobuf 服务。

目前已经提供运行时、项目脚手架、契约生成、统一接口调试和开发流程命令。可以从[文档索引](docs/README.md)或[快速入门](docs/getting-started.md)开始了解。

## Module

```text
github.com/eyesofblue/jgo
```

当前发布版本为 `v0.3.0`，版本能力见 [CHANGELOG.md](CHANGELOG.md)。

## 前置依赖

| 项目类型 | 必需软件 |
| --- | --- |
| `web` | Go `1.24.0` 或更高版本 |
| `grpc` | Go `1.24.0` 或更高版本、Buf `1.46.0`、`protoc-gen-go` `1.36.7`、`protoc-gen-go-grpc` `1.5.1` |
| `mixed` | 与 `grpc` 相同的 Go 和 protobuf 工具链 |
| `proto` | 与 `grpc` 相同的 Go 和 protobuf 工具链；不依赖 JGO 运行时 |

JGO 不强制依赖数据库、Redis、消息队列、服务注册中心、配置中心或其他私有基础设施。后续可以通过应用组件和 HTTP/gRPC 中间件接入私有基础设施。

确保 Go 的二进制目录已经加入 `PATH`：

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

## 安装

安装已经发布的版本：

```bash
go install github.com/eyesofblue/jgo/cmd/jgo@v0.3.0
jgo --version
```

需要始终安装最新发布版本时，可以把版本改为 `@latest`。

参与 JGO 框架开发时，也可以克隆仓库后从源码构建：

```bash
go build -trimpath -o bin/jgo ./cmd/jgo
./bin/jgo --version
```

gRPC、mixed 和 proto 项目还需要安装锁定版本的生成工具。每个 Go 开发环境执行一次：

```bash
jgo tools install
jgo tools check
```

JGO 使用 `GOTOOLCHAIN=local`，不会静默下载或切换 Go 工具链。完整 module 依赖和工具版本见[依赖说明](docs/dependencies.md)。

## 新项目：创建服务和首个接口

生成的服务项目默认依赖 `github.com/eyesofblue/jgo v0.3.0`，proto 项目不依赖 JGO 运行时。所有项目默认采用当前 `go env GOVERSION`，也可以用 `--go-version` 显式指定。创建过程会执行 `go mod tidy` 并生成 `go.sum`；离线环境可使用 `--skip-tidy`。开发和联调 JGO 本身时，可以增加 `--jgo-replace /absolute/path/to/jgo`；正常使用时不要提交包含本机路径的 `replace`。

### Web 服务

先创建项目，再定义请求/返回结构、增加接口并生成代码：

```bash
jgo new user-web \
  --module example.com/user-web \
  --type web
cd user-web

# 在 api/http/model/ 中定义 UpdateUserRequest 和 UserInfo Go struct。
jgo api add UpdateUser \
  --method POST \
  --path /update_user \
  --request-params UpdateUserRequest \
  --response-data UserInfo

jgo api generate
# 实现 internal/service/ 中新生成的业务方法。
go test ./...
jgo run
```

新 Web 项目自带 `/hello` 和健康检查；`api/http/openapi.yaml` 初始没有业务接口，因此通常先执行 `jgo api add`，不必在 `jgo new` 后立即执行 `jgo generate`。Web 服务默认监听 `:8080`。

HTTP 响应统一使用 `{"code":0,"msg":"","data":...}`：

```json
{"code": 0, "msg": "", "data": {"uid": 12345, "name": "Albert"}}
```

业务成功码固定为 `code: 0`。HTTP status 表示传输层结果，整数 `code` 表示业务结果，两者不会混用。

### gRPC 服务

`jgo tools install` 把锁定版本的 Buf 和 protobuf 插件安装到当前 Go 开发环境；同一环境中只需安装一次，不需要每个项目都执行：

```bash
jgo new user-rpc \
  --module example.com/user-rpc \
  --type grpc
cd user-rpc

jgo tools install # 当前开发环境首次使用 JGO gRPC 时执行
jgo doctor
jgo rpc pbapi add GetUser --service UserRpcService
# 编辑 GetUserRequest；GetUserResponse 已有 code/msg，业务字段从编号 3 开始。
jgo rpc generate
# 实现 internal/service/ 中新生成的 UserRpcServiceGetUser 方法。
go test ./...
jgo run
```

新 gRPC 项目的 service 名根据项目名生成；例如 `user-rpc` 对应 `UserRpcService`，并自带 `Echo` 示例。示例可以保留或在形成正式契约时删除。gRPC Health 服务始终注册，默认地址从 `configs/local.yaml` 读取。

JGO 锁定 Buf `1.46.0`、`protoc-gen-go` `1.36.7` 和 `protoc-gen-go-grpc` `1.5.1`。gRPC 业务方法使用 `<Service><RPC>` 命名，例如 `UserRpcServiceGetUser`；对外 protobuf service 和 RPC 名称保持不变。

JGO 的每个 RPC response 固定使用非 optional 的 `int32 code = 1` 和 `string msg = 2`，成功业务码为 `0`。用户定义的业务字段从编号 `3` 开始，并根据是否需要区分“未设置”和“显式零值”自行决定是否使用 `optional`。`jgo call grpc` 会显示普通字段的 `0`、`""`、`false` 等零值，但仍省略未设置的 optional/message 字段。

`jgo doctor` 和生成命令会强制检查所有本地及跨文件引用的 RPC Response；缺少标准 `code/msg` 时直接失败。

业务方法显式返回 `jgo/errors.Error` 时，生成的 gRPC transport 会构造只包含对应 `code/msg` 的 Response，并保持 gRPC status 为 `OK`。未知错误、panic、请求取消和超时等无法形成有效业务 Response 的错误使用非 `OK` gRPC status。业务错误码不会再放入 gRPC status details。

### 公共 protobuf 协议仓库

多个服务端和调用方需要共享同一套带版本的协议时，使用 `proto` 项目。项目名可以自由指定，`company-api` 只是示例：

```bash
jgo new company-api \
  --module example.com/company-api \
  --type proto
cd company-api

jgo tools install # 当前 Go 环境已经安装过时跳过。
jgo rpc pbapi add GetUser --service CompanyApiService
# 后续增加新的业务域 Service：
jgo rpc pbservice add OrderService
# 完善 api/proto/company_api/v1/service.proto 中的请求和返回字段。
jgo rpc generate
jgo list
go test ./...
```

proto 项目会保留初始生成的 `CompanyApiService.Echo` 示例。后续需要增加业务域时，在同一个仓库执行 `rpc pbservice add`，不需要再次执行 `jgo new`。proto 项目没有 `cmd/server`、服务配置、业务层，也不依赖 JGO 运行时。生成命令只更新 `gen/pb` 下的公共 Go 包。`.proto` 和 `gen/pb` 都应提交，随后为协议 module 打 tag；服务端和调用方可以引入 `example.com/company-api/gen/pb/company_api/v1` 等包。`jgo run` 和面向服务端二进制的 `jgo build` 会明确拒绝 proto 项目。

在 gRPC 或 mixed 服务端接入已经发布的 Service：

```bash
jgo rpc server add UserService \
  --module example.com/company-api@v0.1.1
# JGO 自动发现唯一 package、增加 module 依赖、生成 server adapter 和缺失的
# 业务方法，并执行 go mod tidy。
```

在任意 web、gRPC 或 mixed 服务中接入类型安全客户端：

```bash
jgo rpc client add UserService \
  --module example.com/company-api@v0.1.1 \
  --name user \
  --address 127.0.0.1:9090
```

生成的业务 Service 通过 `s.RPC.User` 直接取得 protobuf client interface。`client add` 会写入 `configs/local.yaml` 的 `rpc_client.user`，并通过 `client/grpcx` 完成生命周期管理。客户端 `--name` 是稳定的代码标识，创建后不支持重命名；需要另一个名称时新增一份 client。地址、超时、TLS 等运行参数始终可以直接修改 YAML。`v0.1.1` 等 module 发布版本不决定 protobuf API 的 `v1/v2`；JGO 会在所有生成包中按 Service 查找。相同 Service 出现在多个 package 时，使用 `--package` 指定完整 import path。

### mixed 服务

mixed 项目同时维护 OpenAPI 和 protobuf 契约，并共用业务层和应用生命周期：

```bash
jgo new user-service \
  --module example.com/user-service \
  --type mixed
cd user-service

jgo tools install # 已在当前环境安装过时可跳过
jgo doctor
# 按上面的 Web/gRPC 流程增加所需接口并完善 struct/proto。
jgo generate     # 同时生成 HTTP 和 gRPC 代码
go test ./...
jgo run
```

mixed 项目默认同时监听 HTTP `:8080` 和 gRPC `:9090`，地址可以通过 YAML、环境变量或命令参数覆盖。

## Trace 与结构化日志

生成项目默认启用 OpenTelemetry Trace Context，HTTP 和 gRPC 使用标准 W3C `traceparent` 透传。OTLP 上报默认关闭，因此项目可以在没有 Collector、Jaeger 或 Tempo 的环境中独立运行。HTTP 响应会额外返回 `X-Trace-ID`，便于人工定位日志。

```yaml
telemetry:
  tracing:
    enabled: true
    sample_ratio: 0.1
    exporter:
      enabled: false
      endpoint: "127.0.0.1:4317"
      insecure: true
```

启用 exporter 后，应用会通过 OTLP/gRPC 上报 span；关闭 exporter 时仍会创建和透传 `trace_id`，但不会记录或发送 span。应用退出时会在 HTTP/gRPC Server 停止后刷新并关闭 tracer provider。

业务日志使用结构化 `logx` API：

```go
logx.InfoCtx(ctx, "get user completed", "uid", uid)
logx.ErrorCtx(ctx, "get user failed", "uid", uid, "err", err)
```

`DebugCtx`、`InfoCtx`、`WarnCtx` 和 `ErrorCtx` 会从 context 自动增加 `trace_id`、`span_id`。不提供 printf 风格的 `InfoCtxf`/`ErrorCtxf`。

gRPC 业务错误仍使用 `response.code/msg` 且 gRPC status 保持 `OK`；生成的 transport 会给当前 span 增加 `jgo.business_code`、`jgo.business_message`，使链路系统可以识别业务失败。

## 调用下游 gRPC 服务

`client/grpcx` 以应用组件的形式管理具名、可复用的 gRPC 连接。连接采用延迟建立：下游服务暂时不可用不会阻止当前进程启动，真正发起 RPC 时会返回 gRPC `Unavailable`。下游依赖状态不影响 `/healthz`，该接口只表示当前进程是否存活。

在 `rpc_client` 下配置依赖：

```yaml
rpc_client:
  user:
    address: "dns:///user-rpc:9090"
    timeout: 3s
    tls:
      enabled: false
      server_name: ""
      ca_file: ""
```

生成的 `rpc client add` 代码会自动使用该运行时。手工接入时，创建连接管理器、取得连接、构造 protobuf 生成的 client，并在对外提供服务前把管理器加入应用生命周期：

```go
clients, err := clientgrpcx.New(map[string]clientgrpcx.Config{
    "user": {
        Address: configuration.RPCClient["user"].Address,
        Timeout: configuration.RPCClient["user"].Timeout.Duration,
    },
})
if err != nil {
    return err
}
connection, err := clients.Conn("user")
if err != nil {
    return err
}
userClient := userv1.NewUserServiceClient(connection)
application.Add(clients)
```

unary RPC 默认超时为 3 秒，具名客户端可以覆盖；调用方 context 已有 deadline 时，以该 deadline 和客户端 timeout 中更早到期的一个为准。TLS 支持系统根证书和额外 CA 文件。JGO 会禁用配置式 gRPC retry，不自动重试业务调用；OpenTelemetry Trace Context 会自动透传，传输错误通过 context-aware 结构化日志记录。

## 存量服务：新增接口

### 新增 HTTP 接口

```bash
# 1. 在 api/http/model/ 中新增或复用请求和返回 Go struct。
# 2. 把接口加入 OpenAPI 契约。
jgo api add GetUser \
  --method GET \
  --path /get_user \
  --request uid:int64:required:query \
  --response-data UserInfo

# 3. 只重新生成 HTTP 代码。
jgo api generate
# 4. 实现新生成的业务方法，已有业务方法不会被覆盖。
go test ./...
```

### 新增 gRPC 接口

假设存量 `UserService` 已有多个 RPC，现在新增 `GetUser`：

```bash
jgo doctor       # 检查当前环境中的锁定工具版本
jgo rpc pbapi add GetUser --service UserService
# 编辑 request；response 已有 code=1、msg=2，业务字段从编号 3 开始。
jgo rpc generate
# 实现新生成的 UserServiceGetUser 方法。
go test ./...
```

如果同名 service 出现在多个 proto 文件中，通过 `--file api/proto/.../service.proto` 指定文件。`jgo rpc generate` 会重新生成 protobuf 和 transport 代码，但不会覆盖已有业务方法。

生成命令按修改范围选择：

| 命令 | 生成范围 |
| --- | --- |
| `jgo api generate` | 只生成 HTTP/OpenAPI 代码 |
| `jgo rpc generate` | 只生成 gRPC/protobuf 代码 |
| `jgo generate` | 生成当前项目包含的全部 HTTP 和 gRPC 代码，适合 mixed 项目或 CI |

## 调试接口

HTTP 和 gRPC 使用相同的 JSON 输入方式。JGO 读取 OpenAPI 或 protobuf descriptor，不会为每个接口生成临时调试程序：

```bash
jgo list
jgo call http GetUser --addr http://127.0.0.1:8080 --data '{"uid":12345}'
jgo call grpc UserRpcService.Echo --addr 127.0.0.1:9090 --data '{"message":"hello"}'
```

两种调用命令都支持可重复的 `--header 'Name: Value'` 和 `--timeout`。gRPC 优先使用服务端 Reflection，失败时自动读取 `api/proto/` 下的本地 protobuf 文件。

生成项目自己的 README 只描述稳定工作流，不复制一份容易过期的接口清单。OpenAPI/proto 是协议真源，随时使用 `jgo list` 查看当前 HTTP 和 gRPC 接口。

## 开发流程

```bash
jgo doctor
jgo generate
jgo list
jgo run
jgo build
```

使用 `jgo completion bash` 或 `jgo completion zsh` 生成命令补全。版本规则和发布检查见[发布流程](docs/releasing.md)。

```bash
jgo --version
```

## 完整验证

在 JGO 仓库根目录执行完整质量检查：

```bash
make tools
make ci
```

该命令会执行格式检查、单元测试、竞态测试、`go vet`、CLI 构建，以及 web、gRPC、mixed、proto 四类项目的真实检查。生成验收还会启动彼此独立的 proto module、gRPC 服务端和 Web 调用方，端到端验证 trace_id 透传、3 秒客户端超时、`Unavailable` 行为，以及只表示当前进程状态的 `/healthz`。

## 文档

- [文档索引](docs/README.md)
- [安装与快速入门](docs/getting-started.md)
- [CLI 命令参考](docs/command-reference.md)
- [依赖与锁定版本](docs/dependencies.md)
- [Web、gRPC、mixed 和 proto 示例](docs/examples.md)
- [架构和实施记录](docs/architecture-and-roadmap.md)
- [发布流程](docs/releasing.md)

## License

Apache License 2.0。
