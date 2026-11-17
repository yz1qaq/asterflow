# 数据库迁移操作

ENG-003迁移CLI已通过PR #8合并。G02首批迁移于2026-11-16完成隔离验证并应用开发public，当前实查version=2 dirty=false；见[G02验收记录](../delivery/G02-VERIFICATION.md)。

## 命令与目录

从项目根目录运行，准备可信.env并启动本项目PG：

```bash
make db-up
set -a
source .env
set +a
make migrate-up
make migrate-version
```

make migrate-up执行所有尚未应用的版本；已最新时正常无变更。make migrate-version显示version及dirty，未应用版本时显示version=none。
-path默认当前工作目录中的migrations；独立部署二进制时应一并提供SQL目录，或显式指定-path。
-database-env默认ASTERFLOW_DATABASE_URL，可切换到专用环境变量。CLI由golang-migrate的pgx5驱动管理独立连接，不复用server的pgxpool；两个命令生命周期独立。

当前000001一次建立七张表；schema_migrations由框架维护，记录当前版本和dirty，不按表数量计算版本。
G01用户认证已交付；000002补角色约束，空间Store/Service/API仍在G02实现。工作空间主键及关联列TEXT，支持slug；用户/角色主键UUID。

## 回退与失败

```bash
make migrate-down
```

该命令明确回退一个版本；当前从2回到1仅移除角色约束与联合索引，表和数据保留。继续回退000001才会删除七张表及其中数据。自动回退测试只针对临时schema，不对开发库执行。
需要指定其他步数时可使用迁移二进制的-down N；N必须正数。固定步数回退在无版本时会非零退出，不假报完成。
多个迁移版本的执行不是跨版本整体事务，错误后需核对实际版本；不能把命令失败理解为此前所有版本自动撤销。

SQL失败会保留dirty状态，后续变更被拒绝。先查明SQL及实际数据库状态再决定修复流程；当前CLI不提供自动force，不修改已交付版本文件来掩盖失败。
connect_timeout默认5秒，只限制建立连接；不是整次迁移的总时限。

## 自动化验证

```bash
set -a
source .env
set +a
make test-migrations
make check
```

test-migrations要求显式ASTERFLOW_TEST_DATABASE_URL。测试使用随机临时schema，验证up/重复up/约束/down/reup及dirty，最后清理该schema。
make check在提供测试URL时包含这些PG测试；未提供时明确跳过真实PG场景，不能据此声称迁移已验收。
CI已有独立PG服务与显式测试URL，当前go job会自动包含cmd/migrate；远程是否通过以对应PR Checks为准。
测试schema成功不等于开发库已建表。开发库的up、version、表/约束实查结果须另记。

## 当前迁移语义映射

| AsterFlow | 固定参考 | 内容与状态 |
| --- | --- | --- |
| 000001_init_iam | 000001_init_iam | 七张IAM表，已应用 |
| 000002_workspace_rbac | 000016_workspace_rbac | 两个角色CHECK与空间角色索引，已应用 |

编号按本项目依赖顺序安排，功能语义保持；完整26组参考迁移语义由后续各组记录，最终G22核验，不以当前两版冒充完整数据库。
