# ENG-002本地验收记录

> 合并状态更新：ENG-002已通过PR #6合并，提交699e38f4a01efa23647a04a64fde2b545d10114e；固定HEAD AI COMMENT与远程docs/go通过，Issue #5已关闭。下文为功能分支验收时的历史快照，当前工作以STATUS为准。

日期2026-11-15，分支feat/ENG-002-postgres-readiness。状态：实现及本地验收通过，尚未提交/推送/PR；远程CI与按新head SHA正式PR Review待执行。

## 当前实现与归属

| 位置 | 当前行为 |
| --- | --- |
| internal/config/config.go | HTTP地址配置；必需的ASTERFLOW_DATABASE_URL，保留原值，由pgx解析 |
| internal/infrastructure/postgres/postgres.go | pgx v5.9.2连接池，5秒初始化检查，失败关闭，成功由调用方管理 |
| cmd/server/main.go | 信号ctx→Open→注入router→run；run返回后先Close池、再stop信号监听 |
| internal/api/router.go | ReadinessChecker接口；healthz存活，readyz实时Ping、2秒/request ctx、200或503 |
| deploy/compose.yaml | 独立项目asterflow、独立卷/库、只绑定127.0.0.1:25432，固定多架构PG16/pgvector索引 |
| .github/workflows/go.yml | 新增独立PG服务及显式测试URL；定义已保存，远程尚未执行确认 |

生产Go由负责人保存。助手维护原有归组测试、PG环境、CI定义与文档；没有代替负责人提交/推送或合并。

## 本轮实际执行

在本机可信.env显式导出后执行make check：
- 4包go test -race -count=1 ./...通过，包含真实PG查询与真实二进制进程测试。
- 配置26场景；路由/就绪17场景；run生命周期7场景；直接start非法配置1场景；真实进程7场景；连接池5场景。
- go vet ./...、go build、go mod tidy -diff通过。
- 文档链接/FR与模块/488文件冻结指纹、5项协作工具测试与git diff --check通过。
- 未设置测试URL时，正向PG及相应进程测试明确skip；make test-postgres拒绝无URL运行。CI定义显式提供测试URL，不依赖本机.env。
- 子进程过滤外部ASTERFLOW_DATABASE_URL，仅从ASTERFLOW_TEST_DATABASE_URL选择数据库，不误用日常连接配置。

连接池测试验证非法配置不回显密码、已取消/已过期ctx、真实SELECT、初始化ctx取消不关闭池、Close后不可用。
就绪测试验证成功/缺依赖/依赖错误隐藏、health不探测PG、请求context及2秒期限、请求取消、实际超时、过期后返回nil不算成功、每次请求重新检查/恢复，以及405/404语义。
进程测试验证非法HTTP配置、缺DB配置、非法DB配置不泄露密码、不可用DB启动失败不占HTTP端口、占用端口退出1、SIGINT/SIGTERM就绪后退出0并释放端口。

## 真实HTTP与数据库中断

使用本轮构建的bin/asterflow和临时loopback端口，仅操作Compose项目asterflow：
1. PG正常，真实进程启动，healthz=200 ok、readyz=200 ok。
2. 停止该项目PG，已有进程healthz仍200，readyz=503 not ready。
3. PG停机期间启动第二个二进制，退出1，日志有open PostgreSQL，无HTTP启动日志，密码未泄露。
4. 恢复同一PG容器/卷，原进程readyz恢复200，无须重启服务。
5. SIGTERM后退出0，临时HTTP端口可重新绑定。

验收清理逻辑保证恢复PG并关闭临时进程。当前PG继续运行；没有停止旧项目数据库，没有删除数据卷或建业务表。
镜像已从早期本机ARM64专用标识改为官方多架构索引484e838076e04939d27d3e176e91c86eea96236126023be8d3059761122e579f，支持linux/amd64和linux/arm64；本机实际PG为16.16，vector在可用扩展列表，尚未创建扩展。早期增量验证时为16.14，不将其混为当前镜像版本。

## 独立AI工作区预审

基线HEAD/main为921fe851c65c3222fa9b9d91113fc29b4cbd2b48。独立AI只读审查tracked diff及新增代码/环境文件，未发现P0/P1/P2或需提出的P3。
独立执行无URL和显式测试URL的两套race命令均退出0；后者四包全部通过。预审快照SHA-256为8abdfc70a2de288501b71ca416d8f88e941f45f26b9763893a299463217c6961，覆盖代码/测试/CI/Makefile/依赖与新增PG文件，不含真实.env及随后收尾文档。
这是未提交工作区预审，不能充当新commit的正式PR Review或独立GitHub Approve。HTTP先关闭再释放池已静态核对；现有进程测试没有独立观测池Close调用时序。

## 交付边界

仅完成FR-18的PG/就绪基础切片，不含迁移/sqlc业务查询/IAM/Redis/前端。PG角色为本地开发用途，生产权限/备份/部署治理不在本次验收中。
远程GitHub Actions须作者推送后确认；PR建立后按真实head SHA正式审查，再由负责人决定合并。本地通过不标为上线或Issue完成。

文档复核另发现架构历史段落未更新合并状态、任务文件末尾字面换行两项P3，均已修正，并同步治理文档的当前分支说明；文档检查重新通过。不涉及生产Go变更。
