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
# 编辑 request；response 已有 code=1、msg=2，业务字段从编号 3 开始。
jgo generate
jgo run
```

调试：

```bash
jgo call grpc UserRpcService.GetUser \
  --addr 127.0.0.1:9090 \
  --data '{"uid":12345}'
```

所有由 `jgo rpc add` 创建的响应都包含：

```proto
message GetUserResponse {
  int32 code = 1;
  string msg = 2;
  UserInfo data = 3;
}
```

`code`、`msg` 使用普通字段。业务字段是否使用 `optional` 由接口语义决定；调试 JSON 会显示所有普通字段的零值，并省略未设置的 `optional` 字段。

业务方法可以返回 JGO 业务错误：

```go
return nil, jerrors.New(10001, "invalid uid")
```

生成的 transport 将其转换为 gRPC status `OK` 的响应：

```json
{"code": 10001, "msg": "invalid uid"}
```

未知错误、panic、context cancellation 和 deadline exceeded 不会伪装成业务响应，仍通过非 `OK` gRPC status 返回。

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
