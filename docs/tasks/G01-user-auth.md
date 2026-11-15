# G01：用户与登录（ENG-004+005）

关联[Issue #9](https://github.com/yz1qaq/asterflow/issues/9)。状态In Progress；User、5条SQL、生成代码和用户PG Store已保存；隔离PG验证通过；用户创建/密码/管理员Service及JWT登录/验证已保存并测试，Gin请求鉴权已保存并通过归组测试；三个用户Handler和公共解码/分页已保存并通过归组测试；路由/启动配置已由负责人保存，React对应页面已复用并桌面联调通过；集中make check通过，独立AI工作区预审无已证实P1/P2；等待负责人提交PR及最终head门禁。按[ROADMAP](../delivery/ROADMAP.md)一个需求组/分支/PR，组内分批讲解并集中验收。

## 学习目标与分工

目标是在了解数据与调用流程的基础上亲手完成后端，不只复制代码。每批解释系统位置、输入输出、关键语句、设计原因与运行过程；有疑问先按代码顺序解惑。
负责人写生产Go/SQL并操作Git；助手准备生成配置/测试/React/文档/运维。首轮已有sqlc.yaml与make sqlc-generate，工具固定1.27.0；作者已保存SQL与业务User，生成结果已核对并编译；UUID已按固定参考锁为1.6.0。
ENG-003已由PR #8合并，迁移000001和开发库version1 clean已验收；本组不改七表结构，不提前做G02空间业务。

## 目标链路

构建时：migrations + queries/iam.sql → sqlc → pgstore模型/Queries。
运行时：Gin → iam.Service → iam.Store/PG适配 → pgstore.Queries → PG。
返回时：PG存储行 → domain.User → API JSON，密码哈希不对客户端序列化。

生成文件不手改，不额外生成无消费方Querier接口；db.go中兼容pool/tx的执行契约仍由sqlc维护。领域层不依赖PG驱动，行转换由Store承担。用户Role是全局admin/member，空间角色在G02。

## 代码与接口范围

- 作者新建internal/domain/iam.go：domain.User，ID/Email/Name/Role/PasswordHash/Status/CreatedAt/UpdatedAt，哈希json标签为-。
- 作者新建internal/infrastructure/postgres/queries/iam.sql：GetUserByEmail/GetUserByID/CreateUser为one，ListUsers为many，SetUserRole为exec，沿用000001字段与数据库时间。
- 生成internal/infrastructure/postgres/sqlc，包名pgstore，pgx/v5，非空UUID用google/uuid v1.6.0，非空timestamptz用time.Time，列表空结果为[]。
- platform/iam的消费者Store、PG适配/UUID与行转换已由作者保存；后续继续用户/密码/管理员/登录/JWT，不先写生产内存Store或没有实现的空间方法。
- Gin接入登录与受认证/全局管理员保护的用户创建和列表，Service只接标准context。
- 登录沿用POST /api/v1/auth/login；管理员创建用户沿用POST /api/v1/auth/register；GET /api/v1/users为管理员分页列表。登录响应token/expires_at/user，不返回hash/密钥，不提供公开自助注册或新增单端登录策略。
- 助手按API完成度增量复用React登录/用户页，改品牌与会话存储名；空间页随G02接入。

## 批次与验收

1. [x] 已切入G01分支，User/5条SQL/sqlc产物已保存并最小编译通过；本轮继续Store适配。
2. [x] 作者完成用户Store/PG适配与数据库错误/时间/ID映射；助手新增一个postgres_store_test.go，真实随机schema下5组场景和race通过。
3. [x] 用户创建/密码/管理员初始化已由作者保存；助手新增一个service_test.go，输入校验、真实PG哈希/规范化、初始化幂等/并发及race通过。作者已保存auth.go；助手将真实PG登录/账号拒绝、JWT签名/有效期/算法/身份边界验证合入service_test.go，IAM包race通过。作者已保存Service.GetUser与Gin Auth/RequireGlobalAdmin；助手新增一个middleware/auth_test.go，9类请求/race通过（真实JWT、显式存储测试夹具）。真实业务路由已注册，真实HTTP/PG权限链路已验证。
4. [x] Service.ListUsers、公共解码/分页、三个Handler、构造函数、Gin路由/配置/启动装配均已保存；对应React登录/用户/系统状态已迁入，真实HTTP+PG及桌面浏览器验收通过，测试调用已统一适配。
5. [ ] 本地make check（含真实PG、全部Go包race/vet/build、前端18测试/类型检查/构建、文档）通过；独立AI工作区预审无已证实P1/P2。仍需作者提交同一分支PR，确认远程docs/go/web并按最终head Review后决定合并。

验收重点：重复邮箱/非法输入、哈希保存与不回显、管理员初始化幂等、登录成功与拒绝、token与可信身份、普通用户不能访问全局管理员接口、PG重启回读与Web刷新/退出。具体签名与测试随当前批次明确，不为未来模块预写假实现。

## 当前Git衔接

负责人已切入feat/ENG-004-005-user-auth并带入计划/配置；当前组继续在同一分支累计实现。未替负责人提交/推送，不在每个函数后开新PR。

## 当前启动接入实现

- Config新增JWTSecretKey及AutoseedAdmin/Email/Password。ASTERFLOW_JWT_SECRET_KEY必填且至少32字节、不能全空白；ASTERFLOW_AUTOSEED_ADMIN默认false，显式开启时邮箱/密码必填。
- 环境名沿参考统一前缀：ASTERFLOW_AUTOSEED_ADMIN_EMAIL/PASSWORD。正式示例不提供固定密码；本地已忽略的0600 .env已准备随机JWT密钥、随机管理员密码，并显式开启本地bootstrap。已有同名值保持不变；Go仍不自动读取.env。
- main复用一个pool，构造PG Store和IAM Service；初始化管理员有独立10秒超时并继承启动信号ctx，成功后才启动HTTP。角色/密码策略仍由Service处理，不在Config复制业务校验。
- NewRouter接收checker和IAM Service，注册公开login及Auth/RequireGlobalAdmin保护的register/users；增加Gin Recovery。原有健康/生命周期测试调用已适配；实际Go程序、真实HTTP/PG和桌面浏览器已验收。
- 参考默认开发管理员密码改为显式本地配置，避免公开固定凭据；初始化功能与幂等/角色语义保留。
