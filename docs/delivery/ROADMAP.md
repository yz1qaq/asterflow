# 按需求组交付计划

状态：负责人2026-11-15确认采用推荐分组。本文是正式交付计划，不是功能完成证据；当前实现以[STATUS](STATUS.md)和实际验收为准。固定完整范围仍为FR-01至FR-18、19模块，技术基线与488文件清单不变。

## 交付单位与节奏

- ENG是可追踪的工程子任务；G是需求组。自ENG-004起，同组默认一个Issue、一条功能分支、一次集中验收和一个PR，不按Store/Service/Handler逐层开PR。
- 004至038共35个子任务，合为G01至G22共22组。G00仅归档001至003已有基础工程记录；001/002已合并，003按现有流程收尾，不改历史。
- “需求日”代表功能包；每天3–4小时，较大组可以跨多个学习时段。组内连续提供相关代码并讲解，整体完成后宣布本组完成，不靠日期压缩范围。
- 学习为首要目标：每组说明本组在系统中的位置、先后调用/数据流、关键代码含义以及为什么采用该设计；用具体输入/状态变化解释，不能把合并交付变成只贴大段代码。普通基础语法按需讲，用户追问时先解惑。
- 人类负责生产Go/SQL与Git提交/推送/合并；助手负责增量React/接口联调、归组测试、文档/运维及AI Review。复用既有React界面，按本组接口启用并联调，不一次搬完全部页面。
- 普通小改动不逐句中断做全量测试；有真实故障立即定位修复，组末集中检查。必要时在同一分支保留中间commit；push更新同一PR，不新增重复PR。组最终Squash合并。
- 只有当前组开始时才细化签名、可改路径、迁移、接口契约、测试/前端与验收。未来组不预写占位实现，不批量创建未经细化的Issue。

## 已交付基础

| ENG | 内容 | 状态 |
| --- | --- | --- |
| ENG-001 | 配置/Gin/healthz/HTTP生命周期 | PR #4已合并 |
| ENG-002 | PG连接/启动装配/readyz | PR #6已合并 |
| ENG-003 | 迁移CLI/IAM七表 | PR #8已合并；验收及流程事实见STATUS |

## 需求组与整体验收

