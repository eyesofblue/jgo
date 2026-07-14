# JGO 架构设计与实施路线

> 状态：`v0.3.0` 已发布，当前主干进行 `v0.4.0` 生产化 P0 改造
> Go module：`github.com/eyesofblue/jgo`  
> 最低 Go 版本：`1.24`
> License：`Apache-2.0`  
> 文档目标：记录 JGO 的已确认需求、架构边界、脚手架命令、执行步骤和验收标准。

> 说明：后文 P2 章节保留 v0.3.0 当时的命令和实现记录，仅用于历史追溯；当前有效命令与架构以本节、README 和 `docs/command-reference.md` 为准。

## 0. v0.4.0 生产化 P0 决策

### 0.1 服务类型与协议来源解耦

- `web/grpc/mixed/proto` 都创建空业务骨架，不再生成 Echo、Greeter 或默认 Service。
- 本地协议使用 `jgo pb service add`、`jgo pb method add`、`jgo pb generate`。
- 公共协议运行时角色使用 Service 粒度的 `jgo rpc server/client bind|unbind`。
- grpc/mixed 可以只绑定公共协议、只拥有本地协议，或同时使用两者；不存在 `local/external/hybrid` 项目模式参数。
- `jgo generate` 协调 HTTP、本地 protobuf 和 `.jgo/rpc.json` 外部绑定；空项目成功 no-op。
- 旧 `rpc pbservice/pbapi/generate/server add/client add` 入口全部删除，不保留兼容别名。

### 0.2 协议创建与版本

- 空项目第一次 `pb service add` 默认创建 `api/proto/<project>/v1/service.proto` 和 `<project>.v1` package。
- `--package company.user.v1` 可以选择已有 package 或创建新业务域；`company.user.v2` 用于不兼容 API 大版本。
- protobuf `v1/v2` 与 Go module 的 `v0.1.0/v0.2.0` 独立。
- Response 固定包含非 optional 的 `code=1`、`msg=2`，业务字段从 3 开始。
- `pb lint` 执行 Buf lint 与 JGO Response 检查；`pb breaking --against` 要求显式基线。生成项目在 PR CI 中自动比较目标分支。

### 0.3 标准 Go module/workspace

- 删除规划中的 `jgo module` 命令组，直接支持 `go mod`、用户维护的 `replace` 和 `go.work`。
- `--module path@version` 解析发布版本或匹配该版本的显式 replace，不读取 workspace 未发布源码；省略版本只解析活动 workspace 中的本地 module。
- 正常 generate/bind/doctor 尊重 `go.work`。只有 `jgo new` 的临时原子 staging 目录使用 `GOWORK=off`。
- `doctor` 展示活动 workspace 与本地 replace，并校验外部 binding manifest、module、Service 和客户端配置；生产验证使用 `GOWORK=off go test ./...`。

### 0.4 绑定生命周期

- bind 粒度是整个 protobuf Service，业务调用粒度仍是 Method。
- 重复 bind 幂等更新同一 package 的 module 版本，并保留客户端地址、超时、TLS、readiness 配置。
- unbind 用于低频、永久取消服务职责或客户端依赖；不修改公共协议，不删除用户业务实现。
- unbind 后执行 tidy 与编译检查，失败回滚。单个 Method 下线通过协议 deprecated/新版本治理，不增加 method unbind。

### 0.5 生产安全

- gRPC Reflection 框架默认关闭，`configs/local.yaml` 显式开启。
- 服务端 TLS 支持普通 TLS 与 `require_and_verify` mTLS；无效配置直接阻止启动，不降级明文。
- 核心定义基础设施无关的 `Authenticator`、`Authorizer`、`Principal`；具体 JWT、OAuth 或私有权限中心由用户 hook 实现。
- YAML 严格拒绝未知字段。

### 0.6 Management、Readiness 与 Metrics

- 所有服务新增独立 Management HTTP Server，默认 `:9091`。
- `/healthz` 只表示当前进程；`/readyz` 聚合 required/optional 依赖；`/metrics` 暴露 Prometheus。
- RPC 客户端默认 required readiness；可将非关键依赖显式放宽为 optional。required 依赖不可用时进程仍运行但 readyz 返回 503；registry 收集端强制超时，并把第三方 checker panic 隔离为 NOT_READY，不依赖 checker 正确响应 context 才能安全返回。
- readiness 是通用注册表，后续数据库、Redis、MQ、服务发现和私有组件实现相同接口。
- Metrics 提供 HTTP/gRPC RED、Go runtime 与有限业务码；HTTP timeout/panic 和 gRPC 最终映射状态、认证/授权失败均纳入统计；Prometheus 默认本地开启，OTLP Metrics 默认关闭且可同时启用。
- uid、trace_id、路径参数值、错误消息等高基数字段不得成为 label；未知业务码归一为 `unknown`。

### 0.7 错误码治理与生成边界

- Go `errors.Definition/Catalog` 是错误码唯一来源，检查重复 code/name，并携带独立 HTTP status。
- 公共错误码可由独立 Go module 暴露 Catalog；服务通过 `MustMergeCatalogs` 合并公共和本地定义，进程初始化时检测跨仓库 code/name 冲突。
- RPC 业务错误仍返回 Response code/msg 且 gRPC status 为 OK；无法形成 Response 的系统错误使用非 OK status。
- 未注册下游业务码保留公共 code/msg，但 HTTP 映射为 500 并记录 bounded Metrics。
- JGO 只覆盖明确 generated 文件；`internal/errcode`、`internal/securityx` 和业务实现属于用户文件。
- v1.0 前允许 CLI 演进；本次因尚无线上服务彻底删除旧命令。v1.0 前需冻结 CLI、配置格式、manifest 与公开运行时 API，避免上线后大迁移。

### 0.8 生成安全与边界行为

- protobuf 作者命令拒绝 `api/proto` 下的 symlink，避免通过链接修改项目目录外文件。
- `--package` 只在 package 唯一对应一个文件时自动选择；同一 package 分布在多个文件时必须用 `--file` 明确目标。
- 项目名以数字开头、protobuf package 是 Go 关键字等情况会生成合法的 protobuf/Go package；无法安全推导时直接报错。
- Service/RPC 组合产生相同业务方法名或相同 service 文件名时，在写文件前拒绝生成，不以覆盖顺序决定结果。
- `.jgo/rpc.json` 严格拒绝未知字段、重复 binding、客户端 Go 字段碰撞和不适用于当前项目类型的 server binding。
- bind/unbind 先生成再编译，失败恢复 manifest、go.mod/go.sum、配置和所有 JGO 管理文件；用户业务实现始终不被删除。
- Readiness 收集端执行硬超时并隔离 checker panic；返回给 `/readyz` 的错误不会包含 panic 值或堆栈。
- Management、HTTP 和 gRPC 组件均禁止重复启动；注册或启动异常必须能够进入有界关闭流程。

## 1. 项目目标

JGO 是一个可独立使用的 Go 服务框架和脚手架，用于快速创建、扩展、运行和调试 Web 与 gRPC 服务。

核心目标：

1. 不依赖公司私有基础设施，在普通 Go 开发环境中可独立使用。
2. Web Server 统一使用 HTTP + JSON 对外提供 API。
3. RPC Server 统一使用 gRPC + protobuf。
4. 通过命令创建项目、添加接口、生成代码、运行服务和调试接口。
5. 保留接入私有配置中心、注册中心、日志、监控和 Tracing 等基础设施的扩展能力。
6. 所有生成操作应可重复执行，不覆盖业务实现，失败时不留下半成品。

### 1.1 Go 版本策略

