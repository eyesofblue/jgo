# JGO

[English](README.md) | 简体中文

JGO 是一个不依赖私有基础设施的 Go 服务框架和脚手架，支持 RPC 风格的 HTTP/JSON API 与 gRPC/protobuf。数据库、Redis、MQ、服务发现、认证中心等能力通过标准扩展接口接入。

Module：`github.com/eyesofblue/jgo`。当前主干面向 `v0.5.0`。

## 前置依赖

- 所有项目：Go `1.24.0` 或更高版本。
- 只有创建或生成本地 protobuf 协议时才需要 Buf `1.46.0`、`protoc-gen-go` `1.36.7`、`protoc-gen-go-grpc` `1.5.1`。
- 只绑定公共协议的 external-only gRPC 项目不需要安装 Buf。

```bash
go install github.com/eyesofblue/jgo/cmd/jgo@v0.5.0
jgo --version

# 当前 Go 环境第一次开发本地 protobuf 时执行一次
jgo tools install
jgo tools check
```

JGO 使用 `GOTOOLCHAIN=local`，不会偷偷下载或切换 Go。`jgo new` 会执行 `go mod tidy` 并生成 `go.sum`；`--skip-tidy` 仅用于离线或受控环境。

## 项目类型与空骨架

```bash
jgo new <name> --module <module> --type <web|grpc|mixed|proto>
```

- `web`：可运行的 HTTP 服务，OpenAPI 初始为空。
- `grpc`：可运行的空 gRPC 服务，不自带 Echo 或本地 proto。
- `mixed`：空 HTTP 与空 gRPC 组合服务。
- `proto`：可独立依赖的空公共协议 Go module，没有服务进程和 JGO 运行时依赖。

项目类型与 protobuf 来源相互独立：grpc/mixed 项目可以只绑定公共协议，也可以拥有本地协议，还可以两者同时存在。

空项目可以直接执行：

```bash
jgo generate   # 没有契约和绑定时安全 no-op
jgo list
jgo doctor
go test ./...
```

## HTTP API

JGO 保留 `GET /get_user?uid=12345`、`POST /update_user` 形式。复杂请求和返回值直接使用 Go struct：

```bash
jgo new user-web --module example.com/user-web --type web
cd user-web

# 先在 api/http/model 中定义 UpdateUserRequest、UserInfo
jgo api add UpdateUser --method POST --path /update_user \
  --request-params UpdateUserRequest \
  --response-data UserInfo
jgo api generate
```

统一响应：

```json
{
  "code": 0,
  "msg": "",
  "data": {"uid": 12345, "name": "Albert"}
}
```

HTTP status 表示 HTTP/传输结果，`code` 表示业务结果；成功业务码固定为 `0`，两者不会混用。

## 本地 protobuf 协议

当当前项目拥有协议时：

```bash
jgo pb service add UserService
jgo pb method add GetUser --service UserService
# 编辑 request；response 的 code/msg 固定为字段 1/2，业务字段从 3 开始
jgo pb generate
```

第一次增加 Service 时自动创建：

```text
api/proto/<项目名>/v1/service.proto
```

例如 `company-api` 默认得到 protobuf package `company_api.v1`。可以显式创建业务域或新 API 大版本：

```bash
jgo pb service add OrderService --package company.order.v1
jgo pb service add UserService --package company.user.v2
```

如果同一 package 分布在多个 proto 文件中，必须用 `--file` 明确目标，JGO 不会任意选择。`api/proto` 下的 symlink 会被拒绝，协议作者命令不会通过符号链接修改项目目录外文件。

协议检查：

```bash
jgo pb lint
jgo pb breaking --against '.git#branch=main'
```

生成的 proto/grpc/mixed 项目包含 PR 工作流，自动以 PR 目标分支执行 breaking check。删除 Method、复用字段编号、修改字段类型等不兼容变更会被阻断；真正的大版本升级使用新的 protobuf package（如 `company.user.v2`）。

## 公共协议与 Service 绑定

先创建公共协议 module：

```bash
jgo new company-api --module example.com/company-api --type proto
cd company-api
jgo pb service add UserService --package company.user.v1
jgo pb method add GetUser --service UserService
jgo pb generate
```

