# 当前交接状态

核对日期：2026-11-19。以后接手应先核对实物再更新。此文件记录学习和工作进度；正式合并记录另见 [交付状态](../delivery/STATUS.md)。

- 项目：AsterFlow（GitHub yz1qaq/asterflow）；Go、Gin、PostgreSQL/SQLC、React。kbot 只读，固定基线和识别方法见 [交接入口](START-HERE.md)。
- 上组：G03 模型账户、部署计价、方案版本与环境绑定由 PR #14 已合并，Squash 为 `b85be3818a44d26180c7efd10da08f33b3679915`。实际 PR head 的 Documentation、Go、Web 与独立 AI 复审通过。确认过首次 PR Go 失败是 PG 与 JSON 的时区对象差异，测试统一 UTC 后通过；不要误认成版本持久化错误。
- 上组：G04 提示词治理由 PR #15 合并，Squash `db1422bb665e4cb84eecd4529cd6178eb71b457d`；head `d0f15a4` 的 docs/go/web 全部通过，本地整组验收见 [G04 记录](../delivery/G04-VERIFICATION.md)。
- 上组：G05 Agent 与会话由 PR #16 合并，Squash `82143d3dda4c46c0b1d891d92c4d5e8f7a0e2de0`；head `6eba0fb` 的 docs/go/web 通过。
- 当前：`feat/ENG-014-015-chat-loop`，基于上述 G05 Squash。G06 进行中（ENG-014/015、FR-06/Gateway）。Gateway/执行/SSE/聊天 Web 代码与测试已完成，全量门禁通过；真实模型端到端（DeepSeek）已通过。
- Go 命令沿用项目 Makefile；本机曾用 `env -u GOROOT GOTOOLCHAIN=go1.26.6 go ...` 规避工具链冲突。新设备按当前 go.mod 和 Makefile 核对，不改全局环境。一键联调：`make up`（加载 .env、启动 PG+Redis、应用迁移、同时启动后端与前端，Ctrl-C 停止两个应用）。

