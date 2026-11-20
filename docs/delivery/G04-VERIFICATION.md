# G04 验收记录

## 整组验收（2026-11-20）

本地按 [NEXT](../handoff/NEXT.md) 清单逐项执行，全部通过：

| 命令/检查 | 结果 |
| --- | --- |
| `make deps-check` | 通过（go.mod/go.sum 一致） |
| `make build`、`go vet ./...` | 通过 |
| `make docs-check`、`git diff --check` | 通过 |
| `make web-check` | 7 个测试文件、84 个测试、tsc + 生产构建通过（既有约 563 kB chunk 警告） |
| `TZ=UTC go test -race ./...`，显式 `ASTERFLOW_TEST_DATABASE_URL` + `ASTERFLOW_TEST_REDIS_URL` | 全部包通过；使用一次性 PostgreSQL 与一次性 Redis，每测试独立 schema |
| 桌面链路（Chromium 1440×1000、正式二进制、一次性 PG16） | 版本预览/缺变量、发布/回滚、diff、灰度 20%→50%→完成、多变体、发布记录、刷新回读全部通过 |
| SQL 复核（桌面后） | `dev=v2`、completed=1、rolled_back=2、rollout 事件 7、env 事件 3 |
| 双进程（共享 DB+Redis） | B 渲染 v1 → A 发布 v2 → B 下一次请求渲染 v2 |

边界：远程 CI 尚未运行，以 PR Checks 为准；本机验证不替代远程结果。开发库仅由学员本地应用到 `version=8`，测试未使用开发库。


## 核心表与类型

G04 分支基于 PR #14 的 Squash 提交 b85be38，开发库只读查询为 version=5 dirty=false。负责人已保存 000006 up/down、Prompt 类型及共享 GenerationConfig。已通过 TestPromptCoreMigration 的真实 PostgreSQL/race 验证：默认值、变量 Schema 原样回读、版本序号约束、环境独立性、不同 Prompt 的版本不能交叉绑定、删除级联和回退重建。验证只使用随机 schema。

当前仅验证数据库结构和 Go 类型。首次创建事务、API、前端、实验灰度、Redis 等按 [G04 开发任务](../tasks/G04-prompt-governance.md) 继续验收。

## 首次创建事务

负责人已保存 CreatePrompt、CreatePromptVersion、UpsertPromptEnv 及 Store 原子写入。正式 TestCreatePromptWithInitialVersion 与 TestPromptCoreMigration 在 UTC/真实隔离 PostgreSQL/race 通过。显式零温度、可选模型版本、默认 dev 指向，以及版本插入失败／环境绑定失败不留部分记录均已验证。

下一批查询与追加暂只在临时副本验证，未当作正式 Store 完成证据。验证涵盖空间读写隔离、6 个并发追加得到 v2–v7、旧版本及 dev 指针不变、插入失败不跳号、锁等待取消且不改草稿。

## 读取与追加版本

负责人已保存 workspace 范围内的 Prompt／版本读取和追加。正式 TestPromptReadAndAppend 在 UTC/真实隔离 PostgreSQL/race 通过：6 个并发请求得到连续的 v2–v7，原 v1 和 dev 绑定保留，跨空间查询及追加被隔离，锁等待超时后草稿不被写回。本组现有三个 Store 用例全部通过。

模板编译、必填变量校验、渲染和 Token 估算已在临时副本通过单测；本分支已准备对应 compile_test.go，待负责人写入 compile.go 后运行正式测试。

## 编译正式验证与 Service 示例

负责人保存 compile.go，正式 promptcache 单测（race）通过。覆盖模板语法、变量规则格式、必填键缺失、零值、已有请求取消和 Token 估算。Prompt 创建、版本追加与指定版本预览的 Service 示例在临时副本通过真实 PostgreSQL/race，含模型版本空间隔离。

## 正式 Service 与 Gin 路由

负责人已保存 Service、五个 Gin Handler、路由及启动装配。助手将 `TestPromptCreateAndRenderService`、`TestPromptHTTP` 接到正式包，并修正旧 `NewRouter` 测试调用。`make build`、`go vet ./...` 通过；`TZ=UTC make test` 使用显式 `ASTERFLOW_TEST_DATABASE_URL` 和一次性 PostgreSQL 16 容器通过（各集成测试仍使用独立 schema）。覆盖空列表、创建 v1、追加 v2、版本历史及两个版本的渲染、缺变量、登录与角色限制、Prompt 和模型方案的空间边界，以及追加后 `dev` 仍指向 v1。不使用开发库，不调用模型。

