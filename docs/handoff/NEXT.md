# 下一批：G05 整组验收与 PR

G05（ENG-012/013）功能已完成：Agent 版本/环境/快照/管理页、会话/消息/工具历史、归属、租约与历史页；正式测试与桌面验收通过，见 [G05 验收](../delivery/G05-VERIFICATION.md)。

1. **全量门禁复核。** `make deps-check`、`make build`、`go vet ./...`、`git diff --check`、`make docs-check`、`make web-check`，以及显式 `ASTERFLOW_TEST_DATABASE_URL` + `ASTERFLOW_TEST_REDIS_URL` 下的 `TZ=UTC go test -race ./...`。
2. **桌面复测。** 一次完整链路：提示词、Agent、会话历史；SQL 复核指针与租约清理。
3. **提交与 PR。** 学员执行 Git：提交、推送 `feat/ENG-012-013-agent-conversation`、按模板创建 PR（范围、验证、边界：G06 才接真实模型/SSE/租约 HTTP 端点）；助手只读复审与 CI 核对，确认后再合并。
4. 合并后进入 G06：真实 Gateway、统一 ADK、事件/SSE、取消与保存。

注意：租约端点（Claim/Renew/Commit/Release）目前只在 Store 与测试中使用，聊天 HTTP 在 G06 接入；不要把 Store 测试当作聊天闭环完成。