- JGO 的代码兼容下限为 Go 1.24。
- `go.mod` 使用 `go 1.24.0`；仓库本地开发可使用更高的已安装版本。
- Go 1.24 保留增强后的 `net/http.ServeMux` 能力，并由内部链接器默认生成 macOS Mach-O `LC_UUID`，避免 Go 1.22 工具在当前 macOS 上被 dyld 拒绝。
- `jgo new` 默认把当前 `go env GOVERSION` 写入生成项目，也允许通过 `--go-version` 显式指定；低于 1.24 时拒绝创建。
- 所有工具安装使用 `GOTOOLCHAIN=local`，不通过 Go 自动工具链下载隐式切换版本。
- 开发和 CI 必须始终覆盖最低版本。
- 生产环境建议使用仍处于 Go 官方维护周期内的较新版本。

### 1.2 开源许可

- JGO 使用 Apache License 2.0。
- 允许商业使用、修改和分发。
- 保留 Apache-2.0 的声明、通知和专利授权要求。

## 2. 已确认的产品决策

### 2.1 项目类型

JGO 支持四种项目类型：

| 类型 | 说明 | 协议文件 |
| --- | --- | --- |
| `web` | 仅提供 HTTP API | OpenAPI |
| `grpc` | 仅提供 gRPC API | protobuf |
| `mixed` | 同时提供 HTTP 和 gRPC，共用业务层 | OpenAPI + protobuf |
| `proto` | 独立、可版本化的公共 protobuf Go module，不包含服务进程 | protobuf |

框架底层支持同时运行多个 Server，但脚手架只根据项目类型生成所需内容。

### 2.2 Web API 风格

Web API 默认保留 RPC 风格的 HTTP 路径，不强制 REST 资源式设计。

示例：

```http
GET /get_user?uid=12345
```

```http
POST /update_user
Content-Type: application/json

{
  "uid": 12345,
  "nickname": "Albert"
}
```

默认规则：

- GET 的普通入参放在 query。
- POST 的入参放在 JSON body。
- 认证、请求链路等信息放在 header，由中间件处理。
- 允许项目手动定义 REST 风格接口，但不作为脚手架强制规范。

### 2.3 Web 契约

Web 使用“命令驱动的 OpenAPI First”：

```text
jgo api add
    ↓
更新 OpenAPI 契约
    ↓
生成 request/response/server interface/client
    ↓
开发者实现业务逻辑
```

常见接口不要求开发者手写 OpenAPI YAML，`jgo api add` 通过命令参数或交互式输入维护契约。复杂模型允许直接编辑 `openapi.yaml`。

### 2.4 Web Server

Web Server 默认基于 Go 标准库 `net/http`：

- 核心框架不绑定 Gin。
- 默认使用标准 `http.Handler` 和 `http.HandlerFunc`。
- 后续可在独立适配包中支持 Gin、Chi 等路由器。

### 2.5 gRPC 契约

gRPC 使用 Proto First：

```text
.proto
  ↓
lint/generate
  ↓
pb.go + grpc.pb.go
  ↓
开发者实现 server interface
```

protobuf 是 gRPC 接口的唯一契约来源。

### 2.6 CLI 框架

- `jgo` 命令行使用 Cobra 组织根命令、子命令、flags、帮助和 Shell 补全。
- Cobra 锁定为 `github.com/spf13/cobra v1.10.2`，该版本兼容 JGO 的 Go 1.24 下限。
- Cobra 只位于 `cmd/jgo` 和 `internal/command` 命令层。
- 项目生成、契约编辑和调试等逻辑放在不依赖 Cobra 的普通 Go package 中。
- JGO 运行时核心不依赖 Cobra。
- 生成的 Web/gRPC/mixed 项目不因 JGO CLI 而引入 Cobra。

### 2.7 OpenAPI 代码生成器

- Web 代码生成复用 `github.com/oapi-codegen/oapi-codegen/v2`。
- 当前锁定 `oapi-codegen v2.4.1`；升级生成器版本必须单独验证生成结果和幂等性。
- 生成项目锁定 `github.com/oapi-codegen/runtime v1.1.1`，该版本声明 Go 1.20。
- JGO 负责 `api add`、OpenAPI 契约维护、生成配置、文件边界、幂等校验和用户体验。
- `oapi-codegen` 负责底层 Go 类型、server interface、`net/http` 适配和 client 代码生成。
- JGO 不重新实现一套完整的 OpenAPI 解析和 Go 代码生成器。
- 生成器版本必须固定，不允许未经验证的浮动升级改变生成结果。
- 选定版本必须兼容 JGO 的 Go 1.24 下限。

复杂 HTTP 请求和返回模型由开发者在 `api/http/model/` 中使用 Go struct 定义。JGO 通过 Go AST 读取公开 struct、嵌套类型、数组、map、指针及 JSON tag，更新 OpenAPI schema；不会编译或运行项目代码。操作、参数来源和 HTTP 路由以 OpenAPI 为准，复杂模型字段以对应 Go struct 为准。

### 2.8 protobuf 工具链

- protobuf 工程统一使用 Buf 组织。
- `buf lint` 负责 `.proto` 契约校验。
- `buf generate` 负责调用固定版本的 protobuf Go 插件生成代码。
- `jgo pb generate` 封装本地协议的 Buf 流程；`jgo generate` 在此基础上协调 HTTP、本地 protobuf 和外部 RPC binding，用户日常不需要直接组合底层生成命令。
- `jgo tools install` 使用当前 Go 环境安装锁定工具，`jgo tools check` 检查路径、版本、构建 Go 版本和可执行性。
- `jgo doctor` 只在项目实际存在本地 `.proto` 时检查 Buf 工具；external-only gRPC/mixed 服务不要求安装 Buf。
- JGO 不静默修改用户机器环境；工具缺失时输出明确安装指引。
- Buf、`protoc-gen-go` 和 `protoc-gen-go-grpc` 的版本必须固定，并兼容 Go 1.24。

阶段 6 锁定的生成工具版本：

- Buf `v1.46.0`：当前已完成真实生成回归的锁定版本。
- `protoc-gen-go v1.36.7`：与框架使用的 protobuf runtime 兼容，并已通过真实生成回归。
- `protoc-gen-go-grpc v1.5.1`：当前已完成真实生成回归的锁定版本。
- `github.com/bufbuild/protocompile v0.14.1`：用于 proto AST 解析、语法校验和安全定位，不依赖正则或特殊注释锚点修改契约。

gRPC 运行时已锁定：

- `google.golang.org/grpc v1.79.1`：当前运行时锁定版本。
- `google.golang.org/protobuf v1.36.11`：当前 protobuf runtime 锁定版本。
- 依赖升级与 Go 最低版本提升分开实施，避免生成差异和平台修复同时引入。

## 3. 现有代码调研结论

### 3.1 `grpchelper`

可以继承的思路：

- 命令创建项目。
- 命令新增 RPC 接口。
- 自动生成 server、handler 和 client 骨架。
- 使用 JSON 快速调试 RPC 方法。
- 生成项目可直接编译。

需要改进的问题：

- 移除 GOPATH 目录依赖。
- 不再依赖特殊注释锚点向 Go 文件插入字符串。
- 不通过反射动态调用生成的 gRPC client。
- CLI 错误使用可诊断的 `error`，不使用 `panic`。
- 生成过程应幂等、可校验、可回滚。
- 模板通过 `go:embed` 随 CLI 发布。

### 3.2 `go-pkg`

值得借鉴的框架思路：

- 组件有序初始化和逆序清理。
- HTTP、RPC、Job、Consumer 可由统一运行器托管。
- 统一处理进程信号和优雅停机。
- 健康检查、公共中间件、错误响应和可观测性作为统一能力。
- 配置实现在未初始化时提供安全默认行为。

JGO 不直接照搬：

- 全局单例和大量全局状态。
- 私有配置中心、注册中心和监控系统。
- 框架核心直接引入数据库、Redis、MQ 等重型依赖。
- 框架库内部直接 `Fatal` 终止进程。

## 4. 仓库架构

预计的 JGO 仓库结构：

