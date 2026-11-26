# G07 工具闭环交付

G06 初版和[补充 PR #18](https://github.com/yz1qaq/asterflow/pull/18) 均已合并。进入 G07 时先在固定版 Final 的独立环境演示对应功能，再核对实现路径，由学员编写 2–3 个紧密相关的 Go/SQL 改动。助手维护 React、验证、文档和评审。新增代码的调用关系和输入输出在对话中直接讲解。见[路线图](../delivery/ROADMAP.md)。

1. 工具注册、列表和版本查询已通过隔离 PostgreSQL race 与桌面浏览器验收。[追加版本的 SQL、Store 与装配](next/G07-tool-version-storage.md)已保存，隔离 PostgreSQL race 确认同一工具并发得到连续的 v2–v9。[追加版本的 Service 和 Gin](next/G07-tool-version-http.md)已保存并通过隔离 PostgreSQL HTTP race，React 的「新建版本」已通过桌面浏览器验收。[试跑记录的迁移、查询与领域类型](next/G07-tool-test-runs.md)已保存，隔离 PostgreSQL race 和回退测试通过。[试跑记录 Store 与 Service](next/G07-tool-test-store.md)已保存，正式隔离 PostgreSQL race 验证指定版本归属。[执行前加载工具版本配置](next/G07-tool-execution-config.md)已保存，隔离 PostgreSQL race 通过。[REST 执行器与受控 HTTP 客户端](next/G07-rest-executor.md)已保存并通过隔离 Crossborder 实测。[参数校验和 Runtime Registry](next/G07-tool-registry-runtime.md)已保存，正式隔离电商回归通过。[试跑 HTTP API](next/G07-tool-test-http.md)已保存，正式 HTTP race 与桌面 React 试跑通过。隔离 Crossborder 与 PostgreSQL 回读证明成功和失败按实际版本保存。[发布指定工具版本](next/G07-tool-publish.md)已保存，隔离 PostgreSQL HTTP race 与桌面 React 联调通过。[Agent 快照绑定已发布工具版本](next/G07-agent-tool-binding.md)已保存，React 新建和追加版本已接工具选择，桌面隔离联调验证新旧快照分别固定 v2 / v1。`writeAgentError` 已映射跨空间工具为 404，正式隔离 PostgreSQL HTTP race 通过。试跑记录使用开始执行时选定的 VersionID，发布按目标版本查成功结果。后续布置代码时在对话里直接贴出全部需要新增或替换的代码。
2. [会话把固定工具版本传给 Engine](next/G07-conversation-tool-versions.md)已保存、正式隔离 PostgreSQL race 通过。[按固定版本构造 Eino 工具](next/G07-tool-build-by-version.md)已保存并通过正式隔离 PostgreSQL、ToolsNode 和本地双端点验收。[Engine 组装工具列表](next/G07-engine-build-tools.md)已保存，正式 ToolsNode race 通过。[Eino OpenAI 模型适配器](next/G07-eino-model-provider.md)已保存并通过正式 race。[ADK 执行固定版本工具](next/G07-adk-tool-loop.md)已保存并通过正式 Engine race。[实时工具事件](next/G07-live-tool-events.md)已保存并通过正式 Engine race。[工具消息入库与跨轮回放](next/G07-persist-tool-history.md)已保存并在正式工作区通过隔离 PG race，React 历史工具详情已接通。
3. 第 4 天学习验收通过。正式工作区使用隔离 PostgreSQL 与桌面 Chrome，验证了同一轮订单和库存查询、工具事件配对、跨轮回放与刷新。Crossborder 模块名已统一，本地门禁通过。G07 已保存[追加版本的冲突保护](next/G07-tool-version-conflict.md)，正式隔离 PostgreSQL 回归验证旧配置无法在凭据清空后重新落库。[未知工具的失败轨迹](next/G07-unknown-tool-fallback.md)已写入正式工作区，Engine、会话、HTTP race 和隔离桌面回读通过。[正常工具调用的主记录 ID](next/G07-tool-call-identity.md)已写入正式工作区并合入回归。本地门禁与桌面回读通过，下一步由负责人提交 G07 PR，待远程 CI 和按提交复审。见[G07 验收](../delivery/G07-VERIFICATION.md)。

## Final 能力延后归属（防遗漏）

| Final 能力 | 现状 | 落到 |
| --- | --- | --- |
| Agent 快照工具/技能/知识库引用 | 工具快照已接表单、ADK 与会话；技能、知识库待实现 | G07/G10/G11 |
| agent_endpoints | 未建表 | G09 |
| teams | 未建表 | G13 |
| skill_triggers | 未建表 | G11 |
| checkpoints | 未建表 | G12 |
| conversation_runtime_configs 独立表 | 暂用 conversations.runtime_config_json | 已固化版本；必要时再迁移 |
| model_call_logs / OTel | 未建 | G17 |
| 额度预留 | 仅 ErrQuotaExceeded | G15 |
| 审批/A2UI | 聊天仅完成/失败/取消 | G12 |
| WebSocket | G06 事件流目前只有 SSE 接入 | G19 接入身份/Origin/断连，G20 核对协议一致性 |
| mockllm / CLI / SDK / 部署 | 未做 | G20/G21/G22 |
| prompt_usage_logs | 未建 | G16 审计事实与查询；G17 验证可观测性关联 |

每组验收必须逐项标注“已实现”或“归到 Gxx”，G22 汇总核对。
