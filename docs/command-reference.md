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
  --type <web|grpc|mixed> \
  [--output <directory>] \
  [--jgo-version <version>] \
  [--jgo-replace <absolute-local-path>]
```

- `--module`、`--type` 必填。
- `--output/-o` 默认为项目名。
- `--jgo-version` 默认 `v0.1.0`。
- `--jgo-replace` 仅用于 JGO 本地源码联调。

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
jgo rpc add <rpc-name> \
  --service <service-name> \
  [--file <relative-proto-file>] \
  [--root <project>]

jgo rpc generate [--root <project>]
```

`rpc add` 创建空 request/response message；随后在 `.proto` 中编辑字段，再执行生成。`rpc generate` 使用锁定的 Buf 工具链，覆盖生成代码，但不会覆盖已存在的业务方法。生成的业务方法按 `<Service><RPC>` 命名，例如 `GreeterService.GetUser` 对应 `Service.GreeterServiceGetUser`，以避免 mixed 项目协议间的方法冲突。

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
