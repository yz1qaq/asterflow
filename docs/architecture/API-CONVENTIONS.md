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