```text
jgo/
├── cmd/
│   └── jgo/                 # jgo CLI 入口
├── app/                         # 进程级应用生命周期
├── server/
│   ├── httpx/                   # net/http Server 组件
│   └── grpcx/                   # gRPC Server 组件
├── middleware/
│   ├── recovery/
│   ├── traceid/
│   ├── accesslog/
│   └── timeout/
├── telemetry/                   # OpenTelemetry 生命周期与标准传播器
├── logx/                        # slog 结构化 context 日志
├── config/                      # 本地文件和环境变量配置
├── errors/                      # 统一业务错误
├── response/                    # HTTP 统一响应
├── health/                      # liveness/readiness
├── extension/                   # 外部基础设施扩展接口
├── generator/
│   ├── project/
│   ├── openapi/
│   └── protobuf/
├── internal/
│   ├── command/                 # CLI 子命令实现
│   ├── template/                # go:embed 项目模板
│   └── filesystem/              # 安全写入和回滚
├── examples/
│   ├── web/
│   ├── grpc/
│   └── mixed/
├── docs/
├── go.mod
└── README.md
```

## 5. `app` 应用生命周期

`app` 是进程级的运行容器，不是项目的业务 application 层。生成项目的业务层统一命名为 `service`，避免概念冲突。

计划提供的核心接口：

```go
type Component interface {
	Name() string
	Start(context.Context) error
	Stop(context.Context) error
}
```

`App` 负责：

- 注册 HTTP、gRPC 和后续的 Consumer 等运行组件。
- 启动并监督所有组件。
- 监听 `SIGINT` 和 `SIGTERM`。
- 任一关键组件异常退出时停止整个应用。
- 取消全局 context。
- 按注册逆序优雅关闭组件。
- 限制总关闭时间。
- 保留并返回根因错误。

运行流程：

```text
创建 App
  → 注册组件
  → 校验配置
  → 启动组件
  → 等待信号或组件异常
  → 取消 context
  → 逆序 Stop
  → 返回错误
```

## 6. 生成项目的目录结构

### 6.1 Web

```text
user-api/
├── cmd/server/main.go
├── api/http/openapi.yaml
├── gen/http/                    # 完全由工具管理
├── internal/
│   ├── service/                 # 业务实现
│   └── transport/http/          # HTTP 适配
├── configs/local.yaml
├── Makefile
└── go.mod
```

### 6.2 gRPC

```text
user-rpc/
├── cmd/server/main.go
├── api/proto/                   # 初始为空；拥有本地协议时再创建
├── gen/pb/                      # 本地协议生成后出现
├── internal/
│   ├── service/
│   ├── rpcclient/               # 公共协议客户端 binding
│   └── transport/grpc/          # 本地协议与公共协议服务端 adapter
├── buf.yaml
├── buf.gen.yaml
├── configs/local.yaml
├── Makefile
└── go.mod
```

### 6.3 Mixed

```text
user-service/
├── cmd/server/main.go
├── api/
│   ├── http/openapi.yaml
│   └── proto/                   # 初始为空；本地协议按需创建
├── gen/
│   ├── http/
│   └── pb/                      # 本地协议生成后出现
├── internal/
│   ├── service/                 # HTTP/gRPC 共用
│   └── transport/
│       ├── http/
│       └── grpc/
├── configs/local.yaml
├── Makefile
└── go.mod
```

生成边界：

- `gen/` 内容可删除并完整重建。
- `internal/service/` 由开发者维护。
- 业务骨架只在文件不存在时创建。
- 工具不重写已存在的业务文件。
- HTTP 和 gRPC transport 只负责协议转换，不放核心业务逻辑。

## 7. CLI 命令规划

### 7.1 创建项目

```bash
jgo new user-api --module example.com/user-api --type web
jgo new user-rpc --module example.com/user-rpc --type grpc
jgo new user-service --module example.com/user-service --type mixed
```

### 7.2 Web API

```bash
jgo api add GetUser --method GET --path /get_user
jgo api add UpdateUser --method POST --path /update_user \
  --request-params UpdateUserRequest \
  --response-data UserInfo
jgo api generate
```

`api add` 的交互输入需要支持参数名称、类型、是否必填和来源。对于 GET，默认来源为 query；对于 POST，默认来源为 JSON body。

简单参数和复杂模型示例：

```bash
jgo api add GetUser \
  --method GET \
  --path /get_user \
  --request uid:int64:required \
  --response-data UserInfo

jgo api add ListUsers \
  --method GET \
  --path /list_users \
  --response-data UserInfo \
  --response-list
```

- `--request-params` 指向 `api/http/model/` 中用于复杂 POST JSON body 的 Go struct。
- `--response-data` 指向 `data` 字段中的 Go struct 或基础类型。
- `--response-list` 将 `data` 声明为 `--response-data` 的数组。
- 简单 GET query 或简单 POST body 可以重复使用 `--request name:type:required`。

### 7.3 gRPC API

```bash
# 项目自有协议
jgo pb service add UserService
jgo pb method add GetUser --service UserService
jgo pb generate

# 公共协议运行时角色
jgo rpc server bind UserService --module example.com/company-api@v1.0.0
jgo rpc client bind UserService --module example.com/company-api@v1.0.0 --name user
```

协议作者命令属于 `jgo pb`；`jgo rpc` 只管理服务端/客户端对公共 protobuf Service 的运行时绑定。两者可以在 grpc/mixed 项目中同时使用。

### 7.4 工程命令

```bash
jgo generate
jgo list
jgo doctor
jgo run
jgo build
```

### 7.5 调试命令

HTTP：

```bash
jgo call http GetUser \
  --addr http://127.0.0.1:8080 \
  --data '{"uid":12345}'
```

JGO 读取 OpenAPI，转换为：

```http
GET /get_user?uid=12345
```

gRPC：

```bash
jgo call grpc UserService.GetUser \
  --addr 127.0.0.1:9090 \
  --data '{"uid":12345}'
```

gRPC 调试优先使用 Reflection，Reflection 不可用时读取本地 protobuf descriptor。

HTTP 与 gRPC 调用统一支持：

- `--data` / `-d`：JSON object 输入。
- `--header` / `-H`：可重复的 `Name: Value` HTTP header 或 gRPC metadata。
- `--timeout`：整次解析与网络调用的超时时间，默认 10 秒。
- `--root`：契约所在的 JGO 项目目录，默认当前目录。

首版动态 gRPC 调用支持 unary RPC，`jgo list` 会标记 streaming RPC，调用时返回明确的暂不支持错误。调试连接默认使用本地开发常用的明文 HTTP/2；TLS 参数留在后续开发者体验阶段补充。

## 8. 默认运行能力

### 8.1 HTTP

- 读取、写入、读取 Header 和空闲超时。
- 请求体大小限制。
- panic recovery。
- trace ID。
- access log。
- 统一 JSON 错误响应。
- `/healthz` 存活检查。
- `/readyz` 就绪检查。

默认错误响应：

```json
{
  "code": 20001,
  "msg": "user not found",
  "data": null
}
```

成功响应固定为：

```json
{
  "code": 0,
  "msg": "",
  "data": {}
}
```

- `code` 是整数业务码，成功固定为 `0`，失败为非零。
- HTTP status 和业务错误码分开管理，业务码不复用 HTTP status。
- `data` 可以是对象、数组或 `null`；错误响应固定为 `null`。
- trace ID 通过 HTTP header 传递，不增加到标准响应 body。

### 8.2 gRPC

- 优雅停机和强制停机兜底。
- recovery interceptor。
- trace ID/metadata 传递。
- 业务错误写入标准 Response `code/msg`；无法形成业务 Response 的系统错误转换为 gRPC status。
- 开发环境可开启 Reflection。

## 9. 扩展边界

JGO 核心只声明小型、通用接口，不引入具体私有实现。

示例：

```go
type ConfigSource interface {
	Load(context.Context) (map[string]any, error)
	Watch(context.Context, func(map[string]any)) error
}

type Registrar interface {
	Register(context.Context, Service) error
	Deregister(context.Context, Service) error
}
```

后续可在独立模块中实现：

```text
jgo-apollo
jgo-nacos
jgo-etcd
jgo-opentelemetry
jgo-company
```

