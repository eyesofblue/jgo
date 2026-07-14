# JGO 快速入门

JGO 的 Go module 是 `github.com/eyesofblue/jgo`，最低支持 Go 1.24.0。

## 安装 CLI

安装前确认：

```bash
go version # 需要 Go 1.24.0 或更高版本
```

版本发布后可使用：

```bash
go install github.com/eyesofblue/jgo/cmd/jgo@latest
jgo --version
```

确保 `$(go env GOPATH)/bin` 已加入 `PATH`。如果首个版本尚未发布，请在 JGO 仓库中构建：

```bash
go build -trimpath -o bin/jgo ./cmd/jgo
./bin/jgo --help
```

完整依赖及锁定版本见 [dependencies.md](dependencies.md)。

## 创建项目

```bash
jgo new user-service \
  --module example.com/user-service \
  --type mixed
cd user-service
```

`--type` 支持 `web`、`grpc` 和 `mixed`。默认采用当前 `go env GOVERSION`，可以通过 `--go-version` 指定；创建过程会自动执行 `go mod tidy` 并生成 `go.sum`，离线环境可以使用 `--skip-tidy`。在尚未发布 JGO module 时，增加：

```bash
--jgo-replace /absolute/path/to/jgo
```

## 检查环境

```bash
jgo doctor
```

gRPC 项目需要锁定版 Buf 工具链。缺失时由用户显式安装：

```bash
jgo tools install
jgo tools check
jgo doctor
```

`tools check` 和 `doctor` 只检查，不会修改开发环境。JGO 使用 `GOTOOLCHAIN=local`，不会隐式切换 Go 工具链。

`--jgo-replace` 只用于框架本地开发：它会在新项目的 `go.mod` 中加入指向本机 JGO 源码的 `replace`。普通使用者应使用已发布的 `--jgo-version`，默认值为 `v0.2.0`。

## 生成、运行和构建

```bash
jgo generate
jgo list
jgo run
jgo build
```

`jgo build` 默认生成 `bin/<项目目录名>`，也可以使用 `--output/-o` 指定路径。

新增 gRPC 方法时，`jgo rpc add` 会自动为 response 保留非 optional 的 `int32 code = 1` 和 `string msg = 2`；业务字段从编号 `3` 开始。当前接口始终以 OpenAPI/proto 为准，使用 `jgo list` 查看，不需要手工维护第二份接口清单。

## 调试接口

```bash
jgo call http GetUser \
  --addr http://127.0.0.1:8080 \
  --data '{"uid":12345}'

jgo call grpc UserService.Echo \
  --addr 127.0.0.1:9090 \
  --data '{"message":"hello"}'
```

两种协议都支持可重复的 `--header/-H 'Name: Value'` 和 `--timeout`。

`jgo call grpc` 会显示普通 protobuf 字段的零值；未设置的 `optional` 和 message 字段仍然省略。

## Bash/Zsh completion

Bash 当前会话：

```bash
source <(jgo completion bash)
```

Zsh 可写入 `$fpath` 中的目录：

```bash
jgo completion zsh > "${fpath[1]}/_jgo"
```

所有命令和参数见 [command-reference.md](command-reference.md)，Web、gRPC 和 mixed 的完整示例见 [examples.md](examples.md)。
