# 下一批：G06 收尾（桌面验收 → PR）

G06（ENG-014/015）代码、测试与真实模型端到端已完成：Gateway/failover/OpenAIClient、执行器与事件、会话创建固化版本、RunTurn 租约+提交、SSE 与聊天页；全量门禁通过，真实 DeepSeek 调用已授权并验证，见 [G06 验收](../delivery/G06-VERIFICATION.md)。

1. **桌面浏览器验收（聊天流 UI）。** 用开发库（DeepSeek 已配置）走浏览器：登录 → 海外电商 workspace → 会话历史选会话 → 发消息 → 流式回复逐字显示 → 刷新回读 user/assistant 顺序正确。SQL 复核租约清空。
2. **提交与 PR。** 学员执行 Git：提交、推送 `feat/ENG-014-015-chat-loop`、按模板创建 PR（范围、验证、边界：工具/审批/观测延后）。助手只读复审与 CI 核对。
3. 合并后进入 G07：工具版本/Schema/凭据/REST 与 internal SDK、业务工具闭环。

注意：`run_started` 的 `agent_version_id`/`turn_revision` 当前为空，属非阻塞完善项，可随 PR 一起或后续补；测试用显式 `ASTERFLOW_TEST_DATABASE_URL`/`ASTERFLOW_TEST_REDIS_URL` 与一次性容器。

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
| WebSocket | 先 SSE | G06 后按需 |
| mockllm / CLI / SDK / 部署 | 未做 | G20/G21/G22 |
| prompt_usage_logs | 未建 | 真实使用记录组；不拿空表充数 |

每组验收必须逐项标注“已实现”或“归到 Gxx”，G22 汇总核对。