## 学习第 1 天：真实 React 桌面链路

`make web-check` 通过：7 个测试文件、75 个测试，TypeScript/Vite 生产构建成功；原有约 563 kB 共享 chunk 警告仍在。使用 Chromium（1440×1000）、Vite 代理、实际 Go 二进制及单独的临时 PG16 容器；临时库应用正式迁移，使用临时管理员。实测登录、创建工作空间，再在提示词管理页创建「订单核验」v1、追加 v2。用 `ORD-1001` 分别得到「核验订单 ORD-1001」和「跟踪订单 ORD-1001」，提交 `{}` 显示缺少 `order_id`；刷新后 v1、v2 均可重读。独立 SQL 复查 `dev` 仍指向 v1。开发库未迁移，未调用模型，未做远程 CI、发布或整组验收。

## 独立 AI 复审：空间权限变化

复审确认 Gin 正确返回 403、撤权请求未到达 Prompt Store；但 React 在版本历史读取返回 403 后仍显示旧角色、提示词名称与新建入口。修复后，列表、历史、保存和预览请求遇到 403 都会隐藏旧内容及操作入口，再查 `/context`。受控 Chromium 回归拦截 API 响应，验证了列表和历史撤权后清除当前空间、保存和预览降为 `viewer` 后保留只读、以及角色未变时不循环重试，可手动重新确认。后续独立复审确认 `/context` 成功后 React Query 仍可能在新列表返回前显示旧数据；已让列表和历史在每次挂载后等待本次请求结果。新增 Chromium 回归先在旧构建复现，再在新构建验证：历史 403 后，手动重新确认返回 `owner`，列表延迟并再次返回 403，期间不显示旧提示词或启用新建；再次确认且新列表成功后才恢复。6 个 Chromium 权限场景全部通过。`make web-check`（75 个测试及生产构建）通过；另在真实 Go 和一次性 PG16 上复跑正常桌面链路通过，独立 SQL 再次确认 `dev` 仍指向 v1。撤权回归本身不是数据库联调；`make docs-check` 仍因 `internal/platform/prompt/service.go:219` 末尾空行未通过。

## Prompt 缓存与 Redis 失效

负责人已保存 `promptcache.Cache`、Redis 发布/订阅适配与 Service 接线（`NewServiceWithCache`、`RenderByEnv` 命中缓存、发布/回滚/完成上线后 `cacheVersion` 刷新并广播；实验启停不广播）。助手新增正式测试：缓存按 prompt@env、频道解析、真实 Redis 订阅失效；`TestPromptServiceCache` 验证基线渲染填充缓存、发布/回滚/完成刷新缓存与广播、绕过 Service 改写版本后仍命中缓存、本地失效后回读数据库；`TestPromptCacheInvalidationThroughRedis` 验证进程 A 发布后进程 B 的缓存被清空。`promptcache`、`prompt`、`api` 在一次性 PostgreSQL + 一次性 Redis 上 race 全部通过。`config.RedisURL`（默认 `redis://127.0.0.1:25433/0`，校验 scheme/host）与 `main` 装配已保存：连接失败即启动失败，订阅 goroutine 随进程退出。`go mod tidy` 后 go-redis 为直接依赖。全量 `go test -race ./...` 在一次性 PostgreSQL + Redis 上通过（含 config/server）。双进程验收：同一 DB/Redis 启动 18101/18102 两个服务；登录、建空间与 Prompt v1/v2 后，B 渲染 dev 得 v1；A 发布 v2，B 的下一次请求渲染 v2。`make web-check`（84 个测试）、`make docs-check`、`git diff --check` 通过。注意：远端进程的版本正确性由“解析结果与缓存版本比对 + 回 DB 解析”保证，Redis 失效负责尽早释放内存，而非正确性的唯一来源。

## Prompt 缓存与 Redis 失效候选（未写入正式分支）

仓库已加入 `github.com/redis/go-redis/v9`、compose `redis` 服务（仅 Pub/Sub）与 `ASTERFLOW_REDIS_URL`。临时副本增加 `promptcache.Cache`（prompt@env）、`RedisPublisher`、`Subscriber`（`prompt:*:invalidate` → 本地失效）。隔离测试：Cache 读写/按环境隔离/失效、频道解析、真实 Redis 的发布-订阅使另一本地缓存失效，均通过。Service 尚未使用缓存与广播；配置/启动装配未接。

