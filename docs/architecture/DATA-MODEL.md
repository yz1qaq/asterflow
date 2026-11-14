# 数据模型与一致性

目标设计，尚未建表。SQL与迁移由所属模块任务交付，数据模型需结合API契约共同评审。

| 领域 | 主要实体/关联 | 关键约束 |
| --- | --- | --- |
| IAM | User、Workspace、WorkspaceMember、Role/Permission、参考APIKey模型 | 邮箱唯一、成员联合键、最后owner保护；有APIKey表不等于已实现APIKey鉴权 |
| Prompt | Prompt、Version、环境指针、实验/灰度 | 首版原子创建、版本不覆盖、环境引用归属一致 |
| 模型 | ProviderAccount、Deployment、Profile/Version、项目环境绑定、用量预留/日志 | 凭据密文、版本冻结、预算/预留原子计量和过期治理 |
| 工具 | Tool、Version、试跑记录 | 名称作用域、每Agent每Tool一个版本、同版本试跑发布门禁 |
| 技能 | Skill、Version、Subscription | SKILL解析/引用、发布前空间归属、快照绑定具体版本 |
| 知识 | KB、Document、Chunk、Connector、IngestJob | 来源/hash/增量、片段顺序、向量维度、同KB重建幂等 |
| Agent/会话 | Agent/Version、Conversation、Message、ToolCall | 会话固定版本/运行配置；有序user/assistant/tool历史与租约 |
| 审批 | Approval、Checkpoint | approval/conversation/interrupt绑定；决策状态与执行状态独立 |
| Team | Team/Version、成员Agent版本 | 成员空间一致、Supervisor/Pipeline快照不漂移 |
| 治理 | GuardRule/Quota、Audit、ToolInvocation、SkillTrigger、SandboxExecution | 空间/Actor/Run关联；可读范围受权限限制 |
| 评估 | Dataset、Case、Run、Score | 固定执行版本，历史持久化并可回溯 |
| 业务 | 电商订单/库存/写操作记录；保险理赔/风控状态 | 独立PG库、不使用平台表外键、业务幂等与事务自洽 |

## 标识和版本

ID为全局唯一UUID；Version为同一主记录内的展示序号，不能混为一谈。ToolID/ToolVersionID/Name分别表示工具主记录/不可变版本/模型调用名。
dev/staging/prod是可变引用，不是复制三份业务实体。Conversation保存实际版本和运行配置，重新发布不改旧记录。
SnapshotJSON保存装配配置，不保存动态执行栈；Checkpoint保存ADK中断状态，不替代消息历史。

## 审批与租约

Approval.status为pending/approved/rejected；ExecutionStatus单独表示执行等待/运行/成功/失败等参考状态。批准不等于执行成功。
会话turn_token/lease_until与审批execution_token/lease_until分别保护各自操作；更新必须核对当前token及条件，并检查受影响行数。租约过期可重取，不依赖前端禁按钮保证正确性。
批准者取认证身份，发起者取会话用户；不能信任请求body自报actor/workspace。状态转换与重复处理语义在审批契约中测试。

## 迁移和SQL

平台从000001开始独立演进，不复制旧工程已用编号。参考26组迁移按表/索引/约束/修复语义映射，不按文件个数计算完成率。
所有可生成查询写internal/infrastructure/postgres/queries并sqlc生成，业务组合事务用q.WithTx(tx)的同一事务查询，不能混入独立连接写入。
up/down、真实PG默认值/约束/并发、重启回读在隔离schema/数据库验证。涉及归档删除必须先确认对象存储成功，失败保留原数据。

## 数据分类

PG保存业务事实；Redis为缓存/锁/任务队列；MinIO用于归档/导出；Langfuse为观测副本。任何缓存/观测成功都不能代替PG事实确认。
本机测试使用虚构账号/业务数据与独立schema，不把生产/个人数据和密钥放入Git。
