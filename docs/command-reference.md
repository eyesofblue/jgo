# JGO CLI 命令参考

默认操作当前目录；多数命令支持 `--root <project>`。

## 项目

```bash
jgo new <name> --module <module> --type <web|grpc|mixed|proto> \
  [--output <dir>] [--jgo-version v0.4.1] [--go-version 1.24.0] \
  [--skip-tidy] [--jgo-replace <absolute-path>]

jgo generate [--root <project>]
jgo list [--root <project>]
jgo doctor [--root <project>]
jgo run [--root <project>] [service flags]
jgo build [--root <project>] [--output <file>]
```

`new` 创建空业务骨架并默认生成 `go.sum`。`generate` 是全局协调器，重建存在的 HTTP、本地 protobuf 和 `.jgo/rpc.json` 外部绑定；完全空项目返回成功。`run` 直接透传 `--config` 等服务参数。

## HTTP/OpenAPI

```bash
jgo api add <operation> --method <GET|POST|PUT|PATCH|DELETE> --path </path> \
  [--request 'name:type:required:query'] \
  [--request-params <GoStruct>] \
  [--response-data <type-or-GoStruct>] [--response-list]

jgo api generate
```

复杂 JSON 请求和大对象/数组响应使用 `api/http/model` 中的 Go struct。响应固定为 `{"code":0,"msg":"","data":...}`，HTTP status 与业务 code 分离。

## 本地 protobuf

```bash
jgo pb service add <Service> \
  [--package company.user.v1] [--file api/proto/.../service.proto]

jgo pb method add <Method> --service <Service> \
  [--file api/proto/.../service.proto]

jgo pb generate
jgo pb lint
jgo pb breaking --against <buf-source>
```

- 第一次 `service add` 默认创建 `<项目名>.v1`；`--package` 可创建或选择其他业务域/v2。
- 同一 package 只有一个文件时 `--package` 可自动定位；若分布在多个文件，必须改用 `--file` 明确目标。
- `api/proto` 下的 symlink 会被拒绝；JGO 不通过符号链接读取或修改项目外协议。
- `method add` 同时创建空 Request 和包含非 optional `code=1`、`msg=2` 的 Response。
- `pb generate` 只处理本地 proto；空协议是 no-op，不检查 Buf。
- `pb lint` 检查 Buf lint 与 JGO Response 规范。
- `pb breaking` 的 `--against` 必填，例如 `.git#branch=main`、`.git#tag=v1.0.0` 或本地目录。

## 外部 protobuf Service 绑定

```bash
jgo rpc server bind <Service> --module <module>[@<version>] \
  [--package <exact-go-import>]

jgo rpc server unbind <Service> [--package <exact-go-import>]

jgo rpc client bind <Service> --module <module>[@<version>] \
  [--package <exact-go-import>] [--name <client>] \
  [--address <target>]

jgo rpc client unbind <client-name>
```

绑定粒度始终是整个 protobuf Service；业务代码仍按 Method 调用。服务端以 `package + Service` 为唯一键，允许同名 v1/v2 并存；外部业务方法使用 `<PackagePath><Service><Method>`，所以两个版本采用相同 Go package 名也能共存。同名 Service 并存时，`server unbind` 必须用 `--package` 精确选择。`server bind` 只用于 grpc/mixed，`client bind` 可用于 web/grpc/mixed。

带 `@version` 时解析正式 module 版本或与该版本匹配的用户 `replace`，不会扫描 go.work 中的未发布代码。省略版本时只允许解析活动 `go.work` 中的同路径 module。

相同 Service/client name 重复 bind 是幂等更新：兼容 package 下可更新 module 版本；客户端地址、超时、TLS 和 readiness 不会被覆盖。切换到不同 protobuf package 时使用新的绑定身份，避免旧业务签名被静默替换。

新 client binding 的 readiness 默认是 `required`。非关键依赖需在 `configs/local.yaml` 中显式改成 `optional`；两种模式都不会阻止进程启动，但 required 失败会让 `/readyz` 返回 503。

`unbind` 是低频永久清理操作，不修改公共协议，不删除用户业务实现；完成后执行 tidy 和编译检查，失败自动回滚。

`.jgo/rpc.json` 是 JGO 管理的绑定清单，不建议手工编辑。`jgo doctor` 会校验清单结构、module/Service 漂移、生成文件和客户端配置；`jgo generate` 根据清单重建管理代码。

旧命令 `rpc pbservice add`、`rpc pbapi add`、`rpc generate`、`rpc server add`、`rpc client add` 已彻底移除，没有兼容别名。

## 工具链

```bash
jgo tools install
jgo tools check
```

仅拥有本地 proto 的开发环境需要锁定 Buf 工具链。external-only 服务不需要。

## 调试

```bash
jgo call http <operation-id> --addr <base-url> --data '<json>'
jgo call grpc <service.method> --addr <target> --data '<json>'
```

两者支持重复 `--header/-H` 与 `--timeout`。gRPC 优先使用 Reflection，失败后读取本地 proto；生产关闭 Reflection 时应携带本地协议描述。

## 补全

```bash
jgo completion bash
jgo completion zsh
```
