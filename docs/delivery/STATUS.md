# 项目交付状态

G04 提示词治理由 [PR #15](https://github.com/yz1qaq/asterflow/pull/15) 合并，G05 Agent 与会话由 [PR #16](https://github.com/yz1qaq/asterflow/pull/16) 合并。G06 聊天闭环进行中。最新接续点见[跨 AI 交接](../handoff/STATE.md)。

| 项目 | 当前事实 |
| --- | --- |
| 本地 | feat/ENG-014-015-chat-loop，基于 82143d3dda4c46c0b1d891d92c4d5e8f7a0e2de0 |
| 已合并 | 设计 PR #3、基础工程 PR #4/#6/#8、G01 PR #10、G02 PR #12、G03 PR #14、G04 PR #15、G05 PR #16 |
| G03 最终验证 | head 40e1cd8 的 Documentation、Go、Web CI 与独立 AI 复审通过；Squash 结果与该 head 的树相同 |
| 数据库 | 开发库已应用到 version=8（学员本地）；迁移 000006–000010 均在隔离 schema 验证，开发库尚未应用 000009/000010 |
| G05 交付 | Agent 版本/环境/快照/管理页、会话/消息/工具历史、归属与租约均完成；PR #16 已合并 |
| G04 历史范围 | 版本/环境指针/发布/回滚/操作记录、diff、实验与灰度（含多变体）、事件时间线、Redis 跨进程失效均已完成；隔离 PG+Redis、双进程与桌面验收通过 |
| 下一步 | G06 真实 Gateway/ADK 执行/SSE/聊天；见[下一步](../handoff/NEXT.md) |
| 已知非阻断项 | 共享框架 chunk 约 562.6 kB；G01 用户列表同时间稳定排序建议保留 |

[路线图](ROADMAP.md) · [G03 验收记录](G03-VERIFICATION.md) · [G04 开发任务](../tasks/G04-prompt-governance.md) · [G04 验收记录](G04-VERIFICATION.md)。

完整 FR-01 至 FR-18 的交付范围不变。当前无生产部署；用户停用／删除暂不实现。
