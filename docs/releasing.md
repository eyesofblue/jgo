# JGO 版本与发布流程

JGO 使用 Semantic Versioning，Git tag 使用 `vMAJOR.MINOR.PATCH`。

当前主干候选版本为 `v0.4.1`。在维护者实际创建 tag 前，不得对外宣称已经发布。

- `MAJOR`：稳定版之后的不兼容公开 API 变更。
- `MINOR`：向后兼容的新能力；在 `v0` 阶段也用于明确的接口演进。
- `PATCH`：向后兼容的问题修复和文档改进。

## v0.x 兼容与支持范围

- patch 版本不删除命令、flag、配置字段或公开 Go API，也不改变现有 manifest 的含义。
- minor 版本可以进行明确记录的接口演进；破坏性变化必须写入 Changelog 和迁移说明。
- `v0.4.x` CLI 支持读取、检查和重新生成 `v0.4.x` 创建的项目。新 CLI 遇到未来 manifest version 必须拒绝操作，不能猜测或降级写入。
- 生成项目默认依赖与 CLI 同版本的 JGO runtime；同一 `v0.4.x` 系列内允许用较新的 patch CLI 维护较早 patch 项目。
- JGO 只维护当前 minor 系列。进入新的 minor 后，旧 minor 只接受阻塞升级或安全问题的修复。

## 发布前检查

1. 确认 `go.mod` 仍为 Go 1.24.0 下限。
2. 执行 `make ci`；仓库验证固定使用 `GOWORK=off`，并包含 `go mod verify`、`go mod tidy -diff`。其中 generation-check 会执行 `./scripts/verify-generation.sh`，完成 proto module → gRPC server → Web caller 的真实运行验收，包括 trace_id、3 秒超时、`Unavailable`、Management `/healthz` 与 readiness。
3. 推送发布提交，在 Linux 和 macOS CI 上通过 test、race、vet、format、build 和 generation；必须等待目标 commit 的全部检查成功。
4. 只对上述已通过 CI 的精确 commit 创建 tag，不允许用 tag 绕过分支质量门禁。
5. 更新中英文 README、快速入门、实施记录、CHANGELOG 和 release notes。
6. 确认生成器、模板和锁定工具版本一致。
7. 对公共协议样例执行 `jgo pb lint` 和 `jgo pb breaking`。
8. 创建签名或 annotated tag，并由 tag 触发 `.github/workflows/release.yml`。

发布工作流会再次安装锁定的 protobuf 工具并执行完整 `make ci`，随后为 Linux/macOS 的 amd64/arm64 生成归档、生成 SHA-256 校验文件，并创建 GitHub Release。发布二进制通过 ldflags 写入 tag 版本，可使用 `jgo --version` 查看。

项目源码最低兼容 Go 1.24.0，CI 和发布工作流均使用 Go 1.24.x 验证最低版本。Go 1.24 起内部链接器默认给 macOS Mach-O 二进制写入 `LC_UUID`，避免 Go 1.22 产物在当前 macOS 上被 dyld 拒绝。

发布动作和版本号必须由维护者明确确认，自动化不得自行创建 tag。