服务端绑定整个 protobuf Service：

```bash
jgo rpc server bind UserService \
  --module example.com/company-api@v0.1.0
```

客户端也绑定整个 Service，但业务代码只调用所需 Method：

```bash
jgo rpc client bind UserService \
  --module example.com/company-api@v0.1.0 \
  --name user \
  --address 127.0.0.1:9090
```

`--name` 是稳定的配置 key 和代码字段名。地址、超时、TLS、readiness 直接修改 `configs/local.yaml`。重复执行相同 `bind` 是幂等的；同一 package 下可从 `v0.1.0` 更新到 `v0.2.0`，不会覆盖运行配置。

服务端 binding 以 `package + Service` 为唯一身份。每个 binding 使用独立、由用户持有的 Handler；例如 `UserService.GetUser` 默认实现为 `UserHandler.GetUser`，不再把长方法名堆到应用 `Service` 上。同名 Service 并存时，为后续 binding 显式指定不同名称，例如 `--handler-name UserV2`，解绑仍需指定 package：

```bash
jgo rpc server unbind UserService \
  --package example.com/company-api/gen/pb/company/user/v1
```

`jgo doctor` 会校验 Handler 的完整 RPC 签名并在不匹配时展示 expected/actual；`jgo list` 会显示 `UserHandler`、`UserV2Handler` 等实际业务入口。v0.5 只调整 external server；本地 protobuf 仍使用 `Service.<Service><Method>`，暂不扩大到统一 Handler 迁移。

v0.5 使用 RPC manifest version 2。v0.4 manifest 不会被自动改写；先备份并删除 `.jgo/rpc.json`，重新创建 server/client binding，再把原 `Service.<生成方法名>` 的实现移入 `<Handler>.<RPC>`。

低频永久解除职责或依赖：

```bash
jgo rpc server unbind UserService
jgo rpc client unbind user
```

`unbind` 不修改公共协议，也不删除用户业务实现。客户端仍被业务代码引用时，解绑后的编译检查失败并自动回滚。

## 未发布协议与 go.work

`go.work` 是 Go 官方多 module 本地工作区，适合同时开发公共协议、服务端和客户端：

```bash
cd company
go work init
go work use ./company-api ./user-service ./web-gateway
```

活动 workspace 中的协议尚未发布时可以省略版本：

```bash
jgo rpc server bind UserService --module example.com/company-api
```

省略版本只解析当前 `go.work` 中的本地 module，找不到就报错。显式 `@version` 不读取 workspace 中的未发布代码，但会遵循与该版本匹配的用户 `go.mod replace`。除 `jgo new` 的临时原子生成阶段外，其他标准 Go 命令仍尊重活动 workspace。生产依赖验证建议：

```bash
GOWORK=off go test ./...
```

## 生成、查看和运行

```bash
jgo api generate   # 只生成 HTTP
jgo pb generate    # 只生成本地 protobuf
jgo generate       # 重建 HTTP、本地 protobuf、外部 server/client 绑定
jgo list           # 展示 HTTP、本地 gRPC、外部 server 和外部 client
jgo doctor         # 检查工具、外部 binding manifest/module/config 与 workspace/replace
jgo run --config configs/local.yaml
jgo build
```

`jgo run` 会直接把未知参数传给服务进程，不再要求写成 `jgo run -- --config ...`。

## gRPC 业务错误

每个 RPC response 必须声明：

```proto
int32 code = 1;
string msg = 2;
```

业务返回 `jgo/errors.Error` 时，transport 把 `code/msg` 写入 Response，gRPC status 保持 `OK`。网络、panic、取消、超时等无法形成业务 Response 的错误使用非 OK status。

错误码在 Go 中统一声明：

```go
var UserNotFound = jgoerrors.Define(
    40401,
    "USER_NOT_FOUND",
    "user not found",
    http.StatusNotFound,
)

var Catalog = jgoerrors.MustCatalog(UserNotFound)
```

Catalog 检查重复 code/name，并统一负责 RPC code 到 HTTP status 的映射。未知下游业务码保留 code/msg，但 HTTP status 使用 500 并在 Metrics 中归为 `unknown`。

