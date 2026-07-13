# JGO 版本与发布流程

JGO 使用 Semantic Versioning，Git tag 使用 `vMAJOR.MINOR.PATCH`。

首个确认版本为 `v0.1.0`。在维护者实际创建 tag 前，CHANGELOG 中保持 `Unreleased`，不得提前宣称已经发布。

- `MAJOR`：稳定版之后的不兼容公开 API 变更。
- `MINOR`：向后兼容的新能力；在 `v0` 阶段也用于明确的接口演进。
- `PATCH`：向后兼容的问题修复和文档改进。

## 发布前检查

1. 确认 `go.mod` 仍为 Go 1.22.0 下限。
2. 执行 `make ci`。
3. 执行 `./scripts/verify-generation.sh`。
4. 在 Linux 和 macOS CI 上通过 test、race、vet、format 和 build。
5. 更新 README、快速入门、实施记录和 release notes。
6. 确认生成器、模板和锁定工具版本一致。
7. 创建签名或 annotated tag，并由 tag 触发 `.github/workflows/release.yml`。

发布工作流会为 Linux/macOS 的 amd64/arm64 生成归档、生成 SHA-256 校验文件，并创建 GitHub Release。发布二进制通过 ldflags 写入 tag 版本，可使用 `jgo --version` 查看。

发布动作和首个版本号必须由维护者明确确认，自动化不得自行创建 tag。
