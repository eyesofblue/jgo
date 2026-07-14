# JGO 架构设计与实施路线

> 状态：`v0.1.0` 已发布，`v0.2.0` P0/P1 改造已完成，等待发布验收
> Go module：`github.com/eyesofblue/jgo`  
> 最低 Go 版本：`1.24`
> License：`Apache-2.0`  
> 文档目标：记录 JGO 的已确认需求、架构边界、脚手架命令、执行步骤和验收标准。

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

JGO 支持三种项目类型：

| 类型 | 说明 | 协议文件 |
| --- | --- | --- |
| `web` | 仅提供 HTTP API | OpenAPI |
| `grpc` | 仅提供 gRPC API | protobuf |
| `mixed` | 同时提供 HTTP 和 gRPC，共用业务层 | OpenAPI + protobuf |

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
- `jgo rpc generate` 封装 Buf 流程，用户日常不需要直接组合 protobuf 生成命令。
- `jgo tools install` 使用当前 Go 环境安装锁定工具，`jgo tools check` 检查路径、版本、构建 Go 版本和可执行性。
- `jgo doctor` 在 gRPC/mixed 项目中复用工具检查。
- JGO 不静默修改用户机器环境；工具缺失时输出明确安装指引。
- Buf、`protoc-gen-go` 和 `protoc-gen-go-grpc` 的版本必须固定，并兼容 Go 1.24。

阶段 6 锁定的生成工具版本：

- Buf `v1.46.0`：当前已完成真实生成回归的锁定版本。
- `protoc-gen-go v1.36.7`：与框架 protobuf runtime 保持一致。
- `protoc-gen-go-grpc v1.5.1`：当前已完成真实生成回归的锁定版本。
- `github.com/bufbuild/protocompile v0.14.1`：用于 proto AST 解析、语法校验和安全定位，不依赖正则或特殊注释锚点修改契约。

gRPC 运行时已锁定：

- `google.golang.org/grpc v1.71.3`：当前运行时锁定版本。
- `google.golang.org/protobuf v1.36.7`：当前 protobuf runtime 锁定版本。
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
│   ├── requestid/
│   ├── accesslog/
│   └── timeout/
├── config/                      # 本地文件和环境变量配置
├── errors/                      # 统一业务错误
├── response/                    # HTTP 统一响应
├── health/                      # liveness/readiness
├── log/                         # 基于 slog 的日志能力
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
├── api/proto/user/v1/user.proto
├── gen/pb/
├── internal/
│   ├── service/
│   └── transport/grpc/
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
│   └── proto/user/v1/user.proto
├── gen/
│   ├── http/
│   └── pb/
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
jgo rpc add GetUser --service UserService
jgo rpc generate
```

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
- request ID。
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
- request ID 通过 HTTP header 传递，不增加到标准响应 body。

### 8.2 gRPC

- 优雅停机和强制停机兜底。
- recovery interceptor。
- request ID/metadata 传递。
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
3. 实现 recovery、request ID、access log 和 timeout 中间件。
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
4. 实现 request ID/metadata 传递。
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

1. 实现 `jgo rpc add`。
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
- 实现 request ID 传递与生成，并拒绝不安全的外部 request ID。
- 实现基于 `log/slog` 的结构化 access log。
- 实现 handler timeout，使用缓冲响应防止超时 handler 继续写客户端。
- 实现 panic recovery；响应未提交时返回统一 JSON 错误，已提交时不拼接二次响应。
- 实现 `errors.Error` 公开错误模型，对客户端隐藏未知错误和内部 cause。
- 实现 `response.Envelope`、成功响应和统一错误响应。
- 实现 `health.Probe`、`GET /healthz` 和 `GET /readyz`。
- readiness 默认为 false，支持进程就绪门和多个依赖检查。
- 默认中间件顺序为 request ID → access log → timeout → recovery → application handler。
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
- 实现 unary 和 stream request ID interceptor，支持 metadata 传入、context 注入和 response header 回传。
- 实现 unary 和 stream panic recovery interceptor，使用 `slog` 记录堆栈。
- 实现 unary 和 stream 错误转换 interceptor。
- 实现 HTTP status 到 gRPC code 的稳定映射，未知错误统一转换为 `codes.Internal`。
- 阶段 3 初版曾通过 gRPC `ErrorInfo` detail 传递业务码；v0.2.0 P1 已统一改为 Response `code/msg`，status details 不再承载业务码。
- 保留已有 gRPC status error，并正确映射 `context.Canceled` 和 `context.DeadlineExceeded`。
- 实现活动 unary/stream RPC 跟踪和 draining 状态，停机后拒绝新 RPC。
- 优雅停机先等待活动 RPC 归零；超时后直接强制 Stop，避免 gRPC-Go v1.71.3 中并发 `GracefulStop`/`Stop` 的内部锁竞争。
- 完成 bufconn 端到端测试，覆盖 request ID、业务错误、panic recovery 和 Reflection。
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
- HTTP status 与整数业务错误码独立管理；request ID 继续通过 HTTP header 传递。
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

- 实现 `jgo rpc add <rpc-name> --service <service-name>`，并提供可选 `--file` 处理多文件同名 service。
- 阶段 6 初版的 `rpc add` 创建两个空 message；v0.2.0 P1 已将 response 调整为自动包含标准 `code/msg` 字段。
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
jgo rpc add GetUser --service GreeterService

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

- 初始 Echo 和 `jgo rpc add` 创建的每个 response 固定声明非 optional 的 `int32 code = 1`、`string msg = 2`，用户业务字段从编号 `3` 开始。
- `code = 0` 表示业务成功；gRPC status 继续表达传输或系统错误，不与业务码混用。
- `jgo call grpc` 使用 `protojson.EmitDefaultValues`，普通无 presence 字段的零值会显示，未设置的 optional/message 字段仍然省略。
- `jgo rpc generate`、统一 `jgo generate` 和 `jgo doctor` 强制校验所有 response；非标准契约直接失败，不保留存量兼容分支。
- `rpc add` 的结果提示明确指出保留字段和下一步命令。
- 生成项目 README 只维护稳定工作流，不复制容易过期的接口清单；OpenAPI/proto 是协议真源，`jgo list` 展示当前接口。
- 中英文 README、命令参考、示例和变更记录同步上述约定。
- 生成 unary transport 将显式 `jgo/errors.Error` 转换为 gRPC `OK` 的 Response `code/msg`；panic、未知错误、取消和超时继续使用非 `OK` status。
- Response 标准检查改用完整 protobuf descriptor，覆盖跨文件和 import 场景。
- generate 输出本次新建的业务文件以及需要实现的 `Service.<Method>`。