| 组 | ENG | 功能范围 | 关键验收 |
| --- | --- | --- | --- |
| G01 用户与登录 | 004–005 | IAM领域/sqlc与PG Store、密码、管理员初始化、JWT/认证/API、复用React登录和用户页 | 登录/创建用户/权限拒绝/重启回读 |
| G02 空间与权限 | 006–007 | 空间/成员/角色、事务与最后owner保护、可信context、管理API及Web | 跨空间拒绝、最后owner并发保护、成员撤权 |
| G03 模型配置 | 008–009 | 账户/凭据加密轮换、Deployment/计价、Profile版本/环境绑定、Models Web | 凭据不回显、版本冻结、空间隔离、配置读回 |
| G04 提示词治理 | 010–011 | 模板/变量/版本/发布/渲染/首轮用户模板、diff/回滚/实验灰度、Redis与缓存失效、Web | 旧版本不漂移、模板错误清楚、两进程失效一致 |
| G05 Agent与会话 | 012–013 | Agent版本/环境/快照、Conversation/Message/ToolCall、归属/租约/整轮事务、管理与历史Web | 配置固定、用户会话私有、并发turn拒绝、历史持久化 |
| G06 首次聊天闭环 | 014–015 | 真实Gateway/生成参数/路由failover、统一ADK执行、事件与同步聚合/SSE、取消错误、聊天Web | 真实模型可聊、事件顺序/断连取消/保存准确 |
| G07 业务工具闭环 | 016–018 | Tool版本/Schema/凭据/REST及internal SDK/试跑发布/白名单；独立电商PG查询API；Agent工具绑定/事件/历史与Web | 网页能查真实订单/库存并保留工具轨迹；敏感写操作在审批组完成前不开放 |
| G08 沙箱 | 019 | 独立Runner/HTTP Client/Docker策略/并发取消/输出限制、代码工具与Web试跑 | 实际Docker执行，平台不直接持有Docker Socket |
| G09 外部工具协议 | 020–021 | MCP stdio/HTTP、A2A、连接生命周期、工具配置与试跑 | 真实协议调用、取消/断连/进程或会话清理 |
| G10 知识库 | 022–023 | 来源/Markdown/hash/Document/Chunk/Job、Asynq/Worker、Embedding/pgvector/全文/RRF/重排/缓存、Playground与Agent范围 | 异步多进程摄取、重复同步、检索来源与越权拒绝 |
| G11 Skills | 024–025 | SKILL解析/版本/引用/发布/订阅、L1/L2/触发/策略收窄、事件及Web | 摘要到正文渐进加载，工具/KB/网络权限不能扩张 |
| G12 审批闭环 | 026–027 | 敏感拦截/Approval/精确checkpoint/A2UI/决策、Worker定点恢复/租约心跳/幂等、电商敏感写操作 | 批准前零调用，拒绝不执行，重放不重复副作用 |
| G13 多Agent | 028 | Team/Version/环境/成员pin、Supervisor/Pipeline、事件取消与Teams Web | 两模式运行，成员更新不漂移老版本 |
| G14 Guard | 029 | 四Hook、动态规则/注入/PII/出域控制、配置Web | 真实运行链挂载，空间边界与规则启停可验证 |
| G15 模型额度治理 | 030 | RPM/TPM/月预算、并发预留/过期释放、用量计费归因、模型分级联动 | 并发不超额，错误/取消/过期不泄漏预留 |
| G16 审计与归档 | 031 | 操作/工具/Skill/沙箱账本与查询导出、MinIO、月分区及维护Job | 事实关联完整，归档失败不删除原分区 |
| G17 可观测性 | 032 | OTel/Prometheus/Langfuse、内容采集开关、关联标识及Observability Web | 实际观测栈展示调用树、指标、错误与取消 |
| G18 评估 | 033 | Dataset/Case/Run/Score、三层Judge、版本固定、报告/阈值/CI Gate、Web | 坏样例实际阻断发布，结果可追溯 |
| G19 外部渠道 | 034 | WS身份/Origin、Webhook签名重放、飞书验证/解密/入队/回复重试 | 真实本地HTTP/WS/Worker协议；外部租户实测另记 |
| G20 SDK与接口交付 | 035 | Go SDK/asterctl/Swagger2.0、REST/SSE/WS一致性、前端构建拆包与静态托管 | SDK/CLI调用实际平台、错误与取消正确传播 |
| G21 完整业务场景 | 036 | 补齐电商/保险完整接口与独立PG、工具/知识/Skill/Prompt/Agent预设、初始化、可控Mock LLM | 安装幂等，平台到独立业务/知识/审批的完整场景贯通 |
| G22 部署与总验收 | 037–038 | Server/Worker/Runner镜像/Compose/迁移顺序/运维；全量功能/页面/协议/迁移语义、并发故障性能核验 | 空环境可部署，18需求/19模块全映射，不以文件数冒充完成 |

## ENG完整编号

