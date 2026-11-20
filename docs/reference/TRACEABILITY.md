# 需求到交付追踪

ENG-001/002/003已合并；G01（PR #10）、G02（PR #12）、G03（PR #14）已合并；G04提示词治理进行中，其余模块按组推进。负责人已批准按ROADMAP的需求组交付，计划编号不代表实现完成。实际PR/head与验证证据在各组记录。

| 需求 | 主责模块 | Epic | 需求组 | 当前实现 |
| --- | --- | --- | --- | --- |
| FR-01 | MOD-IAM | EP-02 | G00、G01、G02 | 000001已应用；G01用户/登录/API/Web已由PR #10合并；G02空间/成员/中间件/当前空间Web及P1修复已由PR #12合并，最终head CI与AI复审通过 |
| FR-02 | MOD-PROMPT | EP-04 | G04 | 核心迁移、Store、模板编译、Service/HTTP、环境指针与发布/回滚、diff、实验/灰度（含稳定分桶、完成/回滚）、操作事件、Redis 跨进程失效、React 三 tab 均已交付并通过隔离 PG+Redis、双进程与桌面验收；待整组 PR |
| FR-03 | MOD-MODEL | EP-03 | G03、G06、G15 | G03 已由 PR #14 合并（Squash b85be38）；模型账户、部署计价、方案版本、环境绑定与 Web 已完成；Gateway 路由/额度在 G06/G15 |
| FR-04 | MOD-TOOL | EP-06 | G07、G08、G09 | 未实现 |
| FR-05 | MOD-SKILL | EP-08 | G11 | 未实现 |
| FR-06 | MOD-KB | EP-07 | G10 | 未实现 |
| FR-07 | MOD-AGENT | EP-05 | G05、G06、G07 | 未实现 |
| FR-08 | MOD-APPROVAL | EP-09 | G12 | 未实现 |
| FR-09 | MOD-TEAM | EP-10 | G13 | 未实现 |
| FR-10 | MOD-GUARD | EP-11 | G14、G15 | 未实现 |
| FR-11 | MOD-AUDIT | EP-12 | G16 | 未实现 |
| FR-12 | MOD-OBS | EP-13 | G17 | 未实现 |
| FR-13 | MOD-EVAL | EP-14 | G18 | 未实现 |
| FR-14 | MOD-CACHE | EP-15 | G04、G10、G12、G15、G16、G19 | 未实现 |
| FR-15 | MOD-CHANNEL | EP-16 | G19 | 未实现 |
| FR-16 | MOD-SDK | EP-17 | G20 | 未实现 |
| FR-17 | MOD-BUSINESS | EP-18 | G07、G12、G21 | 未实现 |
| FR-18 | MOD-CORE、MOD-DELIVERY | EP-01、EP-19 | G00、G01、G22 | 001/002已合并；003已合并及完成开发库应用；完整部署/质量范围未完成 |

完整验收链：FR→MOD→Epic→ENG/Issue→PR/head→测试/集成/桌面/部署证据→合并/Verified。当前DOC-001负责项目契约和协作流程，不将它标记为FR-18全部完成。
固定参考每个文件的主责模块在[upstream-files.json](upstream-files.json)；后续按模块补“目标路径/验收证据”差异清单。上游目录存在不能作为本仓库能力。
