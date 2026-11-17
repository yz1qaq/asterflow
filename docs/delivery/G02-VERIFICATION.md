# G02 增量验收记录

日期2026-11-16；需求[Issue #11](https://github.com/yz1qaq/asterflow/issues/11)与[G02任务](../tasks/G02-workspaces.md)，当前分支feat/ENG-006-007-workspaces，尚未提交本组。

## 已保存与已验证

负责人已保存000002_workspace_rbac up/down、Workspace/WorkspaceMember类型、四条空间SQL并生成。
助手扩展现有cmd/migrate/main_test.go，未新增按函数拆分的测试文件；在随机schema运行make test-migrations，全部通过并包含race。
旧000001契约测试固定只加载初始迁移；本组新增1→2→1→2、回退两版/重建与非法历史角色dirty场景，避免未来增加迁移时错误修改旧版本期望。

实际验证内容：
- users允许admin/member，workspace_members允许五种角色；非法角色SQLSTATE23514。
- 2→1后七表及已有用户/成员数据保留，新CHECK和索引移除；修正非法值后可重新升级。
- 重复up无数据改变；完整两版down只在临时schema运行，之后可重新建立v2。
- 已存在非法角色时升级拒绝并保留dirty，原始记录不被静默改写。
- 固定sqlc1.27.0重新生成全部已保存文件，逐文件SHA256一致。

## 开发库应用

先隔离验证，再运行make migrate-version / make migrate-up / make migrate-version：
version=1 dirty=false → migrations applied → version=2 dirty=false。
独立只读实查public.schema_migrations及pg_constraint/pg_indexes确认两个CHECK和workspace_members_workspace_role存在。未在开发库执行down。

## 当前边界

角色迁移不是权限Service或成员并发保护已完成。读取Store已由负责人保存，新增一个postgres_workspace_store_test.go归组验证5类场景：空数组、nullable parent与副本、同时间稳定分页、成员过滤且不继承父空间、错误与取消；IAM包含既有用户/登录测试的race通过。共享IAM测试schema应用000001与000002。成员Upsert与空间+owner创建事务也已保存并验证；成员JOIN读取也已由负责人保存；空间基础Service已保存并验证；成员管理/并发保护/API/React与全组CI/Review尚未完成。

## 创建事务的预验证与正式落地

临时目录添加UpsertWorkspaceMember SQL并用固定sqlc生成，给PostgresStore保留同一连接池，再通过Go事务组合CreateWorkspace和UpsertWorkspaceMember。
候选代码已编译；随机隔离PG下验证创建空间/owner一起提交、owner外键失败回滚空间且调用方对象不变、非法输入/重复ID拒绝。测试通过且包含race。
预验证后负责人已保存正式SQL与事务方法（位于postgres_store.go）；助手将3组原子创建用例合入既有postgres_workspace_store_test.go，对正式方法复验通过：空间与owner同提交、owner外键失败回滚且输入不变、非法输入/重复ID拒绝；空间读取5组回归也通过，均含race。成员读取代码随后已由负责人保存（位于service.go），助手将3组用例合入既有空间测试文件：JOIN姓名/邮箱及改名回读、空间角色/空数组、缺成员/非法UUID/取消。与空间读取及原子创建一起真实PG/race通过。

## 空间基础Service正式落地

候选workspace.go在临时目录真实PG/race通过：创建空间并自动owner、普通用户按成员过滤、全局管理员可见全部且不插入虚构成员、有效空间角色、空名称/不存在或停用账号拒绝、数据库取消cause保留。
负责人已保存workspace.go并将成员读取Store整理至postgres_workspace_store.go。助手新增workspace_service_test.go归组测试，正式实现执行TestWorkspaceService真实隔离PG/race通过；activeUser统一检查账号状态。

## 下一批成员管理预验证（生产实现待保存）

临时副本追加LockWorkspace/CountWorkspaceOwners并用固定sqlc生成，验证必需的UpsertWorkspaceMemberGuarded事务Store与成员管理Service候选。真实隔离PG/race通过：owner添加成员、空间admin管理非owner但不能赋予/修改owner、viewer拒绝、非法角色/UUID/不存在用户拒绝、全局admin也不能降级最后owner，以及两个owner并发降级恰有一个成功、另一个ErrLastOwner且最终保留一个owner。事务显式ReadCommitted，锁空间后再读目标与owner数量。候选已编译；这些生产Go/SQL尚未写入正式工作区，未实现成员删除/API/React。候选测试暂保留于临时副本，待负责人保存实现后归入正式测试。

## 成员管理正式落地与删除候选

负责人已保存LockWorkspace/CountWorkspaceOwners SQL及生成产物、UpsertWorkspaceMemberGuarded事务Store和成员管理Service。助手将TestWorkspaceMembershipManagement归入已有workspace_service_test.go，对正式实现执行TestWorkspaceService与TestWorkspaceMembershipManagement，真实隔离PG/race通过，包含权限矩阵和两个owner并发降级保留一位。

下一批删除候选仅在临时副本追加DeleteWorkspaceMember SQL、必需Store接口及事务实现和Service入口，沿用同一空间行锁与ReadCommitted。TestWorkspaceMemberDeletion与既有成员管理测试真实PG/race通过：viewer拒绝、空间admin不能删除owner、全局admin不能删除最后owner、非法UUID/不存在成员、删除关系保留用户、两个owner并发删除及删除与降级混合并发。正式删除代码尚未保存，未开展G02 API/React验收。

## 成员删除正式落地与首批Gin候选

负责人已保存DeleteWorkspaceMember SQL/生成产物、DeleteWorkspaceMemberGuarded事务Store与Service删除入口，Upsert方法已整理至postgres_workspace_store.go。助手将TestWorkspaceMemberDeletion归入已有workspace_service_test.go，正式实现执行TestWorkspaceService/TestWorkspaceMembershipManagement/TestWorkspaceMemberDeletion真实隔离PG/race通过，覆盖权限、保留用户账号、并发删除及删除与降级混合并发。

临时副本新增Gin的GET /me、GET /workspaces、POST /workspaces候选，使用Auth保护普通登录用户。真实HTTP/隔离PG/race预验证通过：登录、可信身份、缺token拒绝、普通用户创建空间自动owner、普通用户只看自己空间、全局admin可见全部、非法分页/空名/伪造user_id请求体拒绝。候选生产Go未写入正式工作区；G02成员HTTP/Workspace中间件/React与桌面验收未完成。

## 首批HTTP与空间React正式验收

负责人已保存GET /me、GET/POST /workspaces与DELETE /workspaces/:workspace_id/members/:user_id。助手将TestWorkspaceHTTP归入router_test.go，HTTP fixture同时应用000001/000002；正式用户HTTP回归与空间HTTP测试真实隔离PG/race通过。覆盖可信身份、缺token、自动owner、普通/全局空间作用域、分页/空名/未知字段、成员删除204、无权403、不存在404、最后owner409与非法UUID400。

助手复用此前React的列表/新建Modal/分页与会话检查模式，新增WorkspacesPage并接入导航与API，先提供已接通的列表和创建。22项前端测试、tsc与Vite构建通过；共享chunk约562kB的既有非阻断项仍在。

桌面浏览器使用正式API代码的临时测试实例与隔离PG schema：未登录跳登录、普通用户登录可见工作空间导航、空名称校验、成功创建且自动刷新列表、刷新页面保留记录、退出后访问空间页重定向登录均通过。临时服务已停止，schema及测试账号/空间由test cleanup清理；没有手机端测试。

下一步负责人保存成员列表与写入GET/PUT Handler/路由，随后补成员Web及Workspace中间件；不将本批空间Web验收当G02整体完成。

## 成员GET/PUT错误响应回归（待修复）

负责人已保存成员列表和角色写入Handler/路由。助手在既有router_test.go追加TestWorkspaceMemberErrorResponses，真实隔离PG/race复现列表无权、写入非法角色、写入无权3个失败：Handler在writeIAMError后没有return，继续c.JSON(200,nil)，响应体变为错误JSON加null。正式Go修复由负责人补两处return，成员Web联调等待该错误修复。

## 成员GET/PUT及成员Web正式验收

负责人在本轮已补上ListWorkspaceMembers/UpsertWorkspaceMember错误分支的return。正式TestWorkspaceMemberErrorResponses复验通过；TestWorkspaceHTTP扩展成员GET/PUT成功、空间admin可查询但不能修改/赋予owner、全局admin写入、降级后查询拒绝、非法角色/UUID/不存在用户与伪造actor_id拒绝；HTTP helper还要求响应只包含一个JSON值。与用户HTTP回归一起真实隔离PG/race通过。

助手复用此前WorkspaceMembersDrawer，适配当前API及会话边界，新增GET /me取得最新全局角色展示权限。成员按钮不代替后端授权；没有成员管理权限时展示403说明。前端26项测试、tsc/Vite构建通过。

桌面浏览器在正式API代码临时实例、隔离PG schema验证：owner打开成员抽屉、按已有用户ID添加viewer、修改为editor并自动刷新、移除最后owner收到409且保留确认框与解释、取消后移除普通成员、刷新页面重新打开仍保留正确列表。测试实例已停止并清理schema/账号/空间；未开展手机端验收。空间admin按钮及撤权完整桌面矩阵留全组验收，不能把HTTP权限测试当作已做全部浏览器角色组合。

剩余：Workspace中间件、空间context接口/当前空间选择，以及全组取消/故障边界、make check、CI和最终head Review。成员Web完成不等于G02已交付。

## Workspace中间件下一批候选（正式实现待保存）

已在临时副本预验证：标准context空间ID/角色、Workspace中间件、WorkspaceRole空间存在性检查与GET /context。Gin按路由显式挂载，空间列表/创建/成员路径不依赖当前空间Header；成员管理仍依据URL空间和Service权限。Header优先，缺Header回退workspace_id查询参数；缺空间400，无权403，不存在404，内部故障保留500语义。策略沿固定Final的角色/方法和执行/审批路径规则，未知角色拒绝。固定Final没有独立/context，本工程作为当前空间与权限展示的扩展接口，由前端选择空间时消费，不冒充来源原有API。

候选TestWorkspaceContextHTTP真实隔离PG/race通过：无token、缺空间、Header/query优先级、owner/全局admin有效角色、非成员拒绝、全局admin也拒绝不存在空间、viewer可读取、移除后同token拒绝。候选TestWorkspaceMethodPolicy通过，覆盖viewer只读、member执行与控制面写入区别、editor审批拒绝、admin/owner审批允许及未知角色拒绝；既有TestWorkspaceService回归通过。临时副本旧重复测试已移除后回归，不影响正式工作区。以上均仅候选验证，生产Go与正式测试待负责人保存实现后接入，前端当前空间选择尚未实现。

## Workspace与当前空间选择正式验收及预审阻断

负责人已保存Workspace中间件、空间存在性检查与/context。助手将Context HTTP及角色方法策略测试归入router_test.go/auth_test.go，真实PG/HTTP/race通过；追加中间件401/400/403/404/500映射、错误不泄露内部信息、失败不进入Handler和标准context保留测试通过。

前端只持久化workspaceId，角色由/context实时确认。选择验证使用显式目标Header，成功后才切换；普通空间业务请求自动携带选中ID，全局/URL成员接口不依赖该Header。退出和新登录清空ID；成员自身被移除会清理选择；角色变更会使context查询失效并重新读取。401迟到响应仅能清理对应请求的同一token会话，不得退出后来登录的新会话。34项前端测试、tsc/Vite构建通过。

正式代码临时服务配合隔离PG schema完成桌面验收：owner在A/B切换、刷新恢复并重查角色、退出清空、换viewer账号不继承选择、viewer角色正确、切换到刚撤权的A失败时保留B、当前B被撤权后刷新自动清空选择且仍保持登录、网络请求中的显式目标Header正确。测试服务停止并清理所有临时数据，无手机测试。

本轮集中make check曾完整通过：文档/空白、依赖一致性、全Go包race、vet、server/migrate构建及34项Web测试/构建。补充空间锁等待取消/角色CHECK失败回滚测试也随全套通过。

随后独立AI工作区预审发现P1：Service在空间锁外预先算allowOwnerManagement，Store锁内未重读actor。主代理用pg_stat_activity确认actor请求阻塞后才提交撤权，动态复现3条失败：owner降viewer后自提权、admin降viewer后改他人、owner移除后删他人，旧实现均返回成功。该问题不只是一般在途请求语义，因为会恢复持续有效的owner权限。

候选修复只在临时副本实现：必需Store接口用actorID替代bool；Service保留前置粗检；两个写事务先LockWorkspace，再用同一WithTx q读取actor用户状态/全局角色/空间角色，重新判断成员与owner管理权限。目标owner与最后owner规则不变。全量IAM真实PG/race（含新增3回归）通过，AI只读复审认可修复。正式Go尚未修改；助手已将验证过的两个测试文件同步为新接口契约，正式make check需待负责人保存修复后重新执行。当前不是可合并状态，没有G02远程CI/最终head Review/人类Approve；用户停用/删除明确不在本组范围。

## 2026-11-17 正式修复与提交前复审

负责人已在正式工作区保存actorID必需Store接口、两个Service调用、两个写事务的锁后授权查询；workspaceManagementAuthorityTx目前位于workspace.go，使用同一包的pgx/sqlc类型。同包文件位置不改变事务行为，是非阻断组织事项。

正式TestMembershipRechecksActorAfterWorkspaceLock三场景真实隔离PG/race通过：降级owner不能恢复自身owner、降级admin不能改他人、被移除owner不能删他人。回归通过pg_stat_activity确认请求在空间锁上等待后再提交撤权，未使用概率sleep制造交错。

本轮重新运行完整make check并返回0：6项文档测试、未暂存/暂存空白检查、依赖一致性、全Go包真实隔离PG/race、vet、server/migrate构建、34项Web测试、tsc/Vite构建全部通过。此前桌面结果仍为本组真实浏览器验收证据，修复后没有声称再次完成全部桌面矩阵。

原独立AI Reviewer只读复审正式未提交工作区，确认P1可以关闭、两个写入口和所有调用均已切换actorID、没有旧bool或fallback，owner/admin及最后owner规则保持。结论为可进入PR；复审没有修改代码/索引/分支，没有操作DB或发送GitHub消息。这不是最终提交SHA审查，也不是人类Approve。

当前状态Ready for PR：尚未提交/推送/创建G02 PR；负责人使用docs/delivery/G02-PR.md提交正文，随后检查实际head的远程CI并按该SHA评审，由负责人手动合并。暂不自动开始G03，保留用户执行Git的学习边界。
