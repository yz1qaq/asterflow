# G06 验收记录

范围：ENG-014/015、FR-03、FR-07（Gateway/生成参数/路由 failover、统一 ADK 执行、事件、同步聚合/SSE、取消与保存、聊天 Web）。

初版提交 `25dfd8b` 由 [PR #17](https://github.com/yz1qaq/asterflow/pull/17) Squash 合并为 `61fd528`。本文后续记录的补充功能在本地通过验收，远程 CI 和评审以补充 PR 的 head 为准。

## 初期单轮路径

- `internal/runtime/llm`：`Gateway`/`DeploymentClient`/`Stream` 契约、`FailoverGateway`（分类上限、可重试错误才切换、超时/取消）、`OpenAIClient`（Generate/Stream，OpenAI 兼容）。
- `internal/runtime/engine`：`Event`/`Emitter`、`Executor.Run`（started → prompt_resolved → delta → failed/cancelled），只读会话 runtime config。
- `modelconfig.ResolveProfile`：方案版本 → 有序部署（解密 API Key 仅存内存，主备顺序，跳过停用/跨空间/悬挂 fallback）。
- `conversation`：`Create` 把快照 `prompt_id+env` 固化成版本写进 runtime config；`RunTurn` 租约 + 执行 + 同事务保存 + `run_completed`。
- `api/v1/chat.go`：`POST /conversations/:conversation_id/chat` SSE（逐事件 flush、`X-Accel-Buffering: no`）。
- 前端 `/conversations`：会话历史 + 聊天输入 + 流式回复 + 停止（abort）。

## 既有测试记录（一次性 PostgreSQL + Redis，race）

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

## 2026-11-24 接回时的检查点

固定参考见 [基线说明](../reference/BASELINE.md)，当前范围按 [G06 路线图](ROADMAP.md) 和 [ADR-0004](../architecture/adr/0004-feature-groups-and-runtime-scope.md)。本次重跑四个相关包的定向测试。命令退出成功，未配置隔离 PG 测试 URL，涉及 PG 的用例未运行；engine 还没有包级测试。上面的真实模型记录对应一次成功的 HTTP/SSE 单轮。

| 能力 | 当前证据 | 待验收 |
| --- | --- | --- |
| 多轮历史 | `RunTurn` 未读历史，执行器只构造本轮 user 消息。 | 第二轮模型请求包含第一轮两条消息和本轮输入，按序且各一次；刷新可回读。 |
| 首轮用户模板 | 绑定版本后每轮都渲染；当前落库是原始输入。普通 Web 输入也序列化了空 vars。 | 显式首轮模板、补充文本、缺变量校验、所用版本/渲染结果快照、后续自然语言轮次及历史一致。 |
| 租约续期 | SQLC 有续期查询，`RunTurn` 只申请 60 秒租约，缺少续期调度。 | 超过初始窗口仍排斥并发轮次；失败/取消停止续期且清理；旧持有者不能提交。 |
| 统一 ADK 和同步 | 当前执行器直接消费 Gateway 流，尚无 ADK；模型层的 Generate 只提供模型接口。 | 无工具聊天也走统一运行核心；同步聚合复用同一次执行的事件/提交语义。 |

## 首轮模板桌面验收

89 个 Web 测试与生产构建通过。桌面 Chrome 在隔离环境完成 Agent 绑定、缺变量反馈、手动指定 v2、SSE 完成后展示渲染文本、后续普通消息省略模板参数、刷新读回四条消息与首轮快照。会话由测试夹具调用正式 API 创建。后端为含完整快照候选的临时副本，模型为受控替身。

正式工作区复核通过。会话、Prompt、API、LLM 运行 race 测试，服务端只读模块构建成功。用刚保存的 Go 源码复跑桌面，临时启动入口仅替换模型 Gateway，数据库、鉴权、SSE 与消息事务走项目实现。默认选择保留实验来源，显式选择清除归因，失效租约使事务回滚。


## Eino 提示词渲染对齐

固定依赖与 Final 一致，使用 Eino v0.9.15。保存期校验由 Go 模板解析器执行，运行期由 Eino ChatTemplate.Format 生成单条用户消息并返回 Content。回调回归在旧路径失败，在 Eino 路径通过。隔离 PostgreSQL/Redis 的 make check 和桌面 Chrome 首轮模板流程通过，涵盖合法渲染、必填变量错误、显式选择 v2、后续会话与刷新回读快照。桌面使用受控模型替身。


## G06 收尾验收

- 使用一次性 PostgreSQL 和 Redis 运行 `make check`，文档、Go 依赖一致性、全量 race、vet、服务与迁移构建、前端 89 个测试及生产构建通过。
- Go 回归覆盖长轮生成和保存期间续租、续租失败取消和释放、失效 token 禁止提交、同步聚合、事件携带 Agent 版本和 turn revision、并发 409、SSE 写入时限和 flush 错误、模型提前断流与 HTTP 取消。
- 桌面 Chrome 使用正式服务入口、真实 Gateway 和本机 OpenAI 兼容 HTTP 模型替身。主部署返回 503 后切换备用部署，已验证 Agent 表单选择模型方案、从版本创建会话、首轮变量与指定 v2、多轮历史回喂、真实 SSE 终态和用量、断流、并发冲突、点击停止与刷新回读。断流及取消均未写入历史。本轮模型验证未调用付费端点。
- 独立 AI 评审发现保存期间提前停止续租、SSE 写入可能无限阻塞，两项已修复并补定向回归。PR Checks 待本组分支推送后确认。

## 聊天入口调整

- 主导航「聊天」提供 Agent 与版本选择、发送、停止和继续个人会话。「会话历史」只展示已经保存的消息。Agent 版本的「前往聊天」会预选该版本，用户确认开始时才创建会话。
- 桌面 Chrome 使用临时 PostgreSQL、Redis 与本机模型替身，验证主导航新建、Agent 预选、流式回复、取消、刷新、从侧栏续聊、历史只读，以及长回答在消息区内滚动时输入区保持可见。
- `make web-check` 的 89 个测试、TypeScript 检查与 Vite 生产构建通过。

## 会话删除

- 回归覆盖未认证、viewer、同空间其他用户、空间 owner 删除他人记录、跨空间、无效 UUID、有效租约冲突、过期租约清理、删除后读取及重复删除。SQL 确认关联消息与工具调用均被级联删除。
- 桌面 Chrome 在一次性 PostgreSQL、Redis 和本机模型替身上验证取消确认、生成期间返回 409、停止后确认删除、刷新聊天与历史列表，以及旧会话链接失效。
- 完整 `make check` 通过，包括隔离数据库上的 Go race 检查、文档和 89 个前端测试。

## 版本页环境标签

- Agent 和提示词共用版本环境标签，首次加载和发布后根据后端返回的环境绑定，在对应版本行显示 `dev`、`staging`、`prod`。Agent 快照按需展开，提示词发布和回滚从版本行发起，发布记录和实验入口保留。
- 桌面 Chrome 联调使用一次性 PostgreSQL 和 Redis。Agent 发布到 staging 后 dev 仍在 v1，再发布到 dev 后两个标签均位于 v2，刷新一致。提示词发布到 dev/staging 后回滚 dev，标签和 API 均确认 dev 回到 v1、staging 留在 v2，发布记录显示回滚。
- `make web-check` 的 89 个测试、TypeScript 检查与 Web 生产构建通过。

## 供应商试跑与配置清理

- 隔离 PostgreSQL 的测试覆盖供应商权限与跨空间、一次非流式调用、余额不足、禁用重定向、错误清理，以及有部署时阻止删除和空账户删除。
- 提示词软删除回归覆盖已发布版本、Agent 快照、灰度变体、并发场景迟到引用、删除后从版本列表隐藏及编号不复用。迁移 000011 在一次性数据库应用。
- 桌面 Chrome 使用本机 HTTP 模型替身，验证试跑成功、余额错误、已关联账户保护、删除空账户及删除草稿。全量 `make check` 通过。

## 删除 Agent

- HTTP / PostgreSQL 回归覆盖未认证、viewer/editor、跨空间、无效 UUID、两个版本和不同用户的关联会话、运行中冲突及事务完整性。SQL 确认 Agent 版本、环境、会话、消息和工具记录清理，验证其他 Agent、会话和共享提示词仍可读。
- 桌面 Chrome 使用一次性 PostgreSQL、Redis 与本机模型替身，验证取消确认、生成中拒绝、停止后删除和历史列表更新。全量 `make check` 通过。

## PR #18 复审修复

- SSE 断连回归先等待测试执行器进入可取消状态，再关闭 HTTP 请求；用隔离 PostgreSQL、Redis 开启 race 连续运行 5 次通过。
- Prompt 多变体实验未命中时，回落环境绑定版本并记录 `baseline`。新增回归覆盖两个变体合计只分配 2% 流量的场景。
- 桌面 Chrome 使用隔离 PostgreSQL、Redis 和本机模型替身，验证 Viewer 可回读历史，聊天写操作入口隐藏；同一账号切回 Member 后从 Agent 版本发起会话并收到回复。
- 隔离 PostgreSQL 和 Redis 上的完整 `make check` 通过，包含 Go race、vet、构建和 89 个前端测试。远程 CI 和复审在修复推送后核对。
