# G05 验收记录

范围：ENG-012（Agent 版本/环境/快照/管理 Web）与 ENG-013（会话/消息/工具历史、归属、租约）。

## Agent

- 迁移 000009：`agents`、`agent_versions`（快照 TEXT + `pg_input_is_valid`、`UNIQUE(agent_id, version)` 与 `UNIQUE(agent_id, id)`）、`agent_envs`（复合外键）。
- Store/Service/Gin：原子创建 v1 + dev 指针、锁父行递增版本、读取、环境发布；快照构建校验 Prompt 版本与模型方案版本的空间可见性，字面量与 pinned 版本互斥。
- 正式测试：`TestAgentCreateAndAppend`、`TestAgentReadAndPublish`、`TestAgentServiceSnapshotAndPublish`、`TestAgentHTTP`。
- React `/agents`：列表、创建、版本/快照、追加、三环境卡片与发布。

## 会话

- 迁移 000010：`conversations`（固定 agent_version_id、workspace/user、租约 token/until/revision）、`messages`、`tool_calls`；租约恢复索引。
- Store：创建（版本复合外键）、按空间/用户列表、消息与工具读取、Claim/Renew/Commit/Release 单 turn 租约；提交在事务内插消息与工具并校验 token。
- Service/HTTP：创建时校验 Agent 版本归属；他人会话一律 404，owner/admin 可看全空间；`GET /conversations/:id` 返回会话与消息。
- 正式测试：`TestConversationStoreAndTurnLease`（第二次 Claim 冲突、旧 token 回滚、重复释放、过期可抢占）、`TestConversationHTTP`（member 创建、viewer 403、他人 404、管理员可见、跨空间 404、非法 UUID 400）。
- React `/conversations`：历史列表（状态/用户/版本/时间）与消息详情（角色、内容、工具调用、tool_call_id）。

## 本地整组验收（2026-11-20）

- `make deps-check`、`make build`、`go vet ./...`：通过。
- `TZ=UTC go test -race ./...`（一次性 PostgreSQL + Redis，独立 schema）：全部包通过。
- `make web-check`：84 个测试 + 生产构建通过。
- Chromium 1440×1000 + 正式二进制 + 一次性 PG16：提示词全流程、Agent 创建 v1/追加 v2/发布 dev v2、会话经 API 创建并在历史页显示；SQL 复核 agents=1、agent_versions=2、dev=v2、conversations=1。
- 边界：未调用真实模型（G06）；远程 CI 以 PR Checks 为准。
