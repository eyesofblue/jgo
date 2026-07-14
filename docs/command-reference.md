# JGO CLI 命令参考

所有项目命令默认在当前目录执行；在其他目录操作时使用 `--root /path/to/project`。可随时运行 `jgo <command> --help` 查看与当前二进制一致的帮助。

## 全局命令

```bash
jgo --help
jgo --version
jgo completion bash
jgo completion zsh
```

## 创建项目

```bash
jgo new <project-name> \
  --module <go-module> \
  --type <web|grpc|mixed|proto> \
  [--output <directory>] \
  [--jgo-version <version>] \
  [--go-version <version>] \
  [--skip-tidy] \
  [--jgo-replace <absolute-local-path>]
```

- `--module`、`--type` 必填。
- `--output/-o` 默认为项目名。
- `--jgo-version` 默认 `v0.3.0`。
- `--go-version` 默认取当前 `go env GOVERSION`，最低为 `1.24.0`。
- 默认执行 `go mod tidy` 并生成 `go.sum`；`--skip-tidy` 仅用于离线或受控环境。
- `--jgo-replace` 仅用于 JGO 本地源码联调。
- `proto` 只生成公共 protobuf Go module，不包含服务进程或 JGO 运行时依赖。

## protobuf 工具链

```bash
jgo tools install
jgo tools check
```

- `tools install` 使用当前 Go 环境安装锁定版本的 Buf 和 protobuf 插件，并设置 `GOTOOLCHAIN=local`。
- `tools check` 只读检查工具路径、版本、构建 Go 版本和可执行性。
- `doctor` 会在 gRPC、mixed 和 proto 项目中复用同一套检查。

## HTTP/OpenAPI

```bash
jgo api add <operation-name> \
  --method <GET|POST> \
  --path </rpc_style_path> \
  [--request 'name:type:required:query'] \
  [--request-params <GoStruct>] \
  [--response-data <primitive-or-GoStruct>] \
  [--response-list] \
  [--root <project>]

jgo api generate [--root <project>]
```

`--request` 可重复，格式是 `name:type[:required|optional][:query|header|body]`。简单 GET 参数可使用 `--request uid:int64:required:query`；复杂 JSON body 使用 `--request-params` 引用 `api/http/model/` 中的 Go struct。`--response-data` 同样支持 Go struct，`--response-list` 把 `data` 声明为数组。

HTTP 响应固定为：

```json
{"code": 0, "msg": "", "data": {}}
```

HTTP status 表示传输层结果，`code` 是独立的整数业务码，成功值为 `0`。

## gRPC/protobuf

```bash
jgo rpc pbservice add <service-name> \
  [--file <relative-proto-file>] \
  [--root <project>]

jgo rpc pbapi add <rpc-name> \
  --service <service-name> \
  [--file <relative-proto-file>] \
  [--root <project>]

jgo rpc generate [--root <project>]

jgo rpc server add <service-name> \
  --module <module>@<version> \
  [--package <exact-go-import>] \
  [--root <service-project>]

jgo rpc client add <service-name> \
  --module <module>@<version> \
  [--package <exact-go-import>] \
  [--name <rpc-client-name>] \
  [--address <host:port>] \
  [--root <service-project>]
```

`rpc pbservice add` 在已有 proto 文件中创建一个空 protobuf Service；多个 proto 文件存在时使用 `--file` 指定目标。`rpc pbapi add` 创建空 request message，并为 response 自动声明非 optional 的 `int32 code = 1` 和 `string msg = 2`。业务响应字段从编号 `3` 开始。旧的 `jgo rpc add` 已移除，不保留兼容入口。

`rpc generate` 使用锁定的 Buf 工具链，覆盖生成代码；任何 response 不符合标准都会直接阻断生成。服务项目还会生成 transport 和缺失的业务方法，但不会覆盖已有业务实现；proto 项目只生成 `gen/pb` 公共包。服务项目的业务方法按 `<Service><RPC>` 命名，例如 `UserService.GetUser` 对应 `Service.UserServiceGetUser`，以避免 mixed 项目协议间的方法冲突。