跨服务使用独立的公共错误码 Go module，并在每个进程启动时合并；不同仓库声明的冲突也会立即失败：

```go
var Catalog = jgoerrors.MustMergeCatalogs(
    sharedcodes.Catalog,
    jgoerrors.MustCatalog(UserNotFound),
)
```

Web 调用 RPC 后只需通过同一个 Catalog 转换，不再手写 `switch code`：

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

## 生产安全

HTTP 请求体默认限制为 4 MiB，可通过 `service.max_body_bytes` 调整。生成的 JSON handler 只接受一个完整 JSON 文档，并在进入业务方法前校验 OpenAPI required、类型、格式和范围约束；超限返回 413，契约不匹配或尾随第二个 JSON 值返回 400。

本地配置显式开启 Reflection，框架默认关闭：

```yaml
grpc:
  reflection:
    enabled: true
  tls:
    enabled: false
    cert_file: ""
    key_file: ""
    client_auth: none # 或 require_and_verify
    client_ca_file: ""
```

TLS/mTLS 配置缺失、证书无效或文件不可读时服务启动失败，不降级为明文。`internal/securityx/security.go` 是用户拥有且同时应用于 HTTP/gRPC 的认证授权接入点；框架定义 `Authenticator`/`Authorizer`。HTTP 认证/授权失败分别返回 401/403，gRPC 返回 `Unauthenticated`/`PermissionDenied`，不绑定 JWT 或私有权限中心。

YAML 使用严格模式，未知字段会阻止启动，例如误写 `timeuot` 不会被忽略。

## Management、Metrics 与 Readiness

所有服务都有独立管理端口，默认 `:9091`：

```text
GET /healthz  只表示当前进程存活
GET /readyz   聚合 required/optional 依赖
GET /metrics  Prometheus RED 与 Go runtime 指标
```

RPC 客户端默认使用严格 Readiness，新绑定依赖为 `required`；非关键依赖可显式放宽为 `optional`：

```yaml
rpc_client:
  user:
    address: dns:///user-service:9090
    timeout: 3s
    readiness: optional
```

下游不可用不会阻止进程启动；required 依赖未就绪时 `/readyz` 返回 503，真实 RPC 调用返回 `Unavailable`，JGO 不自动重试。进程在 HTTP/gRPC/Management 完成监听前保持 not-ready，并在停机开始时先切回 not-ready。Readiness 从收集端强制执行超时，并将 checker panic 隔离为 `NOT_READY`；即使第三方 checker 未正确响应 context，也不会阻塞或带崩 `/readyz`。数据库、Redis、MQ 和私有组件可实现同一接口。

Prometheus 默认开启；OTLP Metrics 默认关闭：

```yaml
telemetry:
  metrics:
    otlp:
      enabled: false
      endpoint: 127.0.0.1:4317
      insecure: true
```

HTTP/gRPC 请求量、错误率、耗时、有限业务码以及 Go runtime 指标均可从 `/metrics` 获取；Prometheus 与 OTLP 可同时使用。uid、trace_id、错误消息等高基数字段不会作为 label。

## Trace 与日志

HTTP/gRPC 使用 W3C `traceparent` 透传，HTTP 响应返回 `X-Trace-ID`。OTLP Trace exporter 默认关闭。结构化日志：

```go
logx.InfoCtx(ctx, "get user completed", "uid", uid, "user", userInfo)
logx.ErrorCtx(ctx, "get user failed", "uid", uid, "err", err)
```

`DebugCtx`、`InfoCtx`、`WarnCtx`、`ErrorCtx` 自动加入 `trace_id` 和 `span_id`。

## 完整验证

```bash
gofmt -w .
go test ./...
go vet ./...
go build ./cmd/jgo
```

多 module 或本地 replace 环境发布前再执行 `GOWORK=off go test ./...`。

更多资料见 [文档索引](docs/README.md)、[快速入门](docs/getting-started.md)、[命令参考](docs/command-reference.md)、[依赖说明](docs/dependencies.md)和[架构知识库](docs/architecture-and-roadmap.md)。

## License

Apache License 2.0。