## 页面分区优化

提示词详情页由单列长滚动改为三个标签页：「版本」（历史、版本内容/预览、版本对比）、「环境发布」（每个环境的指针卡片 + 发布到此环境 / 回滚 + 发布记录）、「实验灰度」（灰度/多变体实验与时间线）。发布入口同时支持从版本发起（已选版本、再选环境）和从环境发起（已选环境、再选版本），共用同一 Modal。桌面脚本与权限脚本更新为跨 tab 校验：viewer 各 tab 无写入口、owner 各 tab 写入口齐全、单版本仅发布可用；完整 Chromium + 临时 PG16 流程通过（预览、发布、回滚、diff、灰度完成、多变体、发布记录）。`make web-check` 84 个测试与生产构建、`make docs-check`、`git diff --check` 均通过。

## 环境事件读取与发布记录

负责人已保存读取路径，并补上完成上线的环境事件（`published`、detail `rollout completed`，与指针/实验完成同事务）。助手扩展 `TestPromptHTTP`：dev 两条事件最新在前（rolled_back v2→v1、published v1→v2，actor 为 owner）、staging 首次发布 from 为空、完成上线后新增第三条 `rollout completed`、跨空间 404、非法 Prompt 与空环境 400。React 在「环境指针」下增加「发布/回滚记录」（环境选择、最新在前、首次发布显示、操作人与时间）。`make web-check` 84 个测试与生产构建通过。真实 Chromium + 临时 PG16：发布 v2、回滚 v1 后记录显示「发布 v1→v2」「回滚 v2→v1」；灰度完成后新增「发布 v1→v2 · rollout completed」；独立 SQL 复核 dev 环境 3 条事件、其中 1 条 `rollout completed`。

## 环境事件读取候选（未写入正式分支）

临时副本增加 `envEventFromRow`、`ListPromptEnvEvents`、`Service.ListEnvEvents` 与 GET Handler/路由。build/vet 通过；隔离 PG/race 的 `TestPromptEnvEventsReadProbe` 验证最新在前、首次发布 from 为空、发布时间/回滚顺序、跨空间与未知 Prompt 未找到、非法 UUID 与空环境拒绝。正式未写入；HTTP 回归与页面时间线未接。

## 环境发布/回滚事件

迁移 `000008_prompt_env_events`、`PromptEnvEvent`、`PublishEnv` 与 Service 的 `publishEnv`/`Promote`/`Rollback` 已保存；Handler 从认证上下文传 actor。助手修正 Rollback Handler 的语法错误，并在 `TestPromptHTTP` 增加事件断言：dev `published` v1→v2、`rolled_back` v2→v1，staging 首次发布 `from=NULL`，actor 均为 owner；指针与事件同事务。`go build`/`vet`、Prompt/API/cache 在一次性 PostgreSQL/race 通过。读取接口与页面时间线未接；旧 `SetEnvBinding` 仅用于 Store 测试，生产路径不得再使用。

## 环境事件候选（未写入正式分支）

临时副本增加 `000008_prompt_env_events` up/down、`domain.PromptEnvEvent`、`InsertPromptEnvEvent`/`ListPromptEnvEvents`（DESC）与 `PostgresStore.PublishEnv`：同一事务内锁 Prompt、校验版本归属、读取当前指针、写入新指针与事件。SQLC、build/vet 通过；隔离 PG/race 的 `TestPromptEnvEventsProbe` 验证发布/回滚指针、首次发布无 from、事件字段与顺序、跨空间与跨 Prompt 版本拒绝、非法 action 拒绝且失败不动指针。正式未写入；Service/HTTP/页面未接。

## 多变体实验页面

实验空闲态新增「启动多变体实验」：默认两个变体，可选版本与百分比（0–100），可增删至最多 5 个；提交前校验必选版本、去重、合计 1–100。启动后卡片显示「多变体实验：v1 50%，v2 50%」，可停止但不可按灰度调整/完成。多变体启动与停止现在写入事件：started(0→合计) 与 rolled_back(合计→0)，detail 标注 multi-variant；时间线标签统一为「启动实验」并展示 detail。API 回归校验 30/30 实验的事件为 0→60 与 60→0。API 单测与 `make web-check`（83 个测试、生产构建）通过；真实 Chromium + 临时 PG16 启动 50/50（0%→100%）并停止（100%→0%），SQL 复核 rolled_back=2、事件 7 条、指针仍 v2。

