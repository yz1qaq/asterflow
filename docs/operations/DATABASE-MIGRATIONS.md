# 数据库迁移操作

ENG-003已通过本地集中验收，query解析问题已修复；本机开发public已实际应用000001。当前尚待PR/远程CI，具体证据见[验收记录](../delivery/ENG-003-VERIFICATION.md)。

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
初始IAM不是完整鉴权功能：后续RBAC强化和Store/Service/API仍需各自交付。工作空间主键及关联列TEXT，支持slug；用户/角色主键UUID。

## 回退与失败

```bash
make migrate-down
```

该命令明确回退一个版本；当前000001的down会删除七张表及其中数据。自动回退测试只针对临时schema，不对开发库执行。
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