依赖方向必须是扩展模块依赖 JGO，JGO 不反向依赖扩展模块。

## 10. 工具链现状和计划

当前本机已检查状态：

- Go：`go1.25.12 darwin/arm64`
- `protoc`：未安装
- `protoc-gen-go`：未安装
- `protoc-gen-go-grpc`：未安装

gRPC 生成计划使用 Buf 组织 lint 和 generate 流程。`jgo doctor` 检查但不静默修改开发者环境。缺失工具时输出带版本的明确安装指引。

## 11. 具体执行步骤

### 阶段 0：仓库基线

任务：

1. 在 JGO 仓库根目录初始化 Git 仓库。
2. 创建 `go.mod`，module 为 `github.com/eyesofblue/jgo`。
3. 在 `go.mod` 中声明已确认的最低版本 `go 1.22.0`。
4. 创建 `README.md`、Apache-2.0 `LICENSE`、`.gitignore`、`Makefile`。
5. 配置 `go test`、`go vet` 和基础 lint。
6. 建立 `docs/` 与架构决策记录。

产出：可测试、可持续演进的空仓库。

验收：

```bash
go test ./...
go vet ./...
```

全部通过。

### 阶段 1：应用生命周期核心

任务：

1. 实现 `app.Component`。
2. 实现 `app.App`、`Add` 和 `Run`。
3. 实现 context 取消和系统信号监听。
4. 实现逆序 Stop 和关闭超时。
5. 实现组件名称重复、空组件、重复 Run 等校验。
6. 实现启动失败、运行异常、关闭超时的单元测试。

产出：可独立测试的进程级生命周期容器。

验收：

- 任一组件返回异常后，其他组件会被关闭。
- 关闭顺序与注册顺序相反。
- 关闭超时不会让进程无限挂起。
- 竞态检查通过：`go test -race ./app/...`。

### 阶段 2：HTTP 运行时

任务：

1. 实现 `server/httpx.Server`，使其满足 `app.Component`。
2. 提供安全的默认 timeout。
3. 实现 recovery、trace ID、access log 和 timeout 中间件。
4. 实现统一错误模型和 JSON response。
5. 实现 `/healthz` 和 `/readyz`。
6. 使用 `httptest` 完成端到端测试。

产出：不依赖第三方路由框架的 HTTP Server 能力。

验收：

- 正常请求、参数错误、业务错误和 panic 均返回预期格式。
- Shutdown 会等待正在处理的请求。
- 库代码不主动调用 `os.Exit` 或 `log.Fatal`。

### 阶段 3：gRPC 运行时

任务：

1. 实现 `server/grpcx.Server`，使其满足 `app.Component`。
2. 实现 service 注册函数。
3. 实现 unary recovery interceptor。
4. 实现 trace ID/metadata 传递。
5. 实现 JGO 业务 error 到 Response 的转换，以及系统 error 到 gRPC status 的转换。
6. 支持可配置的 Reflection。
7. 实现 `GracefulStop` 超时后 `Stop` 的兜底策略。

产出：可由 `App` 托管的 gRPC Server。

验收：

- 基于 `bufconn` 的端到端测试通过。
- mixed 测试可以同时启动 HTTP 和 gRPC。
- 任一 Server 异常后另一 Server 可优雅关闭。

### 阶段 4：`jgo new` 项目脚手架

任务：

1. 实现 CLI 根命令和 `new` 子命令。
2. 实现 web、grpc、mixed 三套模板。
3. 使用 `go:embed` 嵌入模板。
4. 校验项目名、module path、项目类型和目标目录。
5. 先在临时目录生成和验证，成功后再移入目标目录。
6. 默认不覆盖已存在的非空目录。
7. 为生成目录编写 snapshot/golden tests。

产出：三类可编译的初始项目。

验收：

```bash
jgo new demo-web --module example.com/demo-web --type web
jgo new demo-grpc --module example.com/demo-grpc --type grpc
jgo new demo-mixed --module example.com/demo-mixed --type mixed
```

三个项目内部的以下命令均通过：

```bash
go test ./...
go build ./...
```

### 阶段 5：Web 接口生成

任务：

1. 实现 `jgo api add`。
2. 实现 OpenAPI 结构化读写和校验。
3. 生成 request/response 类型、server interface、路由适配和 client。
4. 为 GET query 和 POST JSON body 提供简单默认规则。
5. 实现 `jgo api generate`。
6. 保证生成结果可重复、格式化并可编译。

产出：基于 OpenAPI 契约的 Web 接口生成链路。

验收：

- 新增 GetUser 后可通过 query 读取 uid。
- 新增 UpdateUser 后可解析 JSON body。
- 重复 generate 不产生 diff。
- 已编写的 service 实现不被覆盖。

### 阶段 6：gRPC 接口生成

任务：

1. 实现 `jgo rpc pbservice add` 与 `jgo rpc pbapi add`。
2. 实现 proto 文件的结构化校验与更新。
3. 接入 Buf lint/generate。
4. 生成 transport 骨架，不覆盖 service 实现。
5. 实现 `jgo rpc generate`。

产出：基于 protobuf 契约的 gRPC 接口生成链路。

验收：

- `buf lint` 通过。
- `buf generate` 结果可编译。
- 重复 generate 不产生 diff。
- 新增 RPC 不破坏原有 RPC 业务实现。

### 阶段 7：统一调试 CLI

任务：

1. 实现 `jgo call http`。
2. 从 OpenAPI 查找 operationId、方法、路径和参数来源。
3. 将 JSON 输入映射为 query/header/body。
4. 实现 `jgo call grpc`。
5. 支持 gRPC Reflection 和本地 descriptor 两种模式。
6. 支持 header/metadata、timeout 和格式化输出。
7. 实现 `jgo list`，列出 HTTP 和 gRPC 接口。

产出：不为每个接口生成重复调试桩的统一调试工具。

验收：

- HTTP GetUser 可从 JSON 自动构造 query 并返回格式化 JSON。
- HTTP UpdateUser 可自动构造 JSON body。
- gRPC GetUser 可通过 Reflection 动态调用。
- 错误方法名会输出可用方法列表，不 panic。

### 阶段 8：开发者体验和发布

任务：

1. 实现 `jgo doctor`、`jgo generate`、`jgo run` 和 `jgo build`。
2. 实现 Bash/Zsh completion。
3. 完善 README、快速入门、Web/gRPC/mixed 示例。
4. 配置 CI，覆盖 test、race、vet、lint 和生成代码一致性检查。
5. 在 macOS 和 Linux 上验证。
6. 制定语义化版本和发布流程。

产出：可安装、可学习、可稳定生成项目的首个版本。

## 12. 实施原则

1. **先运行时，后脚手架**：先证明框架 API 稳定，再把 API 固化到模板中。
2. **契约是唯一事实来源**：Web 使用 OpenAPI，gRPC 使用 protobuf。
3. **生成与手写代码分离**：只有 `gen/` 可无条件重建。
4. **幂等**：连续两次执行 generate 不应产生新 diff。
5. **交易式生成**：先在临时目录产生完整结果，验证成功后再替换目标。
6. **库不退出进程**：库返回 error，只有 `cmd/jgo` 和生成项目的 `main` 决定退出码。
7. **默认安全**：HTTP timeout、body limit、recovery 和优雅停机不应由每个项目重复配置。
8. **轻量核心**：数据库、MQ、私有注册中心等不进入核心模块。
9. **先接口后插件**：只在出现第一个真实外部实现时固化扩展接口，避免过度抽象。

## 13. 第一版不做的内容

- ORM 和数据库框架。
- Redis 封装。
- MQ 框架。
- 私有配置中心和注册中心实现。
- 公司专用监控和发布系统。
- 具体业务鉴权逻辑。
- Kubernetes 和云平台强绑定。
- 自动生成业务实现。

## 14. 里程碑

