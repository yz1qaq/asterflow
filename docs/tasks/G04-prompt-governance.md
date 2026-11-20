# G04 开发任务单：提示词治理

范围：ENG-010 + ENG-011，FR-02 / MOD-PROMPT / EP-04。本组全部在 AsterFlow 实现和验收。上组 PR #14 已合并，基线为 main 上的 b85be3818a44d26180c7efd10da08f33b3679915。

## 场景和产品要求

空间编辑者创建「订单核验」提示词，预览变量渲染，再将不可变版本发布到 dev、staging 或 prod。线上出现问题时，可以对比版本并退回旧版本。灰度可以按用户稳定分桶，在 baseline 与 candidate 间分流，支持调整比例、完成上线、回滚和操作追踪。只读角色可看允许的配置与结果，服务端鉴权决定写权限。旧会话保存实际版本，后续环境指针变化不影响已创建会话。

## 实施顺序

1. 迁移和类型：Prompt 主记录、不可变版本、环境指针。生成参数与可选 Model Profile Version 随版本冻结；变量 Schema 保留原文。确保指针只能引用当前 Prompt 的版本。
2. 真实 PG Store 与提示词编译：SQLC 查询、主记录/v1/dev 原子创建、并发追加版本、模板语法和必填变量校验、渲染、Hash、Token 估算、模型版本归属检查。
3. API 与页面：创建/列表、版本历史、按环境及指定版本解析和预览、发布/回滚、diff、首轮用户提示词模板的变量契约。G05–G06 接入实际会话固定与聊天，保留模板功能。
4. 实验和 rollout：多变体实验、baseline/candidate 灰度、稳定分桶、改流量、完成及回滚；一个 Prompt 在同一环境至多一个 active 实验，操作事件保留历史。
5. Redis 与完整验收：本地编译缓存、发布提交后的跨进程通知与重读、两个进程一致性；前端读取真实状态，跨空间、错误模板、缺变量和发布失败均有回归。集中检查，走一个需求组 PR。

## 架构取舍

React → Gin/Auth/Workspace → prompt.Service → PostgreSQL/sqlc。Redis 通知用于缓存失效，PostgreSQL 保存版本和发布事实。沿用固定参考的冻结语义，把一对一的 prompt_version_configs 内容合到 prompt_versions，避免额外插表和回读连接。以真实事务代替创建失败后删除主记录的补偿。参考的 prompt_usage_logs 语义保留在后续真实使用记录交付中映射；不会把空日志表当作使用记录功能。

分支建议 feat/ENG-010-011-prompt-governance。生产 Go/SQL 由后端负责人编写，助手负责测试、React、验收和 AI Review。日学习进度与需求组验收分别记录。
