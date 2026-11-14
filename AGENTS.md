# AsterFlow工程协作规则

## 项目范围

- 当前独立仓库AsterFlow，从零交付PRD的FR-01至FR-18。项目名、包路径和公开介绍使用AsterFlow/asterflow。
- 先读README.md、docs/delivery/STATUS.md、当前Issue与docs/tasks任务。技术基线和完整范围见docs/reference/BASELINE.md，不追随上游未确认更新。
- 当前只有需求/设计/协作工具，没有业务代码；其他仓库已有实现不能作为本仓库完成证据，不复制整套业务代码冒充交付。
- Skills、MCP/A2A、Team、模型治理、Guard、Eval、观测等全部纳入正式Backlog，不擅自削减范围。
- 原版未实现演进项另列；不得声称已有SSO/Kubernetes/OpenSearch或PDF/Word上传。

## 角色与职责

- 人类后端负责人编写生产Go/SQL，按任务提交方案、实现和PR。除本任务明确授权外，助手不代写生产Go/SQL。
- 助手承担PM/架构/前端/测试/运维及AI Reviewer职责。角色名称不是虚构真实员工或独立GitHub身份；AI评审必须明确标注。
- 每轮围绕当前任务给2–3个相连步骤，按需解释设计与代码；不一次完成所有模块。
- 助手维护归组测试、增量React/TypeScript、桌面联调、文档与依赖图，无需主动手机端测试。
- 平台使用PG/pgvector/Redis/Asynq；业务系统独立目录/进程/PG库，平台仅经工具/协议调用。差异见ADR。

## Git与Review

- main为集成分支。GitHub初始化README是一次性bootstrap；后续变更必须分支→PR→CI→Review→修复/复审→负责人合并。
- 助手分支默认codex/；后端分支可feat/ENG-xxx、fix/ENG-xxx。当前文档分支codex/project-foundation。
- 本轮授权创建/重命名/公开仓库、克隆、项目文档、协作CI和首个PR，没有自动合并设计PR的授权。
- 后续开发者自行执行Git；助手仅在当前任务明确授权下执行提交/推送。
- 服务端保护按docs/process/REPOSITORY-GOVERNANCE.md的实际证据描述。本地hook可绕过，不能代替GitHub保护。
- 单账号不能批准自己的PR。AI用账号提交的COMMENT不算独立Approve；引入真实协作者后再要求1个独立审批，不能虚构评审者。
- 评审锚定当前head SHA；变更后刷新测试证据。重大变更需要独立AI审查，但不把它等同于真实GitHub独立审批。
- 不自动合并PR、改可见性、改默认分支或删除远程分支，除非当前任务已有明确授权。

## 验证与交付

- 保存、编译、测试、PG集成、桌面浏览器、真实外部服务分别记录。参考实现或计划不算本仓库完成。
- SQL在queries维护并sqlc生成；测试按功能/契约归组；复杂持久化/并发用隔离真实PG。
- 当前CI仅检查文档/范围/本地hook，不在没有业务源码时声称Go/前端流水线已经完成。
- 模型替身、真实协议、真实Docker、真实外部质量分开记录；付费外部调用须明确授权。
- 不提交密钥、真实用户/业务数据、本机绝对目录、日志/数据库/截图产物。README保留来源，功能发布前更新状态/架构与运维说明。
- 不修改旧仓库和用户日常数据库。需求/架构未评审完成前不开始业务实现。