## 事件时间线联调

`GET /prompts/:prompt_id/experiments/:experiment_id/events` 已保存。助手扩展 `TestPromptHTTP`：完成后的实验返回 3 条事件（started 0→20、traffic_changed 20→50、completed 50→100，含 actor/时间）、跨空间 404、未知实验 404、非法 UUID 400；`internal/api` 在一次性 PostgreSQL/race 通过。React 实验卡片内展示时间线，完成/停止后保留「最近一次实验」并可在下方继续启动新灰度。真实 Chromium + 临时 PG16：启动 20% 出现「启动灰度 0%→20%」；调整到 50% 追加「调整流量 20%→50%」；完成后卡片为「已完成」且出现「完成上线 50%→100%」，环境卡片变 v2；再启动 v1 10% 并停止，卡片「已停止」、事件「停止实验 10%→0%」，指针仍 v2。`make web-check` 82 个测试与生产构建通过，`make docs-check`、`git diff --check` 通过。发布/回滚操作记录、Redis 与整组验收未完成。

## 事件读取候选（未写入正式分支）

临时副本增加 `GetPromptExperiment`（workspace 限定、不带锁）、`ListPromptRolloutEvents`（按 created_at/id 排序）及 Store/Service 读取。SQLC、build/vet 通过；隔离 PG/race 的 `TestRolloutEventsProbe` 验证启动/调整/完成共 3 条事件顺序与流量字段、完成态实验仍可读取、跨空间与未知实验返回未找到、非法 UUID 拒绝。正式未写入；HTTP 时间线与发布/回滚操作记录未接。

## 实验页面联调

`GET /prompts/:prompt_id/experiments/active` 已保存；助手补充 HTTP 回归：可见 Prompt 无实验返回 `null`、跨空间 404、非法 UUID 400、启动后返回 active 详情、完成后再次为 `null`。`internal/platform/prompt`、`internal/api`、`internal/runtime/promptcache` 在一次性 PostgreSQL/race 全部通过。真实 Chromium（1440×1000）+ Vite 代理 + 正式二进制 + 临时 PG16：`dev` 发布 v2 回滚 v1 后，启动 v2 20% 灰度（环境卡片仍 v1）→ 调整到 50% → 完成上线（`dev` 卡片变 v2、实验消失）→ 再启动 v1 10% → 停止实验（指针仍 v2）。独立 SQL 确认 `dev=2`、completed=1、rolled_back=1、rollout 事件 5 条。`make web-check` 81 个测试与生产构建通过。未接事件查询、发布/回滚操作记录、Redis 和完整组验收。

## 实验 Service/HTTP 与页面（联调中）

负责人已保存实验 Service、五个 Handler 与路由。助手扩展 `TestPromptHTTP`：viewer 写入 403、非法流量/候选=基线 400、未绑定环境与跨空间 404、重复 active 409；启动 20% 后按 `crc32(userID+实验ID)` 校验 owner 的渲染结果；调整到 50%；完成上线后 dev 指针为 v2 且所有人渲染候选；再启动 v1 灰度后回滚，指针仍为 v2；多变体实验可启动、不能按灰度调整/完成、停止后指针不变。隔离 PostgreSQL/race 通过。React 增加「实验与灰度」面板（环境选择、启动灰度、调整比例、完成上线、停止实验）。`make web-check` 81 个测试与生产构建通过。面板依赖 `GET /prompts/:prompt_id/experiments/active`，该接口尚未保存，未做真实桌面联调。

## 实验 Service 候选（未写入正式分支）

临时副本增加 `StartExperiment`（变体必须属于本空间本 Prompt、流量合计 1–100）、`StartRollout`（环境必须已绑定、候选不同于基线、1–99）、`UpdateRolloutTraffic`（拒绝把多变体实验当灰度调整）、`CompleteRollout`（走 Store 事务）、`RollbackRollout`（允许停止任意 active，指针不动）。build/vet 通过；隔离 PG/race 的 `TestExperimentServiceProbe` 验证非法变体、单活跃冲突、未绑定环境、候选=基线、流量边界、分桶两侧稳定、调整到 50%、完成上线后全员候选且事件共 3 条、回滚保留指针、无 active 时完成报错。正式未写入；Gin 与页面未接。

## 分桶解析候选（未写入正式分支）

