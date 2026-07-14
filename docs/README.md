# JGO 文档索引

JGO 是一个不依赖私有基础设施的 Go 服务框架和脚手架，支持 HTTP/JSON、gRPC/protobuf 以及同时包含两者的 mixed 项目。

## 使用者文档

- [快速入门](getting-started.md)：安装 CLI、创建项目、检查环境、生成、运行和构建。
- [命令参考](command-reference.md)：所有 `jgo` 命令、参数和调试调用方式。
- [依赖说明](dependencies.md)：最低 Go 版本、Buf 工具链、Go module 依赖和锁定版本。
- [完整示例](examples.md)：Web、gRPC 和 mixed 三类项目示例。

## 维护者文档

- [架构和实施记录](architecture-and-roadmap.md)：设计原则、阶段规划、实现记录和完整验收结论。
- [版本与发布流程](releasing.md)：版本规则、发布前检查、Tag 和 GitHub Release 流程。
- [变更记录](../CHANGELOG.md)：版本能力与兼容性说明。

## 核心约定

- 当前版本：`v0.2.0`（2026-07-14）
- Module：`github.com/eyesofblue/jgo`
- 最低 Go：`1.24.0`
- HTTP：RPC 风格路径，复杂入参和返回值使用 Go struct。
- HTTP 响应：`{"code":0,"msg":"","data":{}}`，HTTP status 与业务 code 分离。
- gRPC：protobuf-first，使用锁定版 Buf 工具链。
- 项目类型：`web`、`grpc`、`mixed`。
- 私有基础设施：默认不依赖，通过组件和中间件保留扩展能力。
