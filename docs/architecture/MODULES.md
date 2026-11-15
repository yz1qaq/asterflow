# 模块职责、依赖与协作边界

本表定义目标职责，实际实现/验收状态以STATUS为准。平台生产Go/SQL负责人为yz1qaq；前端/自动化测试/运维由助手按任务提供交付，方案与PR由负责人评审。分工是职责划分，不虚构真实员工或独立GitHub账号。

| ID | 模块 | 需求 | 主要目标位置（省略internal前缀） | 牵头职责 | 交付边界 | 依赖 | Epic |
| --- | --- | --- | --- | --- | --- | --- | --- |
| MOD-CORE | 启动与公共契约 | FR-18 | cmd/*、internal/config/domain/util、api公共层 | 平台基础 | 配置、健康/就绪、生命周期、公共装配与错误 | 无 | EP-01 |
| MOD-IAM | 身份与空间 | FR-01 | platform/iam、api/middleware、api/v1/iam/auth | 后端负责人 | 用户/空间/成员/权限和可信context | CORE/PG | EP-02 |
| MOD-MODEL | 模型治理 | FR-03 | platform/modelconfig、runtime/llm | 后端负责人 | 账户/部署/Profile/路由/用量/配额与版本绑定 | CORE/IAM/PG | EP-03 |
| MOD-PROMPT | Prompt治理 | FR-02 | platform/prompt、runtime/promptcache | 后端负责人 | 模板/变量/版本/环境/实验/灰度/缓存失效 | IAM/MODEL/PG/Redis | EP-04 |
| MOD-AGENT | Agent与会话 | FR-07 | platform/agent、runtime/engine、api/streaming | 后端负责人 | 不可变装配、ADK、消息/事件、租约/整轮保存 | IAM/PROMPT/MODEL；TOOL/SKILL/KB按阶段 | EP-05 |
| MOD-TOOL | 工具与沙箱 | FR-04 | platform/tool、runtime/tooling/sandbox、cmd/sandbox-runner | 后端负责人 | 五源Registry/Executor、Schema/凭据/试跑发布和Runner | IAM/PG；APPROVAL用于敏感路径 | EP-06 |
| MOD-KB | 知识库与检索 | FR-06 | platform/kb、connector、runtime/retriever | 后端负责人 | 来源/文档/Chunk/Job、异步摄取、混合检索与范围 | IAM/PG/Redis/CACHE | EP-07 |
| MOD-SKILL | Skills | FR-05 | platform/skill、runtime/skillrunner | 后端负责人 | 解析/版本/发布/订阅、L1/L2、权限收窄 | IAM/TOOL/KB/AGENT | EP-08 |
| MOD-APPROVAL | 审批与受控UI | FR-08 | platform/approval、runtime/engine审批、internal/a2ui | 后端负责人 | 中断/checkpoint、决策/恢复/租约、A2UI | AGENT/TOOL/IAM/CACHE | EP-09 |
| MOD-TEAM | 多Agent编排 | FR-09 | platform/team、runtime/team、engine/team_runtime | 后端负责人 | Team快照、Supervisor/Pipeline、事件/取消 | AGENT/SKILL/TOOL | EP-10 |
| MOD-GUARD | 运行治理 | FR-10 | runtime/guard、platform/guardconfig | 后端负责人 | 四Hook、动态规则、注入/PII/限流/配额/分级 | IAM/AGENT/MODEL/CACHE | EP-11 |
| MOD-AUDIT | 审计与归档 | FR-11 | platform/audit/maintenance、infrastructure/objstore、infrastructure/postgres/partitioning.go | 后端负责人 | 事实轨迹、导出、分区/归档/维护 | IAM/PG/MinIO/CACHE | EP-12 |
| MOD-OBS | 可观测性 | FR-12 | infrastructure/otel/metrics、runtime/* tracing | 助手运维+后端接口协作 | OTel/Prometheus/Langfuse与内容采集 | CORE/AGENT/MODEL/AUDIT | EP-13 |
| MOD-EVAL | 评估与门禁 | FR-13 | platform/eval、api/v1/eval | 后端负责人 | 数据/Case/Run/Score、三层Judge与Gate | AGENT/MODEL/PG | EP-14 |
| MOD-CACHE | 缓存与任务 | FR-14 | runtime/cache、infrastructure/redis/jobs | 后端负责人 | 缓存/锁/幂等、Asynq任务契约与重试 | CORE/Redis/PG | EP-15 |
| MOD-CHANNEL | 渠道接入 | FR-15 | integration、api/v1/integration、WS | 后端负责人 | Webhook/飞书/WS身份、签名、去重与回复 | IAM/AGENT/CACHE | EP-16 |
| MOD-SDK | SDK与CLI | FR-16 | pkg/sdk、cmd/asterctl、OpenAPI/静态托管 | 后端负责人+助手前端 | Go客户端/CLI/Swagger2.0、完整管理页 | 各已交付API | EP-17 |
| MOD-BUSINESS | 独立业务与预设 | FR-17 | business_systems/*、platform/preset、cmd/mockllm | 后端负责人 | 电商/保险完整行为、持久化适配、场景资产 | TOOL/KB/SKILL/APPROVAL；业务可独立启动 | EP-18 |
| MOD-DELIVERY | 部署与工程质量 | FR-18 | deploy、scripts、tests、.github、docs | 助手测试/运维，负责人评审 | 镜像/Compose、测试/CI、runbook/性能和范围核验 | 所有模块分阶段 | EP-19 |

## 接口责任

- API接收请求并提取可信身份/空间，Service执行业务规则，Store做持久化适配；PG约束兜底不替代业务校验。
- Engine依赖平台/模型/工具等最小接口，Registry按固定版本构建Executor并适配Eino工具；不将“注册数据库记录”和“构建可执行对象”混为一件事。
- Skill与KB通过固定Agent版本授予的范围进入运行时，模型Schema只引导，实际执行仍强制检查。
- 审批Store和Engine通过ApprovalGate适配；恢复Worker只消费已经授权且状态可恢复的审批，不接受前端直接指定任意checkpoint。
- 前端请求类型、默认值、错误与事件字段经任务评审冻结；后端未完成可用协议夹具开发，但状态必须明确，不提前展示可用功能。
- 测试工程师用相同契约验证替身/真实PG实现；部署工程师接收配置/迁移/健康和runbook，不能从Handler内部猜配置。

## 拆分原则

Epic按[需求组](../delivery/ROADMAP.md)拆分，ENG作为组内子任务；同组的存储/业务/API/运行时/Web/异常回归共同完成再开一个PR，不按技术层或函数机械拆PR。真实复杂机制保留讲解和验证，大组允许跨天；必要进一步拆分时说明独立验收边界。
模块边界允许增量接入：例如基础Agent先接单模型，但在Backlog中保留完整模型/Skill/工具等必做子项；完成基础任务不标记整个模块完成。
每张ENG任务写明可修改路径、输入/输出接口、依赖版本、数据迁移、测试/前端责任、验收及范围外事项。重大公共接口改变先改设计/ADR，再修改调用方。
