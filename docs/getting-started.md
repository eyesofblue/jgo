# JGO 快速入门

最低 Go 版本是 1.24.0。

```bash
go install github.com/eyesofblue/jgo/cmd/jgo@v0.4.1
```

## 创建空服务

```bash
jgo new user-service --module example.com/user-service --type mixed
cd user-service
jgo doctor
go test ./...
```

新项目没有 Echo/Greeter。`jgo new` 已执行 `go mod tidy` 并生成 `go.sum`。

## 增加 HTTP API

```bash
# 在 api/http/model 定义 UserInfo
jgo api add GetUser --method GET --path /get_user \
  --request uid:int64:required:query \
  --response-data UserInfo
jgo api generate
```

## 增加项目自有 gRPC 协议

```bash
jgo tools install # 当前 Go 环境只需一次
jgo pb service add UserService
jgo pb method add GetUser --service UserService
jgo pb generate
```

## 只实现公共协议

```bash
jgo rpc server bind UserService \
  --module example.com/company-api@v0.1.0
```

这种 external-only 服务不需要本地 proto，也不需要 Buf。

## 调用公共协议

```bash
jgo rpc client bind UserService \
  --module example.com/company-api@v0.1.0 \
  --name user
```

修改 `configs/local.yaml` 中 `rpc_client.user` 的地址、3 秒超时、TLS 和 `readiness`。新绑定默认 `readiness: required`；只有非关键依赖才显式改为 `optional`。

## 本地联调未发布协议

```bash
go work init
go work use ./company-api ./user-service
cd user-service
jgo rpc server bind UserService --module example.com/company-api
```

## 生成、运行与运维端点

```bash
jgo generate
jgo list
jgo run --config configs/local.yaml
```

- 业务 HTTP：`:8080`
- gRPC：`:9090`
- Management：`:9091/healthz`、`:9091/readyz`、`:9091/metrics`

本地 Reflection 开启，框架默认关闭。YAML 使用严格模式。required 下游不可用时进程仍启动，但 `/readyz` 返回 503。

完整命令见 [command-reference.md](command-reference.md)，生产配置与设计见 [README 中文版](../README.zh-CN.md)。
