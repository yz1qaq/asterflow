# G07 业务工具验收

2026-11-26，`feat/ENG-016-018-tools` 的工作区，基线 `58fee9b`。范围为 ENG-016 至 018，覆盖 FR-04 的 REST / internal SDK、FR-07 的 Agent 工具闭环，以及 FR-17 的独立跨境订单与库存查询。

| 验收 | 证据 |
| --- | --- |
| 工具控制面 | 隔离 PostgreSQL/HTTP race 覆盖版本并发、试跑与发布按同一版本归属、空间与凭据边界。桌面验证版本管理、成功及失败试跑、发布与 Agent 快照绑定。 |
| 执行器 | Schema 校验、REST 出站策略、internal SDK 分发和固定版本构建由现有测试覆盖。 |
| 双工具 | 从当前工作区构建正式入口，桌面以同一轮查询订单与库存。Crossborder 通过真实 HTTP 和独立 PostgreSQL 返回结果，本地 OpenAI 兼容模型替身验证两种 Schema、工具返回内容和各自的调用 ID。 |
| 事件与历史 | SSE 顺序、工具结果状态、模型答复、各自的工具版本 UUID 与会话消息已验证。第二轮模型输入含第一轮两条工具结果；第二轮缺参显示失败，刷新仍可展开第一轮工具详情。Go 测试复查数据库调用行，包括 UUID、原始参数、JSONB、耗时与错误。 |
| 开发门禁 | 平台与 Crossborder 两个 Go 模块均使用隔离 PostgreSQL 跑完 `go test -race -count=1 ./...`。平台测试另使用一次性 Redis。前端交付范围含 89 项测试，TypeScript 和 Vite 构建通过。Crossborder 已统一模块命名，其依赖、竞态测试、vet 和构建已在独立测试库复验。 |

第 4 天学习验收通过。已在 Go 工作流中配置独立 Crossborder 集成数据库与模块检查，远程执行结果待 PR 验证。桌面验收使用本地模型替身。

代码沙箱归 G08，MCP/A2A 归 G09，有业务副作用的工具与审批归 G12。固定 Final 的数据库消息结构保存工具调用、结果和正文。供应商专用思考字段在同一轮模型调用间传递，数据库暂未保存该字段。

## 提交前独立 AI 预审

- 并发追加的配置与凭据继承需要验证基础版本。隔离数据库确定性复现旧实现返回 201，写入了带旧凭据的 v3。正式工作区的 PostgreSQL HTTP 回归确认落后请求返回 409 且未追加记录，随后重试继承已清空凭据的 v2。
- [未知工具回退](../handoff/next/G07-unknown-tool-fallback.md)正式工作区通过 Engine、会话、HTTP race 与隔离 PostgreSQL 桌面刷新。[主记录与版本 UUID 联合入库](../handoff/next/G07-tool-call-identity.md)正式源码通过 PostgreSQL race 与桌面回读。已知工具保存两个 UUID，未知工具保留失败原因。
- 固定 Final 对工具运行的最终正文聚合后统一发送，G07 保留该语义。无工具路径现有正文 delta 流式行为保留。固定 Final 唯一内置 SDK 实现是知识库检索，按 G10 接入默认服务。当前 Registry 的 internal SDK 分发覆盖测试，普通服务可试跑的已落地业务来源是 REST。

独立 AI 复核已核对上述三项修复，在当前工作区未发现新的阻断问题。版本冲突检查依据服务端读到的版本。表单在请求发出前已过期时，用户填写的字段仍视为显式配置。PR 评审与远程 CI 以提交的 SHA 为准。

## PR #19 提交复审

[PR #19](https://github.com/yz1qaq/asterflow/pull/19) 的提交 `7c354227bfefd2ec22a02e6d7887c3419b57e5e6`。远程 [Documentation](https://github.com/yz1qaq/asterflow/actions/runs/45832915068)、[Go](https://github.com/yz1qaq/asterflow/actions/runs/45832915117) 和 [Web](https://github.com/yz1qaq/asterflow/actions/runs/45832914968) 全部通过，Go 任务的独立 Crossborder 建库和模块检查通过。

- P2 工具的 EndpointConfig 可包含查询或片段，版本列表只清理 AuthConfig。端点中的查询凭据会随公开版本配置返回，违反 FR-04 的凭据不回显要求。[端点配置回显修复](../handoff/next/G07-endpoint-config-privacy.md)已保存，正式工作区通过隔离 PostgreSQL 上的本地门禁和桌面复验。
- P2 Eino 适配器返回的 HTTP 请求中缺少显式的 top_p=0 和 max_tokens=0。用本地模型替身对照旧客户端，同步和流式都复现，temperature=0 与 seed=0 正常。本地修复通过 Eino ExtraFields 保留显式零值。TestEinoOpenAIPreservesGenerationValues 验证同步与流式的省略、零值和非零值。隔离 PostgreSQL 的 make check 与桌面工具链路通过，Agent 快照中的零值按原值进入模型请求。

独立 AI 复审在版本固定、Crossborder 业务查询及正常工具历史未发现新的合并阻断。两项修复在本地通过验收。推送后按新提交验证 CI 与复审。

## 修复提交复审

[PR #19](https://github.com/yz1qaq/asterflow/pull/19) 的 `8ea9b6a460b7d7d6db0bd0eb1204a0d87256db9e` 已通过 [Documentation](https://github.com/yz1qaq/asterflow/actions/runs/45839160560)、[Go](https://github.com/yz1qaq/asterflow/actions/runs/45839160581) 和 [Web](https://github.com/yz1qaq/asterflow/actions/runs/45839160531)，Go 含 Crossborder 模块检查。正式提交的端点、REST、Eino 定向 race 通过，独立复审确认模型零值问题关闭。

端点仍有一项 P2。JSON 解码为 map 会折叠同名 URL 字段，规则检查的是折叠后的值，版本接口仍返回原始文本。公开响应应只使用检查过的配置，写入应识别重复 URL 字段，旧版本继续按原解析规则执行。附带检查确认格式错误的上游跳转地址仍可能出现在试跑错误里，应稳定映射对外错误。复审未运行真实凭据或外部网络请求。修复并复验前保留 Draft。

[端点重复字段与试跑错误](../handoff/next/G07-tool-endpoint-review.md)已按负责人授权写入正式工作区。原实现未通过新增回归。修复后正式工作区在独立 PostgreSQL/Redis 上完成 make check，Go race、vet、构建、React 93 项测试及生产构建通过。用正式 Go 文件构建的后端与桌面 Chrome 连接隔离 Crossborder，验证隐藏旧配置、原版本仍能执行、必须重新填写端点以及 v2 保存。回归覆盖 Viewer 读取、登记和追加返回 400、试跑网络错误输出、同源策略和取消。新提交 CI 和独立复审待推送。