`rpc server add` 仅用于 gRPC/mixed 项目，生成公共 Service 的注册 adapter 和缺失业务方法。`rpc client add` 可用于 web/gRPC/mixed 项目，生成类型安全的 protobuf client 集合、`rpc_client` YAML 配置和应用生命周期接线。两者都把 module require 写入 `go.mod` 并执行 `go mod tidy`。

上述写入是事务式的：生成、配置写入或 `go mod tidy` 任一步失败时，会恢复命令执行前的 module 文件、生成代码、YAML、业务桩和 `.jgo/rpc.json`。`.jgo/rpc.json` 是项目协议绑定清单，应随代码一起提交。相同 protobuf package 中的多个 Service，以及同一 Service 使用不同 `--name` 创建的多个客户端实例，会复用同一 Go import。

客户端 `--name` 同时决定 `rpc_client.<name>` 配置 key 和 `Service.RPC.<Name>` 代码字段，是创建时确定的稳定标识，不提供 rename 操作。地址、超时、`tls.enabled`、`tls.server_name`、`tls.ca_file` 都是运行配置，可以直接修改 YAML，无需重新生成代码。

`--module` 必须包含明确版本，例如 `example.com/company-api@v0.1.1`。该版本是 Go module 发布版本，不用于推断 protobuf API 的 `v1/v2`。JGO 扫描 module 下所有 `gen/pb` package：Service 唯一时自动选择；同名 Service 存在多个版本时列出候选并要求 `--package`。

生成完成后会逐项输出本次新建的业务文件和方法。生成 transport 将业务方法返回的 `jgo/errors.Error` 转换为 gRPC `OK` Response 的 `code/msg`；非业务错误继续使用非 `OK` gRPC status。Response 即使定义在导入的其他 proto 文件中，也会参与标准字段检查。

业务错误码必须位于正数 `int32` 范围内；超出范围的 `jgo/errors.Error` 会被安全归一化为内部错误码。

## 统一开发流程

```bash
jgo doctor [--root <project>]
jgo generate [--root <project>]
jgo list [--root <project>]
jgo run [--root <project>] [server arguments]
jgo build [--root <project>] [--output <binary>]
```

- `doctor` 检查项目结构、Go 版本、JGO module、契约和 gRPC 工具链，不修改环境。
- `generate` 按项目类型生成 HTTP、gRPC 或两者；mixed 项目会先检查 gRPC 工具，避免半生成状态。
- `list` 从本地 OpenAPI 和 protobuf 契约列出接口。
- `run` 执行 `go run ./cmd/server`，额外位置参数透传给服务程序。
- `build` 默认输出 `bin/<项目目录名>`，`--output/-o` 可覆盖。

生成项目的服务程序支持 `--config`、`--service-name`、`--http-address`、`--grpc-address` 和 `--shutdown-timeout`。配置优先级为命令参数、环境变量、YAML、默认值。

## 调试调用

```bash
jgo call http <operation-id> \
  --addr <base-url> \
  [--data '<json-object>'] \
  [--header 'Name: Value'] \
  [--timeout 10s] \
  [--root <project>]

jgo call grpc <service.method> \
  --addr <host:port> \
  [--data '<json-object>'] \
  [--header 'Name: Value'] \
  [--timeout 10s] \
  [--root <project>]
```

`--header/-H` 可重复，`--data/-d` 默认 `{}`。HTTP 调用按 OpenAPI 契约组装请求；gRPC 调用优先使用服务端 Reflection，失败后读取项目 `api/proto/` 下的本地描述。

`jgo call grpc` 使用 protobuf JSON 的默认值展示模式：没有 presence 的普通标量字段即使为 `0`、`""` 或 `false` 也会输出；未设置的 `optional` 字段和 message 字段保持省略。该行为只影响调试 JSON，不改变 protobuf 二进制协议。
