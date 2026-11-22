# G06 验收记录

范围：ENG-014/015、FR-06（Gateway/生成参数/路由 failover、统一执行、事件、SSE、取消与保存、聊天 Web）。

## 已实现

- `internal/runtime/llm`：`Gateway`/`DeploymentClient`/`Stream` 契约、`FailoverGateway`（分类上限、可重试错误才切换、超时/取消）、`OpenAIClient`（Generate/Stream，OpenAI 兼容）。
- `internal/runtime/engine`：`Event`/`Emitter`、`Executor.Run`（started → prompt_resolved → delta → failed/cancelled），只读会话 runtime config。
- `modelconfig.ResolveProfile`：方案版本 → 有序部署（解密 API Key 仅存内存，主备顺序，跳过停用/跨空间/悬挂 fallback）。
- `conversation`：`Create` 把快照 `prompt_id+env` 固化成版本写进 runtime config；`RunTurn` 租约 + 执行 + 同事务保存 + `run_completed`。
- `api/v1/chat.go`：`POST /conversations/:conversation_id/chat` SSE（逐事件 flush、`X-Accel-Buffering: no`）。
- 前端 `/conversations`：会话历史 + 聊天输入 + 流式回复 + 停止（abort）。

## 测试（一次性 PostgreSQL + Redis，race）

- `llm`：Gateway failover、分类拒绝、空部署、取消；`OpenAIClient` httptest（生成/流式/429·500·503 可重试、400·401·403 不重试、取消）。
- `conversation`：创建固化版本、pinned 优先、缺 resolver 拒绝；`RunTurn` 事件顺序、失败不保存且释放租约、busy 冲突。
- `api`：`TestConversationChatSSE`（事件序列、content-type、消息持久化、他人 404）、非法 ID 400。
- 前端 87 个测试（含 chat SSE 解析）+ 生产构建。

## 门禁（本地）

- `go test -race ./...`（一次性 PG + Redis）全部包通过。
- `go build`、`go vet`、`make docs-check`、`git diff --check`、`make deps-check`、`make web-check` 通过。

## 真实模型端到端（DeepSeek，负责人已授权）

使用开发库已配置的 DeepSeek provider（`api.deepseek.com`）+ 客服主模型 `deepseek-flash` + 客服模型方案（海外电商 workspace），走完整 HTTP/SSE 链路：

1. 登录 → 创建 Agent（`model_profile_version_id` + 字面量 system prompt）→ 创建会话 → `POST /conversations/:id/chat`。
2. SSE 事件序列正确：`run_started → prompt_resolved → model_delta×N → run_completed`，逐 delta 输出「我是你的智能客服助手，很高兴为你服务。」。
3. `run_completed` 携带真实 usage（prompt=44 / completion=62 / total=106）与 `model=deepseek-flash`。
4. SQL 复核：messages 两条（user → assistant，顺序稳定），会话 `status=active` 且 `turn_token`/`turn_lease_until` 均已清空。

链路验证点：`ResolveProfile` 解密 API Key → `FailoverGateway` 路由主部署 → `OpenAIClient` 调 DeepSeek 真实端点 → 流式 delta → 同事务保存消息并释放租约。

## 待完成 / 边界

- `run_started` 事件的 `agent_version_id`/`turn_revision` 当前为空（RunInput 未携带），属非阻塞完善项；功能与持久化不受影响。
- 桌面浏览器验收（聊天流 UI）待做；已有 Prompt/Agent/会话历史桌面验收不受影响。
- 远程 CI 以 PR Checks 为准。
- 工具调用、审批/A2UI、model_call_logs/观测 按 [NEXT](../handoff/NEXT.md) 延后归属。
