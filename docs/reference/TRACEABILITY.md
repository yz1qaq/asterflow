# 需求到交付追踪

当前仅建立需求/模块归属，没有业务实现或验收通过。每个Epic派生ENG任务后，在任务/PR记录实际文件和证据，不用下表替代测试。

| 需求 | 主责模块 | Epic | 当前实现 |
| --- | --- | --- | --- |
| FR-01 | MOD-IAM | EP-02 | 未实现 |
| FR-02 | MOD-PROMPT | EP-04 | 未实现 |
| FR-03 | MOD-MODEL | EP-03 | 未实现 |
| FR-04 | MOD-TOOL | EP-06 | 未实现 |
| FR-05 | MOD-SKILL | EP-08 | 未实现 |
| FR-06 | MOD-KB | EP-07 | 未实现 |
| FR-07 | MOD-AGENT | EP-05 | 未实现 |
| FR-08 | MOD-APPROVAL | EP-09 | 未实现 |
| FR-09 | MOD-TEAM | EP-10 | 未实现 |
| FR-10 | MOD-GUARD | EP-11 | 未实现 |
| FR-11 | MOD-AUDIT | EP-12 | 未实现 |
| FR-12 | MOD-OBS | EP-13 | 未实现 |
| FR-13 | MOD-EVAL | EP-14 | 未实现 |
| FR-14 | MOD-CACHE | EP-15 | 未实现 |
| FR-15 | MOD-CHANNEL | EP-16 | 未实现 |
| FR-16 | MOD-SDK | EP-17 | 未实现 |
| FR-17 | MOD-BUSINESS | EP-18 | 未实现 |
| FR-18 | MOD-CORE、MOD-DELIVERY | EP-01、EP-19 | 未实现 |

完整验收链：FR→MOD→Epic→ENG/Issue→PR/head→测试/集成/桌面/部署证据→合并/Verified。当前DOC-001负责项目契约和协作流程，不将它标记为FR-18全部完成。
固定参考每个文件的主责模块在[upstream-files.json](upstream-files.json)；后续按模块补“目标路径/验收证据”差异清单。上游目录存在不能作为本仓库能力。