| 里程碑 | 包含阶段 | 完成标志 |
| --- | --- | --- |
| M1 运行时原型 | 0-3 | HTTP/gRPC/mixed 可手工组装和优雅停机 |
| M2 项目生成 | 4 | 三类项目生成后可编译和测试 |
| M3 契约生成 | 5-6 | OpenAPI/protobuf 可生成可运行骨架 |
| M4 开发闭环 | 7 | 新增接口后可立即通过 CLI 调试 |
| M5 首版发布 | 8 | 文档、CI、跨平台验证和发布流程完成 |

## 15. 待确认事项

开始阶段 0 前仍需要逐项确认：

1. Buf 和 protobuf Go 插件的具体锁定版本。

上述事项每次只确认一个，确认后写入本文档。

## 16. 实施记录

### 2026-07-13：阶段 0 完成

已完成：

- 初始化 JGO Git 仓库。
- 创建 `go.mod`，module 为 `github.com/eyesofblue/jgo`，Go 下限为 1.22。
- 创建 Apache License 2.0 `LICENSE`。
- 创建 `README.md`、`.gitignore` 和 `Makefile`。
- Makefile 提供 `fmt`、`test`、`test-race`、`vet` 和 `check` 目标。

验证结果：

- `go test ./...` 通过。
- `go test -race ./...` 通过。
- `go vet ./...` 通过。

### 2026-07-13：阶段 1 完成

已完成：

- 实现 `app.Component`。
- 实现 `app.New`、`App.Add` 和 `App.Run`。
- 实现多组件并发运行。
- 实现父 context、`SIGINT` 和 `SIGTERM` 触发的统一关闭。
- 实现逆序 Stop 和总关闭超时。
- 实现组件启动/关闭错误的标识与汇总。
- 实现组件 Start/Stop panic 恢复，将 panic 转换为带堆栈的 error。
- 实现 nil 组件、空名称、重名、非法选项、运行期修改和重复 Run 校验。
- 完成单元测试和竞态测试。
- `go test -count=20 ./app/...` 稳定性检查通过。

### 2026-07-13：阶段 2 完成

已完成：

- 实现 `server/httpx.Server`，并使其满足 `app.Component`。
- 实现默认 HTTP 超时：5 秒 ReadHeader、15 秒 Read、30 秒 Write、60 秒 Idle、30 秒 handler timeout。
- 实现 `middleware.Chain` 和可重用的 response writer 状态记录器。
- 实现 trace ID 传递与生成，并拒绝不安全的外部 trace ID。
- 实现基于 `log/slog` 的结构化 access log。
- 实现 handler timeout，使用缓冲响应防止超时 handler 继续写客户端。
- 实现 panic recovery；响应未提交时返回统一 JSON 错误，已提交时不拼接二次响应。
- 实现 `errors.Error` 公开错误模型，对客户端隐藏未知错误和内部 cause。
- 实现 `response.Envelope`、成功响应和统一错误响应。
- 实现 `health.Probe`、`GET /healthz` 和 `GET /readyz`。
- readiness 默认为 false，支持进程就绪门和多个依赖检查。
- 默认中间件顺序为 trace ID → access log → timeout → recovery → application handler。
- 完成 `httpx.Server` 与 `app.App` 的真实 TCP 端到端测试。

验证结果：

- `go test ./...` 通过。
- `go test -race ./...` 通过。
- `go vet ./...` 通过。
- `go test -count=20 ./errors ./health ./middleware/... ./response ./server/httpx` 通过。

### 2026-07-13：阶段 3 完成

已完成：

- 锁定 Go 1.22 兼容的 `google.golang.org/grpc v1.71.3` 和 `google.golang.org/protobuf v1.36.7`。
- 实现 `server/grpcx.Server`，并使其满足 `app.Component`。
- 实现生成服务的 `RegisterFunc` 注册机制。
- 实现可配置的 gRPC Reflection。
- 实现 unary 和 stream trace ID interceptor，支持 metadata 传入、context 注入和 response header 回传。
- 实现 unary 和 stream panic recovery interceptor，使用 `slog` 记录堆栈。
- 实现 unary 和 stream 错误转换 interceptor。
- 实现 HTTP status 到 gRPC code 的稳定映射，未知错误统一转换为 `codes.Internal`。
- 阶段 3 初版曾通过 gRPC `ErrorInfo` detail 传递业务码；v0.2.0 P1 已统一改为 Response `code/msg`，status details 不再承载业务码。
- 保留已有 gRPC status error，并正确映射 `context.Canceled` 和 `context.DeadlineExceeded`。
- 实现活动 unary/stream RPC 跟踪和 draining 状态，停机后拒绝新 RPC。
- 优雅停机先等待活动 RPC 归零；超时后直接强制 Stop，避免 gRPC-Go v1.71.3 中并发 `GracefulStop`/`Stop` 的内部锁竞争。
- 完成 bufconn 端到端测试，覆盖 trace ID、业务错误、panic recovery 和 Reflection。
- 完成 HTTP + gRPC mixed 应用生命周期测试。

验证结果：

- `go test ./...` 通过。
- `go test -race ./...` 通过。
- `go vet ./...` 通过。
- `go test -count=20 -timeout=60s ./server/grpcx` 通过。

### 2026-07-13：阶段 4 完成

已完成：

- 锁定 `github.com/spf13/cobra v1.10.2`，实现可测试的 `jgo` 根命令和 `jgo new` 子命令。
- `jgo new` 支持 `web`、`grpc`、`mixed` 三种项目类型，以及 `--module`、`--output`、`--jgo-version` 和 `--jgo-replace` 参数。
- 使用 `go:embed` 将项目模板编译进 CLI，不依赖运行机器上的模板目录。
- 三类项目统一生成应用生命周期入口、业务层、配置示例、Makefile、README 和独立 `go.mod`。
- Web 项目生成 RPC 风格 HTTP 示例、OpenAPI 契约和 `net/http` transport。
- gRPC 项目生成示例 proto、Buf 配置和标准 gRPC health service；mixed 项目同时包含两套 transport。
- 生成器校验项目名、module path、项目类型、JGO 版本、本地 replace 路径和目标目录。
- 生成过程先在目标同级临时目录完成渲染、Go 格式化和完整性校验，再原子移动到目标路径。
- 默认拒绝文件目标、非空目录和符号链接，不修改已有内容；允许使用已存在的空目录。
- 完成三类目录 golden tests、CLI 测试、安全边界测试和生成项目编译测试。

本地框架开发时可直接验证：

```bash
go run ./cmd/jgo new demo-web \
  --module example.com/demo-web \
  --type web \
  --jgo-replace "$PWD"
```

正式发布版本后，用户不需要 `--jgo-replace`，由 `--jgo-version` 指定 JGO 版本。

验证结果：

- 三类生成项目内的 `go mod tidy`、`go test ./...` 和 `go build ./...` 均通过。
- JGO 全仓 `go test ./...` 通过。
- JGO 全仓 `go test -race ./...` 通过。
- JGO 全仓 `go vet ./...` 通过。
- `go build ./cmd/jgo` 通过。

### 2026-07-13：阶段 5 完成

已完成：

- 锁定 Go 1.22.0 兼容的 `github.com/oapi-codegen/oapi-codegen/v2 v2.4.1`。
- 锁定生成项目使用的 `github.com/oapi-codegen/runtime v1.1.1`，避免解析到要求 Go 1.24 的新版本。
- 实现 `jgo api add` 和 `jgo api generate`。
- 复杂请求参数使用 `--request-params`，返回 `data` 使用 `--response-data`，数组返回使用 `--response-list`。
- 简单 GET query 和 POST JSON 字段支持重复的 `--request name:type:required`。
- 复杂 POST JSON 请求与复杂对象响应直接引用 `api/http/model/` 下的 Go struct。
- 使用 Go AST 读取公开 struct、嵌套或匿名 struct、指针、数组、map、`time.Time` 和 JSON tag，不通过反射运行项目代码。
- 将 Go struct 同步到 OpenAPI component schema，并通过 `x-go-type` 复用原始 Go 类型，避免生成重复模型。
- OpenAPI 更新使用结构化解析、完整校验和同目录临时文件原子替换。
- `oapi-codegen` 生成 `gen/http/api.gen.go`，包含模型别名、标准库 ServerInterface、路由绑定和 client。
- JGO 生成 `internal/transport/http/routes.gen.go`，负责协议解析、service 调用和统一响应。
- 每个 operation 首次创建独立 service 实现文件，后续 `api generate` 不覆盖开发者修改。
- HTTP body 固定为 `code`、`msg`、`data` 三个字段；成功业务码固定为 `0`，错误时 `data` 固定为 `null`。
- HTTP status 与整数业务错误码独立管理；trace ID 继续通过 HTTP header 传递。
- 更新 web/mixed 项目模板，增加 `api/http/model/`、生成路由占位文件和固定的 OpenAPI runtime 依赖。

