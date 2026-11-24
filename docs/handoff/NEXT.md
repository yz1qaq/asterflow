# 下一批 G07 工具闭环

G06 初版已由 PR #17 合并，[补充 PR #18](https://github.com/yz1qaq/asterflow/pull/18) 的审查修复已保存。先由负责人提交并推送修复，核对新 head 的 CI 与复审，通过后合并，再按 [路线图](../delivery/ROADMAP.md) 进入 G07。G06 证据见 [验收记录](../delivery/G06-VERIFICATION.md)。常规分工保持学员写生产 Go/SQL，助手维护 React、验证、文档和评审。

1. 核对固定 Final 的 Tool 注册、版本、绑定与策略，再按 G07 任务拆出首批生产代码与验收。
2. 将已绑定工具接到现有 Eino ADK Runner，交付独立业务系统查询、工具事件与历史消息保存和回喂。
3. **G07 起附交互学习网页。** 随第一个验收功能交付可运行的代码路径示例，再随工具闭环补齐业务调用、事件和持久化。验收要求见 [学习计划](LEARNING-PLAN.md#从-g07-起的交互学习网页)。

## Final 能力延后归属（防遗漏）

| Final 能力 | 现状 | 落到 |
| --- | --- | --- |
| Agent 快照工具/技能/知识库引用 | 字段暂不启用 | G07/G10/G11 |
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
