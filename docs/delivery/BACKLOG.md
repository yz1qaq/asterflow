# 交付Backlog与迭代组织

ENG-001已由PR #4合并；ENG-002已由PR #6合并；ENG-003已由PR #8合并；[G01](../tasks/G01-user-auth.md)用户与登录（ENG-004+005）已完成本地实现/验收，待PR/最终head门禁，其余业务任务未交付；排期依据依赖和验收能力滚动确定，不用固定截止日削减范围。按可独立评审的小任务领取，Epic不是一次性大PR。首项[ENG-001](../tasks/ENG-001.md)合并和CI事实见[STATUS](STATUS.md)；该PR缺少合并前正式Review，后续任务不得以CI代替评审。

## 已批准的需求组合并

负责人2026-11-15确认[ROADMAP](ROADMAP.md)：保留001/002历史、003按当前流程收尾；004至038合为G01至G22共22个需求组。后续同组默认一个Issue/分支/集中验收/PR，前后端及测试文档随组交付，不按函数或技术层反复提交。大组可跨学习日，实际范围与完成证据分别记录。
G01（ENG-004+005用户与登录）正在提交前收尾；合并后推进G02工作空间与成员。同步/流式共享执行、过渡实现精简按[ADR-0004](../architecture/adr/0004-feature-groups-and-runtime-scope.md)，原完整范围不削减。

## 入口与阶段