使用示例：

```bash
jgo api add GetUser \
  --method GET \
  --path /get_user \
  --request uid:int64:required \
  --response-data UserInfo

jgo api add UpdateUser \
  --method POST \
  --path /update_user \
  --request-params UpdateUserRequest \
  --response-data UserInfo

jgo api generate
```

验证结果：

- GET query 必填参数读取和缺失参数错误响应通过端到端测试。
- 复杂嵌套 POST JSON struct 解析通过端到端测试。
- 单对象和对象数组的 `data` 返回通过端到端测试。
- 重复 `api generate` 不产生内容变化。
- 已修改的 service 实现文件不被覆盖。
- 生成项目内的 `go mod tidy`、`go test ./...` 和 `go build ./...` 均通过。
- JGO 全仓 `go test ./...` 通过。
- JGO 全仓 `go test -race ./...` 通过。
- JGO 全仓 `go vet ./...` 通过。
- CLI 临时目录构建通过。

### 2026-07-13：阶段 6 完成

已完成：

- 实现 `jgo rpc pbapi add <rpc-name> --service <service-name>`，并提供可选 `--file` 处理多文件同名 service；该命令在 P2 阶段 4 取代早期的 `rpc add`。
- `rpc pbapi add` 创建空 request message，并为 response 自动包含标准 `code/msg` 字段。
- 使用 protocompile AST 解析 proto，精确定位 service 结束位置；不使用正则或特殊注释锚点。
- 在写入前检查 proto 语法、service 唯一性、RPC 重名、message 冲突、文件边界和符号链接，并在同目录原子替换契约。
- 实现 `jgo rpc generate`，依次校验锁定工具版本、执行 `buf lint` 和 `buf generate`。
- 从生成的 `*_grpc.pb.go` 结构化读取 service 和 unary RPC，生成 `internal/transport/grpc/register.gen.go` 注册与转发适配器。
- 每个 RPC 首次生成独立的 `internal/service/<rpc>.go` 业务骨架；检测到已有 `Service` 方法时不再创建或覆盖。
- gRPC 与 mixed 项目模板增加可编译的 transport 占位文件，生成后由托管文件替换。
- 生成项目 Makefile 增加 `tools` 和 `rpc-generate`，仅在用户显式执行时安装锁定版工具，不静默修改环境。

使用示例：

```bash
make tools
jgo rpc pbapi add GetUser --service GreeterService

# 编辑 api/proto/<package>/v1/service.proto 中的字段后：
jgo rpc generate
```

验证结果：

- 锁定版 Buf `1.46.0`、`protoc-gen-go 1.36.7`、`protoc-gen-go-grpc 1.5.1` 完成真实工具链验证。
- grpc 和 mixed 示例项目的 `buf lint`、`buf generate`、`go test ./...`、`go build ./...` 均通过。
- 重复 `rpc generate` 后 proto、pb、grpc pb、transport 和 service 文件哈希均保持不变。
- RPC/service/message 冲突、无效标识符、proto 歧义及越界文件均有单元测试覆盖。
- JGO 全仓 `go test ./...`、`go test -race ./...`、`go vet ./...` 均通过。
- CLI 临时目录构建通过。

### 2026-07-13：阶段 7 完成

已完成：

- 实现 `jgo call http <operation-id>`，从 `api/http/openapi.yaml` 定位 operationId、HTTP method 和 path。
- 将统一 JSON object 输入按 OpenAPI parameter/requestBody 自动映射到 query、header、path 和复杂 JSON body。
- OpenAPI request body 在发送前执行 schema 校验，必填参数缺失时直接返回可读错误。
- HTTP 响应 JSON 自动缩进；HTTP status 与响应体中的业务 `code` 保持独立，不对业务码做二次解释。
- 实现 `jgo call grpc <service.method>`，使用 `dynamicpb` 和 `protojson` 动态构造请求与响应，不生成单接口调试桩。
- gRPC 优先通过标准 Reflection v1 获取含依赖的 descriptor；Reflection 不可用时使用 protocompile 编译 `api/proto/` 本地契约。
- HTTP header 与 gRPC metadata 统一使用可重复的 `--header/-H 'Name: Value'`，两种协议统一支持 `--timeout`。
- 实现 `jgo list`，以稳定顺序列出本地 OpenAPI operation 和 protobuf RPC，并标识 unary/stream。
- operation、service 或 method 不存在时返回可用方法列表，不 panic。
- 更新根 README 和生成项目 README，加入 list、HTTP call 和 gRPC call 示例。
- 修复简单 HTTP 请求业务 struct 的 JSON tag，从错误的 `json:"\\"uid\\""` 修正为 `json:"uid"`，并增加回归检查。

真实 mixed 项目验证：

```bash
jgo list
jgo call http GetUser --addr http://127.0.0.1:8080 --data '{"uid":12345}'
jgo call http UpdateUser --addr http://127.0.0.1:8080 --data '{"uid":67890}'
jgo call grpc GreeterService.Echo --addr 127.0.0.1:9090 --data '{"message":"hello jgo"}'
```

验证结果：

- `jgo list` 同时列出 GetUser、UpdateUser 和 GreeterService.Echo。
- HTTP GetUser 自动生成 `GET /get_user?uid=12345`，统一响应的 `data` 为 `12345`。
- HTTP UpdateUser 自动发送 JSON body，统一响应的 `data` 为 `67890`；复杂嵌套 body 另有端到端测试覆盖。
- gRPC GreeterService.Echo 通过 Reflection 动态调用并返回 `hello jgo`。
- 关闭 Reflection 的 gRPC health 服务通过本地 proto descriptor 回退调用成功。
- metadata、错误方法列表、HTTP 非 2xx 与业务码分离均有端到端测试覆盖。
- `go test -count=10 -timeout=60s ./internal/call` 通过。
- JGO 全仓 `go test ./...`、`go test -race ./...`、`go vet ./...` 均通过。
- CLI 临时目录构建通过。

### 2026-07-13：阶段 8 完成

已完成：

- 实现 `jgo doctor`，汇总检查项目结构、Go 1.22.0 下限、JGO module、OpenAPI、protobuf 和锁定版 Buf 工具链；检查失败时继续展示其余结果，不静默安装工具。
- 实现统一 `jgo generate`，自动识别 web/grpc/mixed 项目；mixed 项目先检查外部工具再更新 HTTP 和 gRPC 生成代码。
- 实现 `jgo run`，在项目根目录运行 `./cmd/server` 并透传 server 参数、标准输入输出和 context。
- 实现 `jgo build`，默认输出 `bin/<项目目录名>`，支持 `--output/-o` 自定义路径，并使用 `-trimpath`。
- CLI 支持 `jgo --version`；发布归档通过 ldflags 写入 tag 版本，本地源码构建显示 `dev`。
- 使用 Cobra 原生 completion 提供 Bash/Zsh 补全，并在快速入门中给出安装方式。
- 新增快速入门、web/grpc/mixed 三类示例、CHANGELOG 和 Semantic Versioning 发布清单。
- 确认首个版本号为 `v0.1.0`；脚手架默认依赖更新为 `github.com/eyesofblue/jgo v0.1.0`。
- 配置 GitHub Actions CI：macOS/Linux、Go 1.22、format、vet、test、race、CLI build 和真实生成一致性验证。
- 配置 tag 发布工作流：为 Linux/macOS 的 amd64/arm64 构建归档、生成 SHA-256 校验并创建 GitHub Release。
- 新增 `scripts/verify-generation.sh`，真实创建 web、grpc、mixed 项目并验证 HTTP/gRPC 连续生成内容不变、生成项目可测试和构建。
- 真实生成检查发现并修复 GET-only HTTP 项目无条件导入 `encoding/json` 的问题，并增加回归测试。

