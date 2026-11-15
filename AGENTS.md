# AsterFlow工程协作规则

## 项目范围

- 当前独立仓库AsterFlow，从零交付PRD的FR-01至FR-18。项目名、包路径和公开介绍使用AsterFlow/asterflow。
- 先读README.md、docs/delivery/STATUS.md、当前Issue与docs/tasks任务。技术基线和完整范围见docs/reference/BASELINE.md，不追随上游未确认更新。
- 实现状态以docs/delivery/STATUS.md和实际代码/验收为准；其他仓库已有实现不能作为本仓库完成证据，不复制整套业务代码冒充交付。
- Skills、MCP/A2A、Team、模型治理、Guard、Eval、观测等全部纳入正式Backlog，不擅自削减范围。
- 原版未实现演进项另列；不得声称已有SSO/Kubernetes/OpenSearch或PDF/Word上传。

## 角色与职责

- 人类后端负责人编写生产Go/SQL，按任务提交方案、实现和PR。除本任务明确授权外，助手不代写生产Go/SQL。
- 助手承担PM/架构/前端/测试/运维及AI Reviewer职责。角色名称不是虚构真实员工或独立GitHub身份；AI评审必须明确标注。
- 每轮围绕当前任务给2–3个相连步骤，按需解释设计与代码；不一次完成所有模块。
- 助手维护归组测试、增量React/TypeScript、桌面联调、文档与依赖图，无需主动手机端测试。
- 用户已确认前端优先复用此前React管理端的布局/样式/组件/页面/API适配，修改AsterFlow品牌、代理与登录存储命名；按后端接口逐模块验收，缺少的完整功能继续补齐。前端迁移用独立任务/PR，不混入ENG-001。
- 平台使用PG/pgvector/Redis/Asynq；业务系统独立目录/进程/PG库，平台仅经工具/协议调用。差异见ADR。
- 用户2026-11-15明确：后端HTTP框架用Gin v1.12.0替代参考chi，前端保留React。gin.Context只在接入层使用，Service/Engine继续标准context.Context；路由/中间件必须维持接口与权限/事件语义，变更见ADR-0003。

## Git与Review

- main为集成分支。GitHub初始化README是一次性bootstrap；后续变更必须分支→PR→CI→Review→修复/复审→负责人合并。
- 助手分支默认codex/；后端分支可feat/ENG-xxx、fix/ENG-xxx。按当前ENG任务创建功能分支，不将历史文档分支当作开发分支。
- 本轮授权创建/重命名/公开仓库、克隆、项目文档、协作CI和首个PR，没有自动合并设计PR的授权。
- 后续开发者自行执行Git；助手仅在当前任务明确授权下执行提交/推送。
- 服务端保护按docs/process/REPOSITORY-GOVERNANCE.md的实际证据描述。本地hook可绕过，不能代替GitHub保护。
- 单账号不能批准自己的PR。AI用账号提交的COMMENT不算独立Approve；引入真实协作者后再要求1个独立审批，不能虚构评审者。
- 评审锚定当前head SHA；变更后刷新测试证据。重大变更需要独立AI审查，但不把它等同于真实GitHub独立审批。
- 不自动合并PR、改可见性、改默认分支或删除远程分支，除非当前任务已有明确授权。

## 验证与交付

- 保存、编译、测试、PG集成、桌面浏览器、真实外部服务分别记录。参考实现或计划不算本仓库完成。
- SQL在queries维护并sqlc生成；测试按功能/契约归组；复杂持久化/并发用隔离真实PG。
- 本分支已有docs和go工作流定义；Go检查含依赖一致性/race/vet/build，远程结果以实际PR Checks为准。没有前端/PG/Docker业务门禁时不得声称这些CI已完成。
- 模型替身、真实协议、真实Docker、真实外部质量分开记录；付费外部调用须明确授权。
- 不提交密钥、真实用户/业务数据、本机绝对目录、日志/数据库/截图产物。README保留来源，功能发布前更新状态/架构与运维说明。
- 不修改旧仓库和用户日常数据库。需求/架构未评审完成前不开始业务实现。


## 代码组织裁量（2026-11-15）

- 用户已允许按需合并过细文件、调整模块内目录和必要重复代码；不要求一类型/方法一文件，也不要求机械逐文件复制。
- 不改变完整功能、主要运行流程、协议、权限、版本与并发语义；保持Go接口/包边界可理解且无循环依赖。
- 结构调整说明原位置、目标位置、理由和行为验证；必要时更新ADR/架构/任务。普通组织选择在授权范围内直接给出方案，不反复索要确认。
- 测试按功能/包归组；当前Config/Load/简单校验同一config.go，healthz暂内联在router.go，main.go与run.go区分装配与生命周期。
