# JGO

[English](README.md) | [简体中文](README.zh-CN.md)

JGO 是一个可独立使用的 Go 服务框架和项目脚手架，支持 HTTP/JSON 与 gRPC/protobuf 服务。

目前已经提供运行时、项目脚手架、契约生成、统一接口调试和开发流程命令。可以从[文档索引](docs/README.md)或[快速入门](docs/getting-started.md)开始了解。

## Module

```text
github.com/eyesofblue/jgo
```

首个版本系列为 `v0.1.x`，版本能力见 [CHANGELOG.md](CHANGELOG.md)。

## 前置依赖

| 项目类型 | 必需软件 |
| --- | --- |
| `web` | Go `1.22.0` 或更高版本 |
| `grpc` | Go `1.22.0` 或更高版本、Buf `1.46.0`、`protoc-gen-go` `1.36.7`、`protoc-gen-go-grpc` `1.5.1` |
| `mixed` | 与 `grpc` 相同的 Go 和 protobuf 工具链 |

JGO 不强制依赖数据库、Redis、消息队列、服务注册中心、配置中心或其他私有基础设施。后续可以通过应用组件和 HTTP/gRPC 中间件接入私有基础设施。

确保 Go 的二进制目录已经加入 `PATH`：

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

## 安装

安装已经发布的版本：

```bash
go install github.com/eyesofblue/jgo/cmd/jgo@latest
jgo --version
```

在首个 Release Tag 发布前，克隆仓库后从源码构建：

```bash
go build -trimpath -o bin/jgo ./cmd/jgo
./bin/jgo --version
```

gRPC 和 mixed 项目还需要安装锁定版本的生成工具。在 JGO 仓库或生成的项目中执行：

```bash
make tools
command -v buf protoc-gen-go protoc-gen-go-grpc
```

JGO 不会静默安装这些工具。完整 module 依赖和工具版本见[依赖说明](docs/dependencies.md)。

## 快速开始

创建三种类型中的任意一种项目：

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

生成的项目默认依赖 `github.com/eyesofblue/jgo v0.1.0`。开发和联调 JGO 本身时，可以增加 `--jgo-replace /absolute/path/to/jgo`；正常使用时不要提交包含本机路径的 `replace`。

进入生成的项目并检查环境：

```bash
cd demo-web
jgo doctor
jgo generate
jgo run
```

Web 服务默认监听 `:8080`，gRPC 服务默认监听 `:9090`。mixed 项目在同一个应用生命周期内同时启动两种服务。

## 增加 HTTP API

复杂请求和返回模型使用 `api/http/model/` 中的 Go struct 定义，然后增加接口并生成代码。JGO 支持 `GET /get_user?uid=12345` 和 `POST /update_user` 这样的 RPC 风格 HTTP API：

```bash
jgo api add UpdateUser \
  --method POST \
  --path /update_user \
  --request-params UpdateUserRequest \
  --response-data UserInfo

jgo api generate
```

HTTP 响应统一使用 `{"code":0,"msg":"","data":...}`：

```json
{"code": 0, "msg": "", "data": {"uid": 12345, "name": "Albert"}}
```

业务成功码固定为 `code: 0`。HTTP status 表示传输层结果，整数 `code` 表示业务结果，两者不会混用。

## 增加 gRPC API

gRPC 契约使用 protobuf 和锁定版本的 Buf 工具链：

```bash
make tools
jgo rpc add GetUser --service GreeterService
# 编辑生成的 GetUserRequest 和 GetUserResponse message 字段。
jgo rpc generate
```

JGO 锁定 Buf `1.46.0`、`protoc-gen-go` `1.36.7` 和 `protoc-gen-go-grpc` `1.5.1`，均兼容 Go 1.22.0 最低版本。protobuf 和 transport 生成文件可以覆盖更新，已有业务方法不会被覆盖。

gRPC 业务方法使用 `<Service><RPC>` 命名，例如 `GreeterServiceGetUser`，避免 mixed 项目中 HTTP 与 gRPC 接口同名冲突。对外 protobuf service 和 RPC 名称保持不变。

## 调试接口

HTTP 和 gRPC 使用相同的 JSON 输入方式。JGO 读取 OpenAPI 或 protobuf descriptor，不会为每个接口生成临时调试程序：

```bash
jgo list
jgo call http GetUser --addr http://127.0.0.1:8080 --data '{"uid":12345}'
jgo call grpc GreeterService.Echo --addr 127.0.0.1:9090 --data '{"message":"hello"}'
```

两种调用命令都支持可重复的 `--header 'Name: Value'` 和 `--timeout`。gRPC 优先使用服务端 Reflection，失败时自动读取 `api/proto/` 下的本地 protobuf 文件。

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
