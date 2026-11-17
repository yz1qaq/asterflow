# 需求到交付追踪

ENG-001/002已合并，ENG-003已由PR #8合并并完成开发库迁移验收；IAM业务及其余模块尚未交付。负责人已批准按ROADMAP的需求组交付，计划编号不代表实现完成。实际PR/head与验证证据在各组记录。

| 需求 | 主责模块 | Epic | 需求组 | 当前实现 |
| --- | --- | --- | --- | --- |
| FR-01 | MOD-IAM | EP-02 | G00、G01、G02 | 000001已应用；G01用户/登录/API/Web已由PR #10合并；G02空间/成员/中间件/当前空间Web本地验收及P1正式修复通过；待PR、远程CI与最终head审查后合并 |
| FR-02 | MOD-PROMPT | EP-04 | G04 | 未实现 |
| FR-03 | MOD-MODEL | EP-03 | G03、G06、G15 | 未实现 |
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
