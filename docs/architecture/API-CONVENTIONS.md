# 接口与事件约定

状态：目标规范，具体接口随模块任务评审并锁定；当前健康与鉴权已有实现，业务接口按任务接入。优先保持固定参考的资源、JSON字段和事件语义，产品品牌不改变调用契约。

## HTTP与认证

- 业务资源位于/api/v1；/healthz仅进程存活，/readyz报告必要依赖。SSE/WS和渠道入口按参考路由单独注册。
- JWT使用Authorization: Bearer；空间请求使用X-Workspace-ID。服务端认证/角色/空间/资源检查是权限依据。
- user_id/actor/归属空间由可信context和关联记录确定；body字段不授予访问权。资源URL中的workspace参数和当前空间需明确验证。
- JSON body限制大小、Content-Type/字段类型/未知字段/尾随数据按当前任务约定；拒绝结构错误后不产生业务写入。
- 成功响应沿各资源契约（对象/数组）；不强行套与参考冲突的全局envelope。详情/列表/空数组/分页在模块API设计中给出示例。
- HTTP状态按失败类别：400参数、401认证、403权限、404资源、409状态/版本/并发冲突、429限流/配额、500内部；具体参考已有接口差异记录在任务，不随意改客户端。
- 公开错误简明，内部保留cause/request_id，不回显密钥、完整SQL、认证Header或敏感工具响应。

## 幂等和并发

Idempotency-Key标识一次业务意图，同键同参数可回放，变参冲突。它不能替代业务库自身事务约束，也不代表同账号只能发一个请求。
会话/审批并发使用服务端租约；前端禁用发送/确认只是交互保护。任务重投/重试必须考虑相同tool_call_id、业务幂等键和审批执行token。

## 删除 Agent

`DELETE /api/v1/agents/:agent_id` 使用 Bearer Token 和当前工作空间，无请求体。只允许空间 Owner 和管理员操作。成功返回 `204 No Content`，非法 UUID 返回 `400`，无操作权限返回 `403`，Agent 不存在或不在当前空间返回 `404`，任一关联会话仍有有效租约时返回 `409 agent_conversation_running`。

事务先锁 Agent，再锁其会话。确认可以删除后，依次删除关联会话、环境绑定、Agent，沿现有外键清理消息、工具记录和 Agent 版本。单步失败时整笔回滚。

## 删除会话

`DELETE /api/v1/conversations/:conversation_id` 使用 Bearer Token 和当前工作空间，无请求体。具有会话写权限的成员、编辑者、空间 owner/admin 可删除本人会话，成功返回 `204 No Content`。UUID 无效返回 `400`，未认证返回 `401`，无空间权限或为 viewer 返回 `403`，空间内不可见或不存在的会话返回 `404`，本人会话持有有效轮次租约时返回 `409`。

删除语句在写入时同时检查空间、会话创建者和租约有效期。PostgreSQL 外键级联删除会话消息和关联的工具调用。浏览器删除当前会话后返回新会话页面并刷新列表。

## 供应商试跑与配置删除

- `POST /api/v1/model-accounts/:account_id/test` 接收 `{"model_name":"deepseek-flash"}`。使用该账号保存的凭证直接发起一次、最多 20 秒的非流式生成，成功返回 `model` 和 `latency_ms`。不跟随重定向或切换备用部署。失败只返回清理后的分类和上游 HTTP 状态，余额不足对应 `provider_balance`。
- `DELETE /api/v1/model-accounts/:account_id` 永久删除未被模型部署使用的供应商账号。事务先锁账户，再确认没有部署引用，避免触发历史 schema 的级联删除；有引用返回 `409 provider_in_use`。
- `DELETE /api/v1/prompts/:prompt_id/versions/:version_id` 软删除草稿。v1、发布与回滚历史、灰度实验、Agent 快照和会话均保护已有版本引用。软删除、发布及新引用的最终写入使用数据库行锁；已软删除的版本不出现在版本列表且版本号不复用。引用冲突返回 `409 prompt_version_in_use`。删除成功返回 `204`。
- 三个操作使用认证身份和可信工作空间，仅 owner/admin/editor 有权写入。诊断请求只接受模型名，提示词删除和账号删除无需请求体。

## 同步、SSE和WS

同步返回终态，流式由事件驱动逐段输出。AgentEvent的枚举、payload、终态顺序在Engine/SSE任务中依据固定实现定义；不得仅靠任意字符串让前后端猜测。
契约必须覆盖run开始、answer增量、tool开始/结果、skill触发、await_approval/A2UI、error与结束。工具结果是tool message，与user/assistant同级；assistant tool_calls引用唯一call_id。
成功终态后历史回读，不重复追加完整answer；取消/断连/失败不能当成功。前端切会话/空间时丢弃迟到响应，停止请求传播至模型/工具。

## A2UI与外部渠道

A2UI只允许受控Catalog/组件/action，action必须带可核对的审批/会话归属，按钮点击后真实API再次鉴权。未知组件/伪造动作按拒绝处理。
Webhook用独立签名/重放验证；飞书验证/解密和回复只经后端配置；WS同时校验身份和Origin。渠道不会绕过业务资源权限。

## 工程师交付

每个模块PR必须同步：请求/响应示例、字段与默认值、失败状态、认证/空间规则、分页/幂等/取消语义、Schema/OpenAPI变化和前端联调证据。接口先达成一致再并行开发；Mock协议必须标注，不能冒充后端实现。

## G01 用户接口契约（当前实现批次）

本节锁定接入目标；Handler与路由的实际保存/验收以STATUS为准。G01使用JSON错误对象 `{"error":"简明信息"}`，成功直接返回参考资源对象/数组。

| 接口 | 权限 | 成功响应 |
| --- | --- | --- |
| POST /api/v1/auth/login | 无需已有token | 200，token/expires_at/user |
| POST /api/v1/auth/register | Auth + 全局admin | 201，User |
| GET /api/v1/users | Auth + 全局admin | 200，User数组，空结果为[] |

- 创建用户body只接受email/password/name；登录只接受email/password。严格JSON解码、最多1 MiB、拒绝未知字段与尾随第二个值；不额外按Content-Type拦截，保持固定参考解码行为。
- 用户列表limit默认50、允许1至200；offset默认0、允许0至2147483647。查询值为空沿默认值，非法值400。
- 无效凭据/停用账号401；缺失或无效token401；普通用户访问管理员接口403；邮箱冲突409；已知输入错误（含bcrypt超过72字节）400；数据库/内部故障500且不回显底层原因。
- User密码哈希不序列化。当前角色/账号状态由请求期数据库记录决定；body不能指定角色获得权限。
- 相比参考的用户API分散文件，G01将登录/创建/列表合并到一个v1/iam.go，共享IAMHandler；Gin响应替代标准库writeJSON，公共解码/分页保留在http.go。登录数据库异常按500、邮箱重复按409分类。