验证结果：

- 真实 mixed 项目的 `jgo doctor` 全部检查通过。
- 真实 mixed 项目的统一 `jgo generate`、`go test ./...` 和 `jgo build` 通过，已有业务实现未被覆盖。
- `scripts/verify-generation.sh` 在 macOS arm64 本机通过。
- macOS arm64 CLI 本机构建通过；Linux amd64 静态 ELF 交叉构建通过。
- GitHub Actions 已配置在 macOS/Linux runner 上执行完整质量门禁；实际远端结果需在代码推送后确认。
- `make fmt-check`、全仓 `go test ./...`、`go test -race ./...`、`go vet ./...` 均通过。

发布边界：

- 当前未创建 Git tag、未推送代码、未创建 GitHub Release。
- 维护者确认并创建 `v0.1.0` tag 后，release workflow 才会执行实际发布。

### 2026-07-13：完整回归验收补充

发布前完整验收真实创建并编译了 `web`、`grpc` 和 `mixed` 三类项目。验收覆盖复杂 Go struct JSON 请求、大对象返回、对象数组返回、HTTP 与 gRPC 同名 `GetUser`、`doctor`、统一生成、重复生成幂等性、生成项目测试和构建。

该验收发现 mixed 项目原先会让 HTTP `GetUser` 和 gRPC `GetUser` 争用同一个 Go 业务方法。gRPC 业务方法现统一使用 `<Service><RPC>` 命名，例如 `GreeterServiceGetUser`；对外 protobuf 服务名和 RPC 名保持不变。这样也允许不同 gRPC service 复用常见 RPC 名。

### 2026-07-14：v0.2.0 P0 兼容性与脚手架闭环

已完成：

- 最低 Go 版本提升为 1.24.0，CI 的 Linux/macOS 最低版本验证同步到 Go 1.24，不再依赖 macOS 外部链接 workaround。
- `jgo new` 默认读取当前 `go env GOVERSION`，支持 `--go-version`，并使用 `GOTOOLCHAIN=local` 禁止隐式切换工具链。
- `jgo new` 在临时目录中执行 `go mod tidy` 并生成 `go.sum`，失败时不提交半成品；离线环境可以显式使用 `--skip-tidy`。
- 模板增加 build-tag 依赖锚点，保证首次生成 HTTP/protobuf 代码后无需再次手工补充 module 依赖。
- 实现 `jgo tools install` 和 `jgo tools check`；检查结果包含实际路径、锁定版本和构建 Go 版本，并识别 macOS `LC_UUID` 启动失败。
- 初始 protobuf service 根据项目名生成，例如 `demo-grpc` 对应 `DemoGrpcService`；`rpc generate` 继续扫描契约中的全部 service，不限制数量。
- 生成项目新增 `internal/config`，按命令参数、环境变量、YAML、默认值的顺序加载服务名、HTTP/gRPC 地址和优雅关闭超时。
- web、grpc、mixed 的 `main.go` 均使用配置，不再硬编码监听地址和关闭超时。

验收结果：

- 全仓普通测试、race 测试和 `go vet` 通过。
- `jgo tools install/check` 在 goenv 环境中解析到真实工具路径，三个工具均由 Go 1.25.12 构建并可正常运行。
- 真实创建 web、grpc、mixed 三类项目，创建后立即存在 `go.sum`。
- 三类项目的接口新增、重复生成幂等、doctor、单元测试和构建全部通过。

### 2026-07-14：v0.2.0 P1 协议一致性与使用体验

已完成：

- 初始 Echo 和 `jgo rpc pbapi add` 创建的每个 response 固定声明非 optional 的 `int32 code = 1`、`string msg = 2`，用户业务字段从编号 `3` 开始。
- `code = 0` 表示业务成功；gRPC status 继续表达传输或系统错误，不与业务码混用。
- `jgo call grpc` 使用 `protojson.EmitDefaultValues`，普通无 presence 字段的零值会显示，未设置的 optional/message 字段仍然省略。
- `jgo rpc generate`、统一 `jgo generate` 和 `jgo doctor` 强制校验所有 response；非标准契约直接失败，不保留存量兼容分支。
- `rpc pbapi add` 的结果提示明确指出保留字段和下一步命令。
- 生成项目 README 只维护稳定工作流，不复制容易过期的接口清单；OpenAPI/proto 是协议真源，`jgo list` 展示当前接口。
- 中英文 README、命令参考、示例和变更记录同步上述约定。
- 生成 unary transport 将显式 `jgo/errors.Error` 转换为 gRPC `OK` 的 Response `code/msg`；panic、未知错误、取消和超时继续使用非 `OK` status。
- Response 标准检查改用完整 protobuf descriptor，覆盖跨文件和 import 场景。
- generate 输出本次新建的业务文件以及需要实现的 `Service.<Method>`。

### 2026-07-14：v0.2.0 发布

- 发布提交完成全仓 test、race、vet、格式化、CLI 构建及 web/grpc/mixed 真实生成验收。
- 创建 annotated tag `v0.2.0`，由 GitHub Actions 生成 Linux/macOS 的 amd64/arm64 归档和 SHA-256 校验文件。
- README、快速入门、依赖说明、命令参考、实施知识库和 CHANGELOG 已同步到 `v0.2.0`。

### 2026-07-14：P2 阶段 1 OpenTelemetry 与 context 日志

设计决定：

- 彻底移除公开的 `middleware/requestid`、`X-Request-ID` 和 `request_id` 日志字段，不保留兼容别名。
- HTTP 与 gRPC 统一使用 OpenTelemetry 和 W3C `traceparent`，HTTP 响应额外返回 `X-Trace-ID` 便于人工排查。
- 生成项目默认创建并透传 Trace Context；OTLP exporter 默认关闭，因此运行时不依赖 Collector 或任何私有基础设施。
- exporter 开启时默认采样比例为 `0.1`；endpoint、TLS/insecure 和采样比例由 YAML 配置。
- tracer provider 是 `app.Component`。构造时完成全局 provider/propagator 安装，确保并发启动的 HTTP/gRPC Server 可以立即使用；注册在 Server 之前，反向停机时最后 flush。
- OpenTelemetry 固定为兼容 Go 1.24 的核心 `v1.41.0`、HTTP/gRPC instrumentation `v0.66.0`。更新组合已经要求 Go 1.25，不允许触发隐式工具链升级。
- 新增 `logx.DebugCtx`、`InfoCtx`、`WarnCtx`、`ErrorCtx` 及注入式 `Logger`，底层继续使用 `slog`，自动附加 `trace_id` 和 `span_id`。
- 日志只接受结构化 key/value 参数，不增加 printf 风格接口。
- 生成的 unary gRPC transport 在业务错误仍返回 gRPC `OK` 的同时，为当前 span 增加 `jgo.business_code` 和 `jgo.business_message`，避免标准 instrumentation 把业务失败完全视为无差别成功。

验证范围：

- telemetry 配置校验、无 exporter 时的有效非 recording span、幂等 Shutdown。
- HTTP `traceparent` 提取、`X-Trace-ID` 响应和 access/recovery 日志关联。
- gRPC `otelgrpc` client/server 端到端 Trace ID 透传。
- web、grpc、mixed 三类生成项目的配置、测试、构建与生命周期。

### 2026-07-14：P2 阶段 2 gRPC 客户端运行时

