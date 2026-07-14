# JGO 示例

## Web

```bash
jgo new user-web --module example.com/user-web --type web
cd user-web
jgo api add GetUser --method GET --path /get_user \
  --request uid:int64:required:query \
  --response-data UserInfo
jgo api generate
jgo run --config configs/local.yaml
```

## 项目自有 protobuf

```bash
jgo new user-rpc --module example.com/user-rpc --type grpc
cd user-rpc
jgo pb service add UserService --package company.user.v1
jgo pb method add GetUser --service UserService
jgo pb generate
jgo run --config configs/local.yaml
```

## 公共协议、服务端和 Web 调用方

```bash
# 公共协议
jgo new company-api --module example.com/company-api --type proto
cd company-api
jgo pb service add UserService --package company.user.v1
jgo pb method add GetUser --service UserService
jgo pb generate

# gRPC 服务端
jgo new user-service --module example.com/user-service --type grpc
cd user-service
jgo rpc server bind UserService --module example.com/company-api@v0.1.0

# Web 调用方
jgo new user-gateway --module example.com/user-gateway --type web
cd user-gateway
jgo rpc client bind UserService \
  --module example.com/company-api@v0.1.0 \
  --name user
```

协议尚未发布时，在三者父目录使用：

```bash
go work init
go work use ./company-api ./user-service ./user-gateway
```

然后 bind 的 `--module` 可以省略 `@version`。

## 协议兼容检查

```bash
jgo pb lint
jgo pb breaking --against '.git#branch=main'
```

生成的 GitHub Actions 工作流会在 Pull Request 中自动运行同样的检查。

## 接口调试

```bash
jgo list
jgo call http GetUser \
  --addr http://127.0.0.1:8080 \
  --data '{"uid":12345}'
jgo call grpc UserService.GetUser \
  --addr 127.0.0.1:9090 \
  --data '{"uid":"12345"}'
```
