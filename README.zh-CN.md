# JGO

[English](README.md) | [简体中文](README.zh-CN.md)

JGO 是一个可独立使用的 Go 服务框架和项目脚手架，支持 HTTP/JSON 与 gRPC/protobuf 服务。

目前已经提供运行时、项目脚手架、契约生成、统一接口调试和开发流程命令。可以从[文档索引](docs/README.md)或[快速入门](docs/getting-started.md)开始了解。

## Module

```text
github.com/eyesofblue/jgo
```

当前发布版本为 `v0.2.0`，版本能力见 [CHANGELOG.md](CHANGELOG.md)。

## 前置依赖

| 项目类型 | 必需软件 |
| --- | --- |
| `web` | Go `1.24.0` 或更高版本 |
| `grpc` | Go `1.24.0` 或更高版本、Buf `1.46.0`、`protoc-gen-go` `1.36.7`、`protoc-gen-go-grpc` `1.5.1` |
| `mixed` | 与 `grpc` 相同的 Go 和 protobuf 工具链 |

JGO 不强制依赖数据库、Redis、消息队列、服务注册中心、配置中心或其他私有基础设施。后续可以通过应用组件和 HTTP/gRPC 中间件接入私有基础设施。

确保 Go 的二进制目录已经加入 `PATH`：

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

## 安装

安装已经发布的版本：

```bash
go install github.com/eyesofblue/jgo/cmd/jgo@v0.2.0
jgo --version
```

需要始终安装最新发布版本时，可以把版本改为 `@latest`。

参与 JGO 框架开发时，也可以克隆仓库后从源码构建：

```bash
go build -trimpath -o bin/jgo ./cmd/jgo
./bin/jgo --version
```

gRPC 和 mixed 项目还需要安装锁定版本的生成工具。每个 Go 开发环境执行一次：

```bash
jgo tools install
jgo tools check
```

JGO 使用 `GOTOOLCHAIN=local`，不会静默下载或切换 Go 工具链。完整 module 依赖和工具版本见[依赖说明](docs/dependencies.md)。

## 新项目：创建服务和首个接口

生成的项目默认依赖 `github.com/eyesofblue/jgo v0.2.0`，默认采用当前 `go env GOVERSION`，也可以用 `--go-version` 显式指定。创建过程会执行 `go mod tidy` 并生成 `go.sum`；离线环境可使用 `--skip-tidy`。开发和联调 JGO 本身时，可以增加 `--jgo-replace /absolute/path/to/jgo`；正常使用时不要提交包含本机路径的 `replace`。

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
jgo rpc add GetUser --service UserRpcService
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
jgo rpc add GetUser --service UserService
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

该命令会执行格式检查、单元测试、竞态测试、`go vet`、CLI 构建，以及 web、gRPC、mixed 三类项目的真实生成和构建检查。

## 文档

- [文档索引](docs/README.md)
- [安装与快速入门](docs/getting-started.md)
- [CLI 命令参考](docs/command-reference.md)
- [依赖与锁定版本](docs/dependencies.md)
- [Web、gRPC 和 mixed 示例](docs/examples.md)
- [架构和实施记录](docs/architecture-and-roadmap.md)
- [发布流程](docs/releasing.md)

## License

Apache License 2.0。