设计决定：

- 新增 `client/grpcx.Manager`，以 `app.Component` 管理多个具名、可复用的 `grpc.ClientConn`。
- 使用 `grpc.NewClient` 延迟建连。远端不可用不阻止应用启动，真正调用时返回 `Unavailable`；空地址、负数超时和无效 TLS CA 等本地配置错误在构造阶段失败。
- unary RPC 默认超时为 3 秒，`rpc_client.<name>.timeout` 可以覆盖；调用 context 已经存在 deadline 时，以调用方 deadline 和客户端 timeout 中更早到期的一个为准。
- 默认使用明文 HTTP/2，可按客户端开启 TLS、指定 server name，并在系统证书池上追加 CA 文件。
- 关闭配置式 gRPC retry，不增加 JGO 业务重试；grpc-go 仍负责连接断开后的正常重连。
- outbound gRPC 使用官方 `otelgrpc` client instrumentation 透传 W3C Trace Context；仅传输失败写结构化错误日志，不记录请求/响应正文。
- 下游依赖状态不接入 `/healthz`，健康检查继续只表示当前进程存活。
- 生成项目配置增加 `rpc_client` map；第一版不提供环境变量逐项覆盖，YAML 是客户端依赖配置入口。

验证范围：

- bufconn 成功调用和 Trace ID 透传。
- 默认/配置超时，以及调用方 deadline 与客户端 timeout 的最早期限优先。
- 服务端错误只调用一次，不发生配置式业务 retry。
- 不可用远端不阻止启动且实际调用返回 `Unavailable`。
- 配置、未知客户端、关闭状态和 TLS CA 校验。

### 2026-07-14：P2 阶段 3 公共 protobuf 协议项目

设计决定：

- `jgo new <name> --module <module> --type proto` 创建独立公共协议仓库；项目名和 module 完全由用户决定，`company-api` 只是示例。
- proto 项目只包含 `.proto`、Buf 配置、生成依赖和公共 `gen/pb` Go 包，不生成 `cmd/server`、运行配置、业务层或 transport。
- proto 项目的 `go.mod` 不依赖 JGO runtime，只依赖生成代码需要的 gRPC/protobuf runtime。
- `jgo rpc pbservice add` 用于在同一协议仓库增加 Service；`jgo rpc pbapi add` 按相同规范创建 RPC；`jgo rpc generate` 自动识别协议项目，仅执行工具检查、Buf lint、Response 契约校验和公共 Go 包生成。
- `.proto` 与 `gen/pb` 都属于公共 module 的发布内容，必须提交并通过 Go module tag 提供给服务端和调用方。
- `jgo generate`、`jgo doctor`、`jgo list` 支持 proto 项目；`jgo run` 和面向 server binary 的 `jgo build` 返回明确的不适用错误。
- 服务项目布局只要出现一部分关键文件就视为损坏，不会静默退化成 proto 模式。

验证范围：

- proto 项目树、派生 service/package 名、无 JGO 依赖和无 server 文件。
- 服务项目继续生成业务桩及 transport；proto 项目只生成 `service.pb.go`、`service_grpc.pb.go`。
- web、grpc、mixed、proto 四类项目真实创建、重复生成幂等、doctor、test 和 build。

### 2026-07-14：P2 阶段 4 protobuf 命令与服务接入

设计决定：

- 原 `jgo rpc add` 彻底替换为 `jgo rpc pbapi add`，不保留兼容入口；新增 `jgo rpc pbservice add`，用于在同一协议仓库增加 protobuf Service。
- proto 项目继续保留默认的 `<ProjectName>Service.Echo`；后续 Service 使用 `pbservice add`，不重复执行 `jgo new`。
- `rpc server add <Service> --module <module>@<version>` 用于 gRPC/mixed 服务端；生成公共协议 adapter、注册代码和缺失业务方法。
- `rpc client add <Service> --module <module>@<version>` 可用于 web/grpc/mixed 调用方；生成具名 `rpc_client` 配置、连接生命周期和类型安全 protobuf client 注入。
- module tag 仅选择公共协议仓库发布版本，不推断 protobuf API `v1/v2`。JGO 扫描 module 内全部 `gen/pb` package；Service 唯一时自动选择，同名跨版本时要求完整 `--package`。
- 客户端通过 `Service.RPC.<Name>` 直接持有生成的 protobuf client interface，不通过反射调用。
- `rpc client add --name` 同时决定配置 key 和注入字段，是稳定代码标识；首版不提供 rename。地址、超时和 TLS 属于运行配置，直接修改 YAML。
- server/client add 自动写入 module require 并执行 `go mod tidy`；本地开发可以使用标准 Go `replace` 指向协议仓库。
- 绑定清单保存在 `.jgo/rpc.json`，用于稳定记录 server/client 来源、版本、package 和方法，不依赖运行时服务发现。

验证范围：

- `pbservice add` 重复定义、多文件选择和 proto 语法安全。
- 旧 `rpc add` 入口不可用，所有模板和现行手册改用 `pbapi add`。
- 唯一 Service 自动发现、同名跨版本歧义和 `--package` 选择。
- 公共 proto module、gRPC 服务端和 web 调用方三项目生成、依赖整理、测试与构建。

### 2026-07-14：P2 阶段 5 三项目运行验收与发布准备

已完成：

- `scripts/verify-generation.sh` 在原有 web、grpc、mixed、proto 生成、幂等、测试和构建检查之后，继续组装并启动三个彼此独立的项目：公共 proto module、gRPC 服务端和 Web 调用方。
- 使用固定 W3C `traceparent` 发起 Web → gRPC 调用，验证 HTTP `X-Trace-ID`、HTTP response 和 gRPC 服务端结构化日志中的 trace_id 一致。
- 使用耗时 5 秒的 RPC 验证默认 3 秒客户端 timeout。验收发现 HTTP handler context 自带较长 deadline 时会跳过客户端 timeout，现已修正为两者取更早期限，并增加单元回归测试。
- 停止 gRPC 服务后，Web 调用方的 `/healthz` 继续返回 200；下一次业务调用返回系统错误，结构化客户端日志记录 gRPC `Unavailable`。
- 运行验收失败时自动输出服务端、调用方、HTTP header/body 等诊断信息，成功或失败都会停止子进程并清理临时项目。

验证结论：

- 公共协议发布物能够被服务端和调用方分别以 Go module 依赖接入。
- 延迟建连、类型安全 client 注入、Trace Context 透传、最早 deadline、无业务重试和进程级健康检查的组合行为符合 P2 设计。
- P2 阶段 1 至 5 的实现和文档已形成发布候选；创建版本、tag 和 GitHub Release 仍需维护者明确指定版本并授权。

### 2026-07-14：P2 阶段 6 发布前代码审查

审查修正：

- `rpc server add` 和 `rpc client add` 改为事务式文件更新；生成、配置或 `go mod tidy` 失败时恢复 `go.mod`、`go.sum`、生成代码、业务桩、YAML 和 `.jgo/rpc.json`，避免留下半完成项目。
- 同一 protobuf package 中多个 Service 的服务端接入只生成一次 import；同一 Service 以不同 `--name` 接入多个目标实例时也只生成一次 import，保证生成代码可编译。
- 客户端名称增加生成 Go 字段后的冲突检查，例如 `user_primary` 与 `userPrimary` 不允许同时生成同名字段。
- `logx` 修正混合使用 `slog.Attr` 和 key/value 参数时的显式 `trace_id` 检测，避免重复关联字段。
- 增加 tidy 失败回滚、protobuf import 去重、客户端字段冲突和混合日志参数回归测试。
- 完成敏感信息扫描与 `go mod verify`；命中的 token/password 文本均为验证错误脱敏和 Header 透传的固定测试数据，不包含真实凭据、邮箱或本机绝对路径。

下一版本号已确定为 `v0.3.0`；生成项目默认依赖、安装命令和发布文档已统一指向该版本。创建 tag 和 GitHub Release 仍需单独执行。
