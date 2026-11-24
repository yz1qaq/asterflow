# 项目交付状态

G04 提示词治理由 [PR #15](https://github.com/yz1qaq/asterflow/pull/15) 合并，G05 Agent 与会话由 [PR #16](https://github.com/yz1qaq/asterflow/pull/16) 合并。G06 初版聊天由 [PR #17](https://github.com/yz1qaq/asterflow/pull/17) 合并，后续补充范围通过本地验收，[补充 PR #18](https://github.com/yz1qaq/asterflow/pull/18) 进入评审修复。最新接续点见[跨 AI 交接](../handoff/STATE.md)。

| 项目 | 当前事实 |
| --- | --- |
| 本地 | `feat/ENG-014-015-chat-hardening`，HEAD `55d5778`，PR #18 的审查修复在工作区 |
| 已合并 | 设计 PR #3、基础工程 PR #4/#6/#8、G01 PR #10、G02 PR #12、G03 PR #14、G04 PR #15、G05 PR #16、G06 初版 PR #17 |
| G03 最终验证 | head 40e1cd8 的 Documentation、Go、Web CI 与独立 AI 复审通过；Squash 结果与该 head 的树相同 |
| 数据库 | 2026-11-24 只读实查开发库为 version=11、dirty=false |
| G05 交付 | Agent 版本/环境/快照/管理页、会话/消息/工具历史、归属与租约均完成；PR #16 已合并 |
| G04 历史范围 | 版本/环境指针/发布/回滚/操作记录、diff、实验与灰度（含多变体）、事件时间线、Redis 跨进程失效均已完成；隔离 PG+Redis、双进程与桌面验收通过 |
| G06 交付 | 统一 Eino ADK、Gateway 主备、续租、多轮与首轮模板、SSE / Go 同步聚合和桌面联调已完成本地验收。见 [G06 验收](G06-VERIFICATION.md) |
| 下一步 | 推送 PR #18 的审查修复，确认新 head 的远程 CI 与复审，通过后合并，再进入 G07 工具闭环与交互学习网页。见[下一步](../handoff/NEXT.md) |
| 已知非阻断项 | 共享框架 chunk 约 562.6 kB；G01 用户列表同时间稳定排序建议保留 |

[路线图](ROADMAP.md) · [G03 验收记录](G03-VERIFICATION.md) · [G04 开发任务](../tasks/G04-prompt-governance.md) · [G04 验收记录](G04-VERIFICATION.md)。

完整 FR-01 至 FR-18 的交付范围不变。当前无生产部署；用户停用／删除暂不实现。
