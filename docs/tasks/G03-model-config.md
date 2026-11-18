# G03：模型账户、部署与配置版本（ENG-008+009）

关联[Issue #13](https://github.com/yz1qaq/asterflow/issues/13)，FR-03 / MOD-MODEL / EP-03。
状态 Ready for PR：模型配置全链路、开发库迁移、正式回归与 G03 工作区复审已通过，尚未创建 PR。
前置G02已由[PR #12](https://github.com/yz1qaq/asterflow/pull/12)合并为ad533f7cbc8082353fa51e2ec27a05b7e4c171bf，本地main已同步。
负责人创建分支feat/ENG-008-009-model-config；本组代码与文档仍在同一需求分支，当前未提交，不单独提交main。

## 本组交付

提供真实PG持久化的模型账户、部署与计价、逻辑Profile及不可变版本、空间与环境的版本绑定，并增量复用Models管理界面。一个需求组一个Issue/分支/PR，不按小函数重复提交。

| 对象 | 保存内容 | 关系 |
| --- | --- | --- |
| ProviderAccount | 供应商类型、base URL、API Key密文与状态 | 属于Workspace |
| ModelDeployment | 模型名、账户、超时、重试、地域和计价 | 指向ProviderAccount |
| ModelProfile | 逻辑模型方案名称和描述 | 拥有多个版本 |
| ModelProfileVersion | 主Deployment、备选列表、数据分级上限 | 固定引用Deployment |
| ProjectBinding | Workspace + env选择哪个Profile版本 | 指向不可变版本 |

G06再接真实Gateway与聊天执行；G15实现配额账本与强制执行，本组只保存配置所需字段；G21补业务预设。本组不调用付费模型，不把配置成功当真实推理验证。

## 文件与分工

- 负责人编写生产Go/SQL，创建分支、commit/push/PR及手动合并；助手写归组测试、React/API联调、运维文档和AI Review。
- platform/modelconfig/crypto.go负责Cipher；模型记录/Store/Service按职责组织，不复制生产内存Store或可选事务fallback。
- 查询维护在infrastructure/postgres/queries并由sqlc生成，持久化直接PG。Profile与首版本应使用必需原子事务。
- Gin接入层负责JSON/可信身份与空间，Service验证参数与引用归属；客户端不授予actor/Workspace权限。
- 账号API不返回API Key或密文，仅返回has_api_key；解密只供后续网关内部使用。
- 加密主密钥通过独立持久化配置提供，不能用公开默认值或误用JWT签名密钥。SHA-256规范化AES密钥长度，不会把弱口令变强；配置阶段使用足够长的随机值并保持重启一致。
- API Key轮换是更新供应商凭据，不等于更换应用加密主密钥；主密钥轮换须另有历史密文迁移策略。
- 前端只增量接入已完成API，不提前展示可调用但未实现的聊天/配额能力；不做手机端验收。

## 数据库语义映射

000003至000005已由负责人应用，开发库本轮只读核对为version=5 dirty=false，环境绑定表、主外键和约束均存在。SQLC 1.27.0独立重新生成与提交候选文件一致。
本组需要固定参考000008的providers基础、000013的模型控制面子集及000023的Deployment计价字段/非负约束。
Prompt、调用日志、配额预留账本及过期字段在对应需求组补全，并保留完整26组迁移语义追踪。

## Profile 与环境绑定联调

1. 负责人保存四个Profile Handler与workspaceRoutes，助手将HTTP用例合并到既有modelconfig_test.go，正式PG/race通过（6.074s）。
2. Models新增模型方案页签，支持当前空间列表、方案/v1原子创建、版本列表和追加，主备部署从当前空间部署选择。API适配与测试按现有模块归组，新增组件负责该页签。
3. 真实隔离Go/PG桌面1280x720验证创建v1、追加v2、刷新回读、重名保留输入、切空间后不显示旧方案和无部署前置提示；57项Web测试及构建通过。viewer读写限制本轮由正式HTTP验证，未额外声称viewer浏览器验收。

4. 已接通空间全版本列表、按环境查询和保存。桌面环境验证 dev 与 prod 保存及刷新后独立回读、staging空态、计价校验、空间切换和viewer只读。

5. 预审修复通过：Base URL 查询参数拒绝、改价三项完整提交、后台权限刷新保留表单，以及两条写入通过同一个空间行锁协调。真实 PostgreSQL 并发测试与 HTTP、桌面 409 回归通过。

下一步使用[G03 PR 正文](../delivery/G03-PR.md)创建同组 PR，再按实际 head SHA 检查远程 CI 和 Review。详见[G03验证记录](../delivery/G03-VERIFICATION.md)。

## 验收

- [x] 负责人保存加密器，正式归组测试通过且无密钥/明文回显。
- [x] 000003–000005的隔离迁移、开发库版本和sqlc可重复生成已核对。
- [x] 账号创建/轮换/查询、Profile原子初始版本、版本冻结和环境绑定通过隔离验证。
- [x] 跨空间引用/越权/非法输入/故障与事务回滚通过后端回归。
- [x] Models桌面管理流程真实联调通过。
- [x] 同组完整 make check 及独立 AI 工作区复审通过。
- [ ] PR 实际 head 的远程 CI 与 AI 复审通过后，由负责人手动合并。
