# 发给下一位 AI 的提示词

复制以下整段即可：

> 这是已经开始的 AsterFlow 项目，请继续用中文带我做，不要从头重来。我给你的两个目录中，asterflow 是唯一要开发的项目，kbot 只用于核对固定 Final 参考。先阅读 asterflow 的 AGENTS.md、docs/handoff/START-HERE.md、STATE.md、NEXT.md，再按交接入口检查真实分支、保存的文件和验证记录。请使用仓库里的 .agents/skills/asterflow-tutor/SKILL.md；如果你的软件不支持 Skill，就把它当作项目带学规则读。
>
> 我要完整实现 Final 的功能和验收，不因 10 天计划删掉任何模块。默认我亲手写正式 Go/SQL，自己提交、推送、开 PR 和合并；你负责需求拆解、解释、测试、按进度接 React、桌面联调、运维文档和 AI 复审。每次给当前需求组 2–3 个相关步骤，说明文件、代码、调用过程和为什么这么写；我问原理时先回答。不要默认替我写生产 Go/SQL 或执行远程写入。
>
> 请先用实际文件确认「已经保存／已经验证／还缺什么」，然后从最早没完成的必要步骤继续。完整验收之前不要宣称已完成。每次实际进度变化后更新 docs/handoff/STATE.md 和 NEXT.md，让我下次换 AI 或回到 Codex 时只靠目录就能续上。请保留未提交改动，不操作日常数据库，不在交接里写真实凭据。

若已经在原 Codex 对话里重新打开同一份工作区，可以只发：「我回来了，请按 AsterFlow 的 docs/handoff/START-HERE.md、STATE.md、NEXT.md 核对我在另一个 AI 软件中的最新进度，然后继续。」
