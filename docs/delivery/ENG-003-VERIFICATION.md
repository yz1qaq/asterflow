# ENG-003集中验收记录

日期2026-11-15，分支feat/ENG-003-database-migrations，基线699e38f4a01efa23647a04a64fde2b545d10114e。
状态：生产实现及本地验收通过，开发库已应用000001；待作者提交/推送/PR，远程CI及按真实head的正式Review尚未完成。

## 已保存实现

| 位置 | 行为 |
| --- | --- |
| migrations/000001_init_iam.up.sql | 七张IAM初始表、字段默认值/主外键、两个显式索引 |
| migrations/000001_init_iam.down.sql | 按依赖反序删除该版本七表 |
| cmd/migrate/main.go | 互斥动作、path/env配置、URL严格解析、pgx5/file驱动、版本/dirty与错误清理 |
| cmd/migrate/main_test.go | 单文件归组参数、真实二进制及随机隔离schema测试 |
| Makefile | up/version/down1、专用测试、server/migrate双二进制构建 |
| .github/workflows/go.yml | 既有独立PG服务与显式测试URL，新增测试自动随go test ./...执行 |

生产Go/SQL由负责人保存，助手维护测试/文档/运维配置，没有代替负责人提交或合并。

## 最终完整复验

负责人修复ParseQuery后，在显式导出本机可信.env的环境运行make check，退出0：
- 5包go test -race -count=1 ./...通过，包括cmd/migrate的19个参数场景与真实PG/真实二进制场景。
- cmd/server、internal/api、internal/config、internal/infrastructure/postgres既有回归均通过。
- go vet ./...、server/migrate双二进制构建、go mod tidy -diff通过。
- 文档链接/需求与模块/488文件冻结指纹、5项协作工具测试、git diff --check通过。

真实迁移场景：
- version none→up→version1 dirty=false→重复up保留数据→down1→none→reup。
- 七表、两个显式索引、用户默认值、工作空间TEXT主键；邮箱/联合主键/哈希唯一、UUID/NOT NULL/外键约束。
- 用户删除级联清理成员/角色绑定/API Key；父空间删除后子空间parent_id置NULL。
- postgres/postgresql/pgx5三scheme、带空格和#的目录、自定义database-env。
- 非法SQL后version1 dirty=true，后续up/down均被拒绝，没有自动force。
- 无已应用版本时请求固定步数down会退出1，不虚报回退成功。

全部回退只在本次创建的随机asterflow_mig_前缀schema中执行，结束后只清理对应schema。只读复查残留数量0。

## 开发库首次应用（与测试schema分开）

完整检查通过后，核对本项目独立PG的asterflow数据库、asterflow角色、public schema，确认public为空：
1. make migrate-up实际输出migrations applied。
2. make migrate-version实际输出version=1 dirty=false。
3. SQL实查public.schema_migrations为1/false。
4. 实查public有api_keys、role_permissions、roles、user_roles、users、workspace_members、workspaces七表，以及框架的schema_migrations。
5. 临时测试schema残留0；未对public执行down，未操作旧项目数据库。

这是真实开发库应用记录，不把临时schema成功当成开发库已建表。没有生产环境部署声明。

## 预审发现与修复闭环

初次独立AI预审发现一项P1：URL.Query静默丢弃非法RawQuery键值，可能丢失search_path并改用默认schema。
助手先补malformed_query_escape与malformed_query_separator，两项在旧实现中失败；负责人改为url.ParseQuery并在错误时返回固定invalid database URL query。
本轮make check中两项均通过。独立AI只读复审核对错误处理位于migrate.New之前、不回显原连接串，确认原P1关闭、无新增问题；实际测试由主代理执行。未通过的历史make check不计入当前通过证据。
这是基于699e38f的未提交工作区预审，不能代替新commit的正式PR Review或人类独立Approve。

## 尚待交付与范围

作者commit/push/开PR→确认本分支远程docs/go→按实际head SHA正式Review→负责人决定合并。旧PR的绿色不代替本次远程检查。
本任务只包含IAM初始数据库结构和迁移基础设施；Store/Service/JWT/业务HTTP/sqlc查询生成及后续RBAC强化仍待对应任务交付。