临时副本中 `ResolveVersion` 先查 active 实验，按 `crc32(userID+experimentID)%100` 稳定分桶，未命中回落环境指针；`RenderByEnv` 增加 `userID` 并走该解析。build/vet 与隔离 PG/race 的 `TestResolveExperimentProbe` 验证：无实验回落 dev；200 个用户重复解析稳定，候选/基线人数与 crc32 预期一致；候选用户渲染新模板；完成上线后所有用户走已移动的指针；rolled_back 实验不参与分桶、回落指针。正式未写入；Service 生命周期与 HTTP/页面未接。

## 完成上线候选（未写入正式分支）

临时副本增加 `MarkPromptExperimentCompleted` 查询，并把事件参数校验抽为 `rolloutEventParams`；`CompleteRollout` 在单事务内依次锁定 Prompt、按空间读取候选版本、锁定实验、把环境指针切到候选、将 active 实验标记 completed/100%、写入事件后提交。SQLC、build/vet 通过；隔离 PG/race 的 `TestCompleteRolloutProbe` 验证指针切换、完成状态与事件各一处、重复完成返回未找到且指针不变、跨空间返回未找到、重复事件 ID 使整个事务回滚（指针仍为旧版、实验仍 active）、旧版本内容不变。正式未写入；Service/HTTP/页面未接。

## 实验 Store 候选（未写入正式分支）

在实验表迁移基础上，临时副本增加 `LockPromptExperiment`、`GetActiveExperiment`（无实验返回 nil）、`UpsertExperiment`（先校验 Prompt 可见、参数校验、单活跃冲突映射为 `ErrActivePromptExperiment`）与 `AppendRolloutEvent`（先按 workspace 锁定实验，默认 UTC 时间）。SQLC 生成、build/vet 通过；隔离 PG/race 的 `TestExperimentStoreProbe` 覆盖 active 读取、跨空间读取/写入/事件拒绝、第二个 active 冲突、流量与变体越界、候选=基线、完成后可开新实验、时间戳回读。正式未写入；Service/HTTP/页面未接。

## 实验表候选（未写入正式分支）

临时副本增加 `000007_prompt_experiments` up/down、`domain.PromptExperiment` 系列与三条查询。SQLC 1.27.0 生成、`go build` 通过；隔离 PostgreSQL 的 `TestPromptExperimentMigrationProbe`（race）验证：JSONB 变量原样语义、同一 Prompt/环境第二个 active 被唯一索引拒绝、completed 可与新 active 共存、跨 Prompt 版本被复合外键拒绝、候选=基线与流量越界被约束拒绝、删除实验/删除 Prompt 级联事件、down/up 重建可再写入。正式迁移与 Go 待学员保存；Store/Service/HTTP/页面未接。

## Diff

负责人已保存 `GetPromptVersionByNumber`、`diff.go`、`Service.Diff`、`GET /prompts/:prompt_id/diff`。助手扩展 `TestPromptHTTP`：v1→v2 与反向 diff 文本准确、Schema 变化时附加 `--- variables_schema` 段、跨空间 404、缺失参数/非整数/0 版本 400。React 增加版本对比（起止版本选择，`-`/`+` 分色，只读角色可用）。`make web-check` 79 个测试与生产构建通过；Chromium + 临时 PG16 实测发布、回滚后 v1→v2 diff 正确显示。未接实验/灰度、操作记录或 Redis。

## Diff 候选（未写入正式分支）

临时副本增加 `GetPromptVersionByNumber`、Store、`internal/platform/prompt/diff.go` 与 `Service.Diff`。SQLC 1.27.0 生成、`go build` 通过；一次性 PostgreSQL 隔离 schema 的 `TestDiffCandidate`（race）验证：相同文本、单行替换、反向比较、Schema 变化时附加分段、缺失版本与跨空间返回未找到、0/负数版本在访问数据库前被拒。正式 SQL/Go 待学员保存；HTTP 与页面未接。

## 回滚

负责人已保存 `Service.Rollback`（与 `Promote` 共用指针写入）、`POST /prompts/:prompt_id/rollback` 与路由。助手扩展 `TestPromptHTTP`：viewer 403、非法 UUID 400、跨空间与跨 Prompt 404 且指针不动；dev v2 回滚 v1 返回 `{"status":"rolled_back"}`，staging 保持 v1，默认环境渲染回到旧模板，指定 v2 预览不受影响。React 环境卡片为 writable 角色提供「回滚到 vN」（同一 Prompt 小于当前绑定的最高版本）并二次确认。`make web-check` 78 个测试与生产构建通过；Chromium + 临时 PG16 + 正式二进制实测发布 v2 后再回滚，页面和刷新均为 v1、staging/prod 未发布；独立 SQL 确认 dev=1、staging 无行。未接 diff、实验/灰度、操作事件或 Redis。

