# 产品需求：AsterFlow Agent平台

版本0.1，2026-11-14。状态：待需求/架构评审；实现尚未开始。产品与技术负责人：yz1qaq。技术基线见[BASELINE](../reference/BASELINE.md)。

## 背景与目标

建立可由业务团队配置、发布、调用和治理Agent的平台。工程师提供工具/知识与模型配置，设计者组合Prompt/Skill/Agent/Team，业务用户通过网页或渠道处理任务。研发采用明确模块分工、接口契约、自动化验证与PR评审，所有变更可追踪到需求和验收证据。

目标交付覆盖固定技术基线全部已实现行为。本仓库从零验收，每项需求必须关联模块、Issue/任务、PR与验证证据；其他工程的实现不能代替本仓库交付。

## 用户与场景

| 用户 | 主要职责 |
| --- | --- |
| 全局管理员 | 用户与平台级管理，依服务端规则访问空间 |
| 空间owner/admin | 空间成员与配置管理，权限不得跨作用域 |
| Agent设计者 | 版本化Prompt/Skill/Tool/Agent/Team与模型配置 |
| 业务用户 | 自己会话中的流式聊天、查询和受审批保护的敏感操作 |
| 审批者 | 在授权空间/会话范围确认实际调用，查看执行结果 |
| 运维/测试 | 部署、观测、评估、审计与故障定位 |

具体权限矩阵在IAM任务中按参考路由/Service固定，不能只靠前端隐藏按钮。典型场景：电商订单与库存排查→Skill指导→KB查询→调拨审批；保险理赔/风险调查→多工具/知识→人工确认。

## 完整功能需求

### FR-01 身份与空间
JWT登录、用户、空间/成员与角色、服务端授权、资源和私有会话归属。密码哈希保存；身份由认证context提供。验收：两用户/两空间/不同角色的允许与拒绝、撤权、重启持久化；最后owner等约束按参考行为。

### FR-02 Prompt与版本治理
模板/变量/用户提示词、生成配置、不可变版本、环境指针、渲染、diff/rollback、实验与baseline/candidate灰度、流量调整/完成/回滚、缓存失效。验收：发布与新建版本分离，老会话固定配置，两个进程缓存更新一致，界面状态读回真实数据。

### FR-03 模型控制面与Gateway
Provider Account/凭据加密与轮换、Deployment/计价、Model Profile不可变版本、项目/环境绑定、路由/重试/failover/数据分级、同步/流式、调用用量/计费归因、RPM/TPM/月预算、预留与过期释放。验收：超额和并发不越界，错误/取消不泄漏预留，旧会话不改模型配置；模型替身与外部模型质量分开记录。

### FR-04 五源工具及沙箱
Tool/Version注册、JSON Schema、试跑与发布门禁、凭据不回显、REST/internal_sdk/code_execution/mcp_server/a2a五源，统一执行治理与身份版本。独立Sandbox Runner经HTTP调用Docker执行Python/Bash，服务端固定禁网/只读/非root/资源策略。验收：实际协议/容器调用、白名单/重定向/输出上限/取消/并发与敏感拦截，不把Shell当任意宿主机命令执行入口。

### FR-05 Skills
SKILL.md解析、领域/版本/发布、工具/KB引用检查、订阅与Agent固定技能版本、Eino Skill Middleware的L1/L2渐进披露、显式/模型触发、disable-model-invocation/网络要求、激活后工具/KB权限收窄及事件。验收：未触发只有摘要，触发才加载正文；技能不能扩张Agent原有权限；网页可管理并展示触发。

### FR-06 知识库与检索
KB/Document/Chunk/Connector/Job、Markdown目录与来源配置、内容hash/增量同步、Asynq Worker异步摄取、分块/Embedding/PG索引、检索模式路由、全文+向量+RRF及参考重排/Playground、来源引用与KB范围。验收：实际异步进程/PG完成、错误可查、重复同步行为明确、跨空间/未授权KB拒绝；LocalEmbedder与真实语义质量分开。

### FR-07 Agent与会话运行
Agent/Version/环境发布、Prompt/模型/Tool/Skill/KB配置快照、Conversation/Message/ToolCall、Eino ADK ReAct、最大步数、工具历史回喂、同步/SSE/WS、取消/错误、整轮保存、会话并发租约。验收：无工具/两工具/技能/知识路径，事件与消息顺序准确；刷新/重启保留；旧会话固定，失败不伪装完成。

### FR-08 审批与A2UI
敏感调用执行前pending、精确interrupt address和approval/conversation绑定的ADK checkpoint、受控A2UI surface/action、批准/拒绝、Worker定点恢复、执行租约/心跳/完成/失败、重复恢复防护。验收：批准前业务调用为0；批准后只恢复原调用；拒绝不执行；伪造/跨会话/跨空间/重复批准无越权与重复触发。

