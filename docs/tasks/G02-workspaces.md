# G02：工作空间与成员权限（ENG-006+007）

关联[Issue #11](https://github.com/yz1qaq/asterflow/issues/11)，FR-01 / MOD-IAM / EP-02。
状态Done：PR #12已手动合并为ad533f7cbc8082353fa51e2ec27a05b7e4c171bf，本地main已同步；最终head的CI与AI复审通过。
分支feat/ENG-006-007-workspaces，基线为G01合并提交b82d826e46e8112fca2cb9cf836b76ec62685272。

## 需求结果

已登录用户可创建空间并成为首个owner，查看有权访问的空间并选择当前空间；owner与空间admin按权限管理成员。全局admin可见和管理全部空间，但不绕过最后owner保护。
同一账号可属于多个空间，各空间角色独立。parent_id仅表示结构，不继承成员权限。

## 实现与边界

- 负责人编写生产Go/SQL，助手维护归组测试、React/API联调、文档和AI Review。
- 000001已有workspaces/workspace_members；本组000002增加用户/成员角色CHECK与空间角色联合索引，对应固定参考000016，编号按本项目依赖顺序。
- 工作空间ID为TEXT、用户ID为UUID；nullable parent_id通过Store映射；列表用created_at和id稳定排序。
- 创建空间及首位owner由必需PG事务组合sqlc调用，失败不留半成品；没有可选事务fallback。
- 成员新增/改角色/删除共用空间行锁；取得锁后，使用同一WithTx查询操作者身份与成员角色，再检查目标owner与最后owner。Guarded Store接收可信actorID，不接收旧权限bool。
- 事务权限辅助函数目前位于workspace.go，同包调用成立；按持久化职责移入postgres_workspace_store.go是非阻断的组织建议。
- Gin接入层使用gin.Context，Service/Store使用标准context.Context。Auth提供可信user_id/global_role；Workspace补空间存在性、角色及操作检查。
- GET /me返回身份；GET /context为当前空间展示扩展接口，使用Header优先、query回退。空间列表/创建与按URL指定空间的成员API不要求选中空间，成员Service仍检查目标空间。
- React只持久化当前workspaceId，选择或恢复时由/context重查角色。失败切换保留原选择；失效空间清除；退出/新登录清空选择。
- 普通空间业务请求携带当前空间Header，显式目标Header不被覆盖；全局与URL成员API独立。迟到的旧请求401不能清理新会话。
- 暂不增加账号停用/删除，也不提前实现后续模型/Agent等模块；不做手机端验收。

## 权限规则

| 身份 | 空间可见性 | 成员管理 |
| --- | --- | --- |
| 全局admin | 全部空间 | 可管理owner及其他成员 |
| 空间owner | 自己加入的空间 | 可管理owner及其他成员 |
| 空间admin | 自己加入的空间 | 可管理非owner，不能赋予owner |
| editor/member/viewer | 自己加入的空间 | 不管理成员，业务操作按角色限制 |

最后一位owner不能被移除或降级。操作者在等待空间锁时被降级或移除，锁内复查必须拒绝其成员写入。

## 验收与交付

- [x] 角色约束迁移、领域类型、SQL及sqlc生成；迁移先隔离验证后应用开发库。
- [x] 空间/成员Store、创建事务、最后owner保护与取消/失败回滚通过真实PG/race。
- [x] 负责人修复锁外权限快照问题，3条确定性并发撤权回归通过正式实现。
- [x] Gin身份/路径权限/context及React空间、成员与当前空间流程完成本地联调。
- [x] 2026-11-17完整make check通过，前端34项测试及构建通过。
- [x] 正式工作区修复复审通过，P1关闭。
- [x] 负责人提交、推送并创建同组PR #12。
- [x] 最终head 2fec0f6的远程CI/AI复审通过，负责人手动Squash合并。

真实数据库、桌面、修复复现和历史预验证分别记录在[G02验收记录](../delivery/G02-VERIFICATION.md)。工作区预审不等于未来提交SHA审查，也不等于人类Approve。
