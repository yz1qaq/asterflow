# 下一批：G04 整组验收与 PR

G04 功能已全部保存：版本/环境指针/发布/回滚/env 事件、实验与灰度（含多变体）、diff、事件时间线、Redis 跨进程失效与双进程验收。下一批不写新功能，集中验收和交付。

1. **证据复核。** 对照 [G04 任务](../tasks/G04-prompt-governance.md) 与 [G04 验收](../delivery/G04-VERIFICATION.md) 逐项确认：迁移 000006–000008、Store/Service/HTTP、React 三个标签页、操作事件、Redis。检查 [TRACEABILITY](../reference/TRACEABILITY.md) 与 [BACKLOG](../delivery/BACKLOG.md) 是否需要更新状态。
2. **全量门禁。** 一次跑齐并记录：`make build`、`go vet ./...`、`git diff --check`、`make docs-check`、`make web-check`、以及设置显式 `ASTERFLOW_TEST_DATABASE_URL` 与 `ASTERFLOW_TEST_REDIS_URL` 后的 `TZ=UTC go test -race ./...`。全部使用一次性容器；不碰开发库。`make test` 是否自动要求 Redis 由学员决定，至少保留显式环境变量途径。
3. **桌面与双进程。** 重新跑一次完整桌面链路（版本、发布/回滚、diff、灰度、多变体、记录）和两服务共享 Redis 的验证；截图/输出只作证据，不入库。
4. **提交与 PR。** 学员执行 Git：整理提交、推送分支 `feat/ENG-010-011-prompt-governance`、创建 PR 并按模板写变更/验证/边界；助手只做只读复审与 CI 结果核对，不代提交、不合并。合并后再开始 G05。

注意：G04 的“完成”以实际命令输出和 PR 合并为准，不以本轮聊天结论代替；Redis 负责尽早释放内存，正确性还依赖“解析结果与缓存版本比对 + 回 DB 解析”。