## 环境指针列表与页面发布

负责人已保存 `ListPromptEnvs` 查询、SQLC、Store、`Service.ListEnvBindings`、`GET /prompts/:prompt_id/envs`。助手扩展 `TestPromptHTTP`：member 可读单条 dev 绑定、跨空间与不存在 Prompt 404、非法 UUID 400、发布后返回 dev v2 与 staging v1 的有序指针。`internal/api` 在一次性 PostgreSQL/race 通过。React 增加环境卡片（固定 dev/staging/prod，未绑定显示「未发布」）、发布所选版本对话框和 403 撤权复用；2 个新 API 单测通过，`make web-check` 77 个测试与生产构建通过。真实 Chromium + 临时 PG16 + 正式二进制验收：发布前 dev v1、staging/prod 未发布；发布 v2 到 dev 后卡片与刷新回读均为 v2，staging/prod 未发布；独立 SQL 确认 dev=2 且 staging 无行。未接回滚、diff、实验/灰度、操作记录或 Redis。

## 环境列表候选（未写入正式分支）

临时副本增加 `ListPromptEnvs` 查询、Store 与 `Service.ListEnvBindings`；SQLC 1.27.0 生成、`go build`/`vet` 通过。一次性 PostgreSQL 隔离 schema 的 `TestListEnvBindingsProbe`（race）验证 dev/prod/staging 排序正确、其他 Prompt 的绑定不混入、跨空间与不存在 Prompt 返回未找到、空工作空间与非法 UUID 在访问数据库前被拒。正式 SQL/Go 待学员保存；HTTP、页面与操作事件未接。

## 环境 Service 与 Gin

负责人已保存 `GetEnvBinding`、`Promote`、`RenderByEnv`、`POST /api/v1/prompts/:prompt_id/promote`，并让 `render` 同时接受 `{version_id, vars}` 与 `{env, vars}`（两者同时出现返回 400）。助手扩展 `TestPromptHTTP`：默认 dev、显式 dev/staging、prod 未绑定 404、跨空间 404、viewer 发布 403、无效 UUID 与空环境 400、失败发布不影响指针、staging 发布不动 dev、显式发布 dev 后预览切换为 v2 且 staging 仍为 v1、指定 v1 预览保持原结果。`make build`、`go vet ./...` 通过；Prompt、API、`api/v1`、`promptcache` 在一次性 PostgreSQL 隔离 schema/race 全部通过。未接环境列表、页面发布、Redis 或操作事件。

## 环境指针 Store 候选（未写入正式分支）

核对固定 Final 的 `GetEnvBinding`、`Promote` 和环境渲染契约。在临时副本用项目固定 SQLC 1.27.0 生成、Go 编译，并用一次性 PostgreSQL 的隔离 schema 运行候选 `TestPromptEnvBindingProbe`（race，PASS）。覆盖 `dev` v1→v2、首次绑定 staging、prod 未绑定、跨空间不可见、错误 UUID 对应的不存在版本和其他 Prompt 的版本被拒、失败后指针与更新时间未变、旧版本不变。独立 AI 复审未发现阻断：临时副本的 build、无 PG 的 Go race 回归、vet 及畸形 UUID 拒绝探针均通过；未重跑真实 PG。正式 SQL/Go 仍待学员写入；没有接 Service、HTTP、操作事件或 Redis。当时工作区另复跑 `make docs-check` 已通过。

## 正式环境 Store 与 Service 候选

学员已保存 SQL、SQLC 生成文件和 Store。新增正式 `TestPromptEnvBinding`，在一次性 PostgreSQL 隔离 schema 上与 Prompt 包其他用例一起通过 race；覆盖首次绑定、dev 切换、两个空间和不同 Prompt 的边界、失败无副作用、其他环境及旧版本不变。Service 候选仅在临时副本通过真实 PG/race（默认 dev、显式发布、staging 保持、未绑定 prod、错误参数与指定 v1 预览）；现有 Gin 包调用方可编译，未接新路由、不计正式 Service 或 Redis 完成。本轮 `make docs-check` 报 `internal/platform/modelconfig/postgres_store.go:152` 末尾空行。
