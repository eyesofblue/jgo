# JGO 项目示例

以下示例都使用 RPC 风格 HTTP 路径和 protobuf-first gRPC 契约。

## Web

```bash
jgo new user-web --module example.com/user-web --type web
cd user-web
```

先在 `api/http/model/user.go` 定义复杂模型：

```go
package model

type UserInfo struct {
	UID  int64  `json:"uid"`
	Name string `json:"name"`
}

type UpdateUserRequest struct {
	UID  int64  `json:"uid"`
	Name string `json:"name"`
}
```

然后更新契约并生成：

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

jgo generate
jgo run
```

生成的业务占位方法名为 `UserRpcServiceGetUser`；gRPC transport 仍对外实现 protobuf 中的 `UserRpcService.GetUser`。

复杂请求和返回值在 `api/http/model/` 中定义为 Go struct。HTTP body 固定为：

```json
{"code": 0, "msg": "", "data": {}}
```

HTTP status 与业务 `code` 独立。

## gRPC

```bash
jgo new user-rpc --module example.com/user-rpc --type grpc
cd user-rpc
jgo tools install

jgo rpc add GetUser --service UserRpcService
# 编辑 api/proto/user_rpc/v1/service.proto 中的 request/response 字段。
jgo generate
jgo run
```

调试：

```bash
jgo call grpc UserRpcService.GetUser \
  --addr 127.0.0.1:9090 \
  --data '{"uid":12345}'
```

## mixed

```bash
jgo new user-service --module example.com/user-service --type mixed
cd user-service
jgo tools install

# 按 Web 和 gRPC 示例分别维护两份协议契约。
jgo generate
jgo list
jgo run
```

mixed 项目使用同一个 `app.App` 生命周期和 `internal/service` 业务层，同时启动 HTTP `:8080` 与 gRPC `:9090`。