| 范围 | 事实 |
| --- | --- |
| 核心数据库 | 已保存 000006 up/down、共享 GenerationConfig、Prompt／PromptVersion／EnvBinding 与 SQLC 生成文件；隔离 PG/race 验证迁移、复合外键、原样回读及回退重建。开发库只读实查 `version=5 dirty=false`，未应用 000006。 |
| Store | 已保存并通过正式隔离 PG/race：Prompt/v1/dev 原子创建、两步失败回滚、按空间读、并发追加 v2–v7、旧版本和 dev 指针保留。测试在 `internal/platform/prompt/postgres_store_test.go`。 |
| 模板 | `internal/runtime/promptcache/compile.go` 已保存、正式 race 单测通过。Compile 解析 Go 模板及 required；Render 用 vars 得到文字，不调用模型。0/false 是有效值。 |
| 模型引用校验 | 学员已保存 `modelconfig.Service.ValidateProfileVersion`。先查版本再查父 Profile 的空间；模型版本可不填；跨空间/不存在同样返回对应未找到。 |
| Service/HTTP | 初批 Service、五个 Handler、工作空间路由及启动装配均已保存。`internal/api/prompt_test.go`、`internal/platform/prompt/service_test.go` 已接入；全量 Go race 在一次性 PostgreSQL 16 上通过，覆盖权限、跨空间、两个版本预览及 dev 指针未移动。 |
| Web | React `/prompts` 已接上导航及真实 API，支持列表、创建、版本历史、追加、指定版本预览、环境指针卡片和发布所选版本到环境；本批未接模型方案版本选择和生成参数表单。`make web-check`（77 个测试与生产构建）通过；Chromium 桌面验收通过：保存 v1/v2、同一变量渲染、缺变量提示、刷新回读；发布后 dev 从 v1 切到 v2，staging/prod 仍未发布，独立 SQL 复核一致。 |
| Web 权限复审 | 独立 AI 复审发现：读请求收到 403 后页面仍显示旧角色及操作入口；后端已正确拒绝。现已修复：Prompt 读写和预览收到 403 时先隐藏旧页面内容、重查 `/context`。重新确认后，列表和版本历史都等新请求完成再显示内容及操作，不提前恢复旧缓存。6 个 Chromium 拦截响应回归通过（含撤权、降级、同角色、延迟后再次 403）；正常桌面流程在真实临时 PG 上复跑通过。 |
| 环境 Store | 学员已保存 `GetPromptEnv` / SQLC 生成文件 / `GetEnvBinding` / `SetEnvBinding`。正式 `TestPromptEnvBinding` 与 Prompt 包其他用例在一次性 PostgreSQL 的隔离 schema/race 通过，覆盖读取、空间隔离、dev 从 v1 切到 v2、其他环境与已有版本不变。 |
| 环境 Service/HTTP | 学员已保存 `GetEnvBinding` / `Promote` / `RenderByEnv`、`POST /prompts/:prompt_id/promote` 及兼容 `{version_id}` 的环境渲染。助手扩展 `TestPromptHTTP`；`make build`、`go vet` 通过，Prompt、API、cache 在一次性 PostgreSQL 隔离 schema/race 全部通过。 |
| 回滚 | 学员已保存 `Service.Rollback`（委托 `Promote`）、`POST /prompts/:prompt_id/rollback` 与路由。助手补 HTTP 回归（v2→v1、staging 不动、跨空间/跨 Prompt 404、viewer 403、非法 UUID 400）与页面「回滚到 vN」入口；`make web-check` 78 个测试与生产构建通过，Chromium + 临时 PG 实测发布后回滚成功，独立 SQL 复核 dev 回到 v1、staging 仍未绑定。 |
| 环境列表带练 | `ListPromptEnvs` 查询、Store 与 `Service.ListEnvBindings` 候选在临时副本通过 SQLC 生成、编译、vet 和隔离真实 PG/race（按 env 排序、不可见 Prompt 返回未找到、非法输入被拒）；正式分支未写入，等学员保存后接 Gin 与页面。 |
| diff | 学员已保存 `GetPromptVersionByNumber`、`UnifiedDiff`、`Service.Diff`、`GET /prompts/:prompt_id/diff`。助手补 HTTP 回归（正向/反向、Schema 分段、跨空间 404、缺参/非整数/零版本 400）与页面版本对比（起止版本选择、`-`/`+` 着色）；`make web-check` 79 个测试与生产构建通过，Chromium + 临时 PG 实测 v1→v2 diff 正确显示。 |
| 实验表 | 学员已保存迁移 `000007`、domain 类型与三条查询。助手验证 SQLC/编译通过，迁移约束在隔离 PG/race 通过。 |
| 实验 Store | 学员已保存 `LockPromptExperiment`、`GetActiveExperiment`/`UpsertExperiment`/`AppendRolloutEvent`，`make build` 通过。 |
| 完成上线 | 学员已保存 `MarkPromptExperimentCompleted`、`rolloutEventParams` 与 `CompleteRollout`。助手验证 SQLC/build/vet 与隔离 PG/race。 |
| 分桶解析 | 学员已保存 `selectExperimentVersion`、`ResolveVersion`、`RenderByEnv` userID 与 Handler 调用；助手验证 build/vet 与隔离 PG/race。 |
| 实验 Service/HTTP | 学员已保存实验 Service 与五个 Gin Handler/路由。助手扩展 `TestPromptHTTP`：启动灰度（含 403/400/404/409 与分桶渲染）、调整 50%、完成上线后指针与全员渲染、二次灰度回滚保留指针、多变体启动/拒绝调整/停止且指针不动；隔离 PG/race 通过。 |
| 实验页面 | `/prompts` 的「实验与灰度」面板已联调：环境切换、启动灰度、调整比例、完成上线、停止实验、启动多变体实验（最多 5 个变体，校验必选/去重/合计 1–100）；`make web-check` 83 个测试与生产构建通过。多变体启动/停止会写 started(0→合计流量) 与 rolled_back(合计→0) 事件并带 multi-variant detail；卡片与时间线显示「启动实验」标签及 detail。真实 Chromium + 临时 PG16 验证 20%→50%→完成（指针 v2）、再启动 v1 10%→停止、再启动 v1/v2 50/50 多变体（0%→100%）→停止（100%→0%），SQL 复核 completed=1、rolled_back=2、事件 7 条，指针始终 v2。 |
| 事件时间线 | `GET /prompts/:prompt_id/experiments/:experiment_id/events` 已保存并接入页面。助手补 HTTP 回归（3 条事件顺序/流量、完成态可读、跨空间与未知实验 404、非法 UUID 400）；真实 Chromium + 临时 PG 验证启动/调整/完成/停止后时间线依次显示，完成后保留「最近一次实验」并可继续启动新灰度；`make web-check` 82 个测试与生产构建通过，`make docs-check`、`git diff --check` 通过。 |
| 事件读取带练 | `GetPromptExperiment`、`ListPromptRolloutEvents` 查询、Store 的 `GetExperiment`/`ListRolloutEvents` 与 `Service.ListRolloutEvents` 候选在临时副本通过 SQLC、build/vet、隔离 PG/race（3 条事件有序、完成态实验仍可读、跨空间/未知实验未找到、非法 UUID 拒绝）；正式未写入。 |
| 环境发布事件 | 迁移 `000008`、domain 类型、查询与 `PublishEnv` 已保存；`Promote`/`Rollback` 改为带 actor 的 `publishEnv`（published/rolled_back），Handler 传当前用户。助手修正 Rollback Handler 的语法错误并补 HTTP 断言：dev 发布 v1→v2、回滚 v2→v1，staging 首次发布 from=NULL，actor 为 owner；Prompt/API/cache 隔离 PG/race 通过。 |
| 环境事件读取 | 学员已保存读取路径与完成上线的 env 事件（published、detail `rollout completed`，同事务）。助手补 HTTP 断言（dev 最新在前、staging 首次发布、完成后第三条事件、跨空间 404、非法/空环境 400）与页面「发布记录」（环境选择、最新在前、首次发布、操作人、时间），真实 Chromium + 临时 PG 验证发布/回滚/完成上线三条记录；`make web-check` 84 个测试与生产构建通过，SQL 复核 3 条 env 事件。 |
| Redis 跨进程失效 | `promptcache.Cache`、Redis 发布/订阅、`NewServiceWithCache`、缓存命中与发布/回滚/完成广播、`config.RedisURL`、main 装配均已保存。正式测试含真实 Redis 的订阅失效与双进程发布后 B 读取新版本；全量 Go race 在一次性 PG+Redis 通过。 |
| G04 交付 | PR #15 已合并；本地全量门禁、桌面与双进程验收通过，远程 docs/go/web 通过。 |
| Agent 核心 | 迁移 000009（snapshot 用 TEXT + `pg_input_is_valid`）、`domain.Agent/AgentVersion`、`queries/agent.sql` 与 SQLC、Store 全部方法（创建/追加/读取/环境发布）已保存。正式 `postgres_store_test.go` 在隔离 PG/race 通过：原子创建、锁父行递增、并发 v2–v5、跨空间、复合外键、快照原样、未绑定环境与发布指针。 |
| Agent Service/Web | Service（快照构建与 Prompt/模型引用校验、创建/追加/发布/读取）、7 个 Gin 路由、main 装配与 `/agents` 管理页已保存。正式 `TestAgentServiceSnapshotAndPublish` 与 `TestAgentHTTP` 通过；Chromium 实测创建 v1、追加 v2、发布 dev v2、环境卡片与快照显示正确，SQL 复核 agent_versions=2、dev=v2。 |
| 会话与历史 | 迁移 000010、会话 Store（租约 Claim/Renew/Commit/Release）、Service/HTTP（用户私有、owner/admin 全量、409/404 语义）与 `/conversations` 历史页已完成。正式 Store/HTTP 测试与桌面验收通过。 |
| G05 交付 | PR #16 已合并；正式测试、桌面与 SQL 复核通过。 |
| G06 聊天闭环 | `runtime/llm`（Gateway/failover/OpenAIClient）、`runtime/engine`（事件/执行器，只读 runtime config）、`modelconfig.ResolveProfile`（解密部署）、会话创建固化版本、`RunTurn`（租约+提交）、SSE Handler 与聊天页均已完成。真实模型端到端（DeepSeek）已通过：SSE 事件序列、流式 delta、usage、消息持久化、租约清理均正确。 |

核验：G06 分支 `go build`、`go vet`、`make docs-check`、`git diff --check`、`make deps-check`、`make web-check`（87 测试）通过；全量 `go test -race ./...` 在一次性 PostgreSQL+Redis 通过。真实模型调用经负责人授权后对 DeepSeek 完成。未运行远程 CI。

学习接续：G06 代码与测试已完成，按 [NEXT](NEXT.md) 收尾（受控替身模型桌面验收 → 真实模型授权 → 提交 PR）。会话固定 agent_version_id；聊天读 runtime_config_json；发布新 Prompt 只影响新会话。