EP-00（设计基线）：[DOC-001 / Issue #1](https://github.com/yz1qaq/asterflow/issues/1)需求/架构/模块/流程文档+docs CI+来源范围清单，经PR审阅。它完成不代表任何业务模块完成。
建议交付顺序：CORE/IAM → 模型/Prompt/Agent基础 → 工具/KB/Skill → 审批/A2UI/业务写操作 → MCP/A2A/Team → Guard/Eval/审计观测 → 渠道/SDK/业务全场景 → 完整部署/覆盖核验。
缓存/任务、测试/运维在所需节点先交付子任务，不等待Epic全完；基础Agent可先单模型，但必须保留完整模型/Prompt/Skill装配子任务。

## Epic及任务切片

| Epic | 模块 | 必须交付的任务切片 | 依赖 | 交付验收 |
| --- | --- | --- | --- | --- |
| EP-01 | MOD-CORE 启动与公共契约 | 配置/健康与生命周期（ENG-001）→PG连接/迁移/sqlc→就绪/基础Web→启动集成 | 无 | 对应FR-18与模块契约、真实依赖和必要桌面联调 |
| EP-02 | MOD-IAM 身份与空间 | User/Workspace/Role模型与Store→Service/JWT→Auth/Workspace/API→用户/空间Web及越权/并发验收 | CORE/PG | 对应FR-01与模块契约、真实依赖和必要桌面联调 |
| EP-03 | MOD-MODEL 模型治理 | 账户/密钥→部署/计价→Profile/环境/绑定→路由/failover→用量预留/过期/配额→Models联调 | CORE/IAM/PG | 对应FR-03与模块契约、真实依赖和必要桌面联调 |
| EP-04 | MOD-PROMPT Prompt治理 | 模板/变量/版本/环境→用户提示词/渲染→diff/rollback→实验/灰度→跨进程失效→Prompt联调 | IAM/MODEL/PG/Redis | 对应FR-02与模块契约、真实依赖和必要桌面联调 |
| EP-05 | MOD-AGENT Agent与会话 | Agent版本装配→Conversation/Message/ToolCall→租约/整轮事务→ADK同步/SSE/WS→多工具/Skill/KB组合回归 | IAM/PROMPT/MODEL；TOOL/SKILL/KB按阶段 | 对应FR-07与模块契约、真实依赖和必要桌面联调 |
| EP-06 | MOD-TOOL 工具与沙箱 | Registry/Schema/凭据→REST/SDK→独立Runner/代码执行→MCP stdio/HTTP→A2A→试跑/发布/治理→Tools联调 | IAM/PG；APPROVAL用于敏感路径 | 对应FR-04与模块契约、真实依赖和必要桌面联调 |
| EP-07 | MOD-KB 知识库与检索 | KB/文档/来源/Job→Markdown与hash同步→Asynq摄取→Embedding/索引→路由/混合/重排→Agent范围/来源→KB联调 | IAM/PG/Redis/CACHE | 对应FR-06与模块契约、真实依赖和必要桌面联调 |
| EP-08 | MOD-SKILL Skills | SKILL解析→版本/Store→发布引用门禁/订阅→快照→L1/L2/触发→工具/KB/网络收窄→Skills联调 | IAM/TOOL/KB/AGENT | 对应FR-05与模块契约、真实依赖和必要桌面联调 |
| EP-09 | MOD-APPROVAL 审批与受控UI | Approval/Checkpoint存储→ADK中断→pending/等待→决策/Worker恢复→执行租约→A2UI/归属→敏感业务幂等回归 | AGENT/TOOL/IAM/CACHE | 对应FR-08与模块契约、真实依赖和必要桌面联调 |
| EP-10 | MOD-TEAM 多Agent编排 | Team/Version/环境→成员Agent固定→Supervisor→Pipeline→事件/取消/权限→Teams联调 | AGENT/SKILL/TOOL | 对应FR-09与模块契约、真实依赖和必要桌面联调 |
| EP-11 | MOD-GUARD 运行治理 | 四Hook契约→注入/PII/出域→动态规则/空间→限流/配额/分级→Guard页面与运行链验证 | IAM/AGENT/MODEL/CACHE | 对应FR-10与模块契约、真实依赖和必要桌面联调 |
| EP-12 | MOD-AUDIT 审计与归档 | 操作事实/Store→工具/技能/沙箱轨迹→审计查询→MinIO导出→月分区/维护/安全归档→Audit联调 | IAM/PG/MinIO/CACHE | 对应FR-11与模块契约、真实依赖和必要桌面联调 |
| EP-13 | MOD-OBS 可观测性 | trace context/span→OTLP→Metrics→Langfuse Compose→内容采集→Observability联调与错误/取消追踪 | CORE/AGENT/MODEL/AUDIT | 对应FR-12与模块契约、真实依赖和必要桌面联调 |
| EP-14 | MOD-EVAL 评估与门禁 | Dataset/Case/Run/Score→三层Judge→固定版本运行→阈值/报告/Gate→Eval界面与CI阻断 | AGENT/MODEL/PG | 对应FR-13与模块契约、真实依赖和必要桌面联调 |
| EP-15 | MOD-CACHE 缓存与任务 | Redis/Jobs基础→Embedding缓存→Prompt PubSub→幂等/锁→摄取/审批/渠道/维护任务→多进程重试回归 | CORE/Redis/PG | 对应FR-14与模块契约、真实依赖和必要桌面联调 |
| EP-16 | MOD-CHANNEL 渠道接入 | WS身份/Origin/断连→Webhook签名/重放→飞书验证/解密/入队→回复/重试→真实协议与外部状态区分 | IAM/AGENT/CACHE | 对应FR-15与模块契约、真实依赖和必要桌面联调 |
| EP-17 | MOD-SDK SDK与CLI | Go SDK→asterctl→Swagger2.0→静态托管→完整页面/路由拆包→接口一致性验证 | 各已交付API | 对应FR-16与模块契约、真实依赖和必要桌面联调 |
| EP-18 | MOD-BUSINESS 独立业务与预设 | 电商独立PG/查询/写操作→保险独立业务→场景知识/工具/Skill→Prompt/Agent预设→Mock LLM→初始化/全场景 | TOOL/KB/SKILL/APPROVAL；业务可独立启动 | 对应FR-17与模块契约、真实依赖和必要桌面联调 |
| EP-19 | MOD-DELIVERY 部署与工程质量 | 增量CI/契约→Go/前端/PG/Docker集成→镜像/Compose→runbook/回滚→性能/故障→范围全量复核 | 所有模块分阶段 | 对应FR-18与模块契约、真实依赖和必要桌面联调 |

## 状态和任务规则

状态：Draft→Ready→In Progress→Review→Changes Requested→Ready to Merge→Merged→Verified/Done。Blocked须有原因和解除条件。Closed不自动等于Done，PR草稿不自动等于已交付。
每个ENG任务给场景/范围/接口/文件/依赖/失败语义/测试与前端责任/验收。进入迭代再细化具体签名，避免预写几十个未经实现验证的内部API。
PR引用Issue与FR/MOD/Epic；新接口/依赖/范围变动先修任务/ADR。评审按当前head，CI通过后仍需负责人合并和部署范围验收。
阶段复盘记录已实现、验证范围、未完成、风险、下一任务。不得把生成文件/空目录/PR数量或参考源码能力计为产品完成率。

## 最终范围核验

[固定清单](../reference/BASELINE.md)冻结488个参考文件与根级CI，全部有模块归属；完成验收另需参考路由/页面/迁移语义到目标实现/测试映射。结构清单只防漏项，语义对齐和外部验收不能被自动化清单替代。