| ENG | 子任务 | 所属组 |
| --- | --- | --- |
| ENG-001 | 基础配置、Gin、healthz和HTTP生命周期 | G00 |
| ENG-002 | PG连接池、启动装配和readyz | G00 |
| ENG-003 | 迁移CLI与IAM初始七表 | G00 |
| ENG-004 | IAM领域、sqlc/PG Store与密码处理 | G01 |
| ENG-005 | 用户创建/管理员初始化/JWT/认证/API与登录用户Web | G01 |
| ENG-006 | 空间/成员业务、事务和最后owner保护 | G02 |
| ENG-007 | 空间上下文/角色鉴权/API与空间成员Web | G02 |
| ENG-008 | 模型账户/凭据轮换/Deployment/计价 | G03 |
| ENG-009 | Profile版本/环境绑定与Models Web | G03 |
| ENG-010 | Prompt模板/变量/版本/发布/渲染/首轮用户模板与Web | G04 |
| ENG-011 | diff/回滚/实验灰度/Redis及跨进程缓存失效 | G04 |
| ENG-012 | Agent版本/环境/依赖快照与管理Web | G05 |
| ENG-013 | 会话/消息/工具历史、归属、租约/整轮事务与历史Web | G05 |
| ENG-014 | 真实Gateway/生成配置/路由failover/模型接口 | G06 |
| ENG-015 | 统一ADK/事件/同步薄适配/SSE/取消保存与聊天Web | G06 |
| ENG-016 | 工具版本/Schema/凭据/REST及SDK执行/白名单/试跑发布/Web | G07 |
| ENG-017 | 独立电商module/进程/PG及查询API | G07 |
| ENG-018 | Agent工具绑定/ADK适配/事件持久化和网页业务查询 | G07 |
| ENG-019 | Runner/Client/Docker/代码工具/策略/取消并发及Web | G08 |
| ENG-020 | MCP stdio/HTTP及配置试跑/生命周期 | G09 |
| ENG-021 | A2A适配与配置/协议验证 | G09 |
| ENG-022 | KB来源/文档/Job/Markdown增量/分块/Asynq Worker | G10 |
| ENG-023 | Embedding/PG索引/混合检索重排/缓存/Playground/Agent范围 | G10 |
| ENG-024 | SKILL解析/版本/引用/发布订阅/Web | G11 |
| ENG-025 | L1/L2/触发/策略收窄与事件 | G11 |
| ENG-026 | 敏感拦截/Approval/checkpoint/A2UI/决策Web | G12 |
| ENG-027 | 恢复Worker/执行租约心跳/幂等及电商敏感写操作 | G12 |
| ENG-028 | Team版本、Supervisor/Pipeline与Web | G13 |
| ENG-029 | Guard四Hook/动态规则/PII注入及出域控制/Web | G14 |
| ENG-030 | 模型额度/预算/预留/过期释放/用量计费归因 | G15 |
| ENG-031 | 审计事实/查询导出/MinIO/月分区维护 | G16 |
| ENG-032 | OTel/Prometheus/Langfuse与观测Web | G17 |
| ENG-033 | Eval三层Judge/报告/Gate/CI及Web | G18 |
| ENG-034 | WS/Webhook/飞书及Worker回复重试 | G19 |
| ENG-035 | Go SDK/asterctl/Swagger/前端构建与静态托管 | G20 |
| ENG-036 | 完整电商保险业务、场景预设/初始化/Mock LLM | G21 |
| ENG-037 | 镜像/Compose/迁移启动顺序及运维 | G22 |
| ENG-038 | 全部功能/路由/页面/迁移语义及故障性能核验 | G22 |

## 依赖与完整范围约束

按表顺序推进；共享基础设施随首次需求接入：Redis在G04，Asynq/Worker在G10，后续审批/渠道/维护复用，不为连接配置另开细小需求组。
G05固定依赖版本；Tool/Skill/KB在其组内补入装配与运行验收，不提前制造假实现。G07先贯通查询；敏感写操作由G12的审批和幂等闭环开放，没有临时放行路径。
G15保留完整模型额度/预留/过期治理；G16保留操作/工具/Skill/沙箱事实与归档；G21补齐两个业务系统及全部预设，不能以早期单个工具演示替代它们。

初始七表不等于IAM全模块完成；源26组迁移按表/索引/约束/修复语义在对应模块分批落实，G22逐项复核。所有目标路由、页面、SDK/CLI、五源工具和协议都必须有实现与验收映射。模板/模拟模型、真实PG/协议/Docker和外部质量分别记录。

## 避免过渡实现

按[ADR-0004](../architecture/adr/0004-feature-groups-and-runtime-scope.md)执行：生产直接PG与统一ADK；跳过无实际调用方的内存业务Store、假回答/假流式、重复执行循环及不需要的非事务fallback。必要测试替身/Mock LLM保留为显式验收夹具。
同步聚合仍供Eval/Team/渠道/SDK/CLI使用；SSE/WS直接消费同一事件流。模型框架要求的Generate/Stream和审批暂停/错误/取消语义必须保留，不因网页默认流式而删掉正式能力。

## 组完成标准

需求组的生产代码/迁移、前端联调、相关真实依赖与错误边界、文档/依赖图、必要CI/按head Review共同完成，才由负责人决定合并。当前组完成前不开始下一组代码；功能量确需进一步拆分时，基于实际可验收边界解释，不退回每函数开PR。