### FR-09 多Agent Team
Team/Version/环境绑定及成员固定Agent版本；Supervisor使用ChatModelAgent/AgentTool，Pipeline确定顺序执行。验收：Web可配置与运行两模式，输入输出可追踪、权限/取消/错误传播，成员升级不改老Team快照。

### FR-10 Guard
on_input/on_llm_call/on_tool_call/on_output四Hook，注入检测、PII改写/出域控制、动态空间规则、限流/配额与模型分级。验收：真实运行入口挂载，规则启停/越权/并发边界通过，Guard与审批各自职责清楚，日志采集边界明确。

### FR-11 审计、对象存储与维护
操作、工具调用、Skill触发、Sandbox执行与模型调用的结构化事实，用户/空间/会话/审批关联；审计查询/导出、MinIO/S3、日志月分区、定期建分区与成功归档后清理。验收：一次任务可追踪；对象写入失败不删除原分区；跨空间审计不可见。

### FR-12 可观测性
HTTP/Agent/LLM/Tool/Guard/Approval OTel spans与上下文，OTLP/HTTP→自托管Langfuse，Prometheus指标，user/session/release关联及内容采集开关、Observability页面。验收：本地真实观测栈查看父子关系/错误/取消；指标实际增长、不把ID放进高基数标签。

### FR-13 Eval与发布质量
持久化Dataset/Case/Run/Score、确定性/LLM light/LLM full三层Judge、历史/报告、绑定具体Agent版本运行、阈值与CI Gate。验收：好坏样例产生可解释得分与正确退出码，不达标实际阻断；真实模型评估质量单列。

### FR-14 缓存与异步任务
Redis Embedding缓存、模型/维度隔离、Prompt缓存与Pub/Sub失效、幂等/分布式锁、Asynq摄取/审批/渠道/维护任务、任务事实与失败/重试。验收：至少两个进程共享状态，相同幂等键请求回放，异参冲突，过期/取消/重启不重复产生敏感业务副作用。

### FR-15 对外渠道
Webhook签名与重放防护；飞书验证/解密、事件映射、入队、自动回复与失败重试；WebSocket身份/Origin/断连。验收：本地真实协议HTTP/WS与Worker流程、非法输入/签名/重放拒绝；真实飞书账号联调状态另记。

### FR-16 SDK、CLI与接口文档
参考Go SDK、kbotctl、Swagger2.0、REST/SSE/WS约定、静态前端托管与管理端路由/详情。验收：SDK/CLI调用真实本地平台，参数/错误/取消传播，文档与实际路由一致。

### FR-17 独立业务与场景预设
完整电商与保险业务工具及示例数据/知识/技能、coursepreset的Prompt/Tool/Skill/KB/Agent资产、Mock LLM、安装/初始化与演示。业务独立目录/进程/PG库，平台只通过已注册接口调用。验收：独立业务无需平台也可用；受控模型到真实业务/知识/审批贯通，重复初始化安全。

### FR-18 工程交付
配置/健康/就绪/优雅关闭、sqlc/migrations、内存/PG契约、Server/Worker/Runner分进程、镜像/Compose、前端构建/拆包、race/vet/集成/CI、运维runbook及参考性能用例。验收：空环境可按文档部署；测试隔离不破坏日常数据；故障/资源回收有证据；所有模块通过[完成标准](../process/DEFINITION-OF-DONE.md)。

## 非功能要求与验收边界

- 数据：PG为事实源，配置版本不可变，环境为指针；业务幂等与执行租约不宣称外部系统天然exactly-once。
- 安全：JWT/角色/空间/资源归属服务端校验；凭据密文不回显；出站/重定向/沙箱策略在实际执行层生效。
- 可用性：请求取消贯通，失败状态可定位；本轮不承诺生产HA/灾备。并发/租约/恢复测试必须真实执行。
- 性能：按参考用例在固定机器/语料/并发下记录基线，不在未测量时承诺任意P95/QPS。
- 可维护性：明确分层/模块接口/错误cause，SQL入queries并sqlc生成；测试按契约/功能归组，避免镜像实现。
- 可访问性：桌面主要路径、键盘操作/明确错误/加载状态；本轮无需主动手机端测试。
- 来源：遵循参考Apache-2.0来源说明；原版演进项OpenSearch、SSO/OIDC、API Key鉴权、OpenAPI3生成、Kubernetes/HA等不冒充现有参考基线功能。
- MinIO当前是归档/导出能力，不把它扩写成原版已有PDF/Word知识上传。

## 需求追踪与变更

FR→MOD→Epic/ENG任务→PR→测试证据一一关联。完整范围见[追踪表](../reference/TRACEABILITY.md)与[Backlog](../delivery/BACKLOG.md)。新需求经评审进入变更记录；不能以“暂时够用”删除FR。

本次交付仅需求/设计/协作准备，FR-01至FR-18全部为未实现。
