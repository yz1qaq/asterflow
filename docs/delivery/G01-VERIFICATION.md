# G01 本地验收记录

日期：2026-11-15。需求：[G01](../tasks/G01-user-auth.md)，关联[Issue #9](https://github.com/yz1qaq/asterflow/issues/9)。
分支feat/ENG-004-005-user-auth，基线8b64cb52e0e61bcd905b71d5a0981919bf7b8680。本组已创建[PR #10](https://github.com/yz1qaq/asterflow/pull/10)，初版head为2a18e3e8557d0c1efa6146ed9f5b65f713f25ca5。原本地验收与后续远程结果分开记录。

## 实现范围与归属

负责人已保存生产Go/SQL：User、5条sqlc查询与PG适配、密码/管理员初始化、JWT、Gin身份与管理员中间件、严格JSON/分页、三个Handler、Config和启动装配。
助手维护归组测试、生成/依赖配置、增量React登录/用户/系统状态、Makefile/CI定义与文档。未自动提交/推送/合并。
React复用原管理端对应页面与公共样式，只迁移本组已接入的功能；独立品牌与登录存储名，未搬入空间/模型/Prompt/Agent等未来页面。

## 已运行的验证

- 真实PG使用随机schema，结束后只删除对应schema；没有在public执行测试DDL/down。
- Store：创建写回、时间/UUID/默认值、查询副本、邮箱与主键冲突区分、取消、角色和分页。
- Service：输入校验、bcrypt与空格保留、管理员初始化幂等/并发、登录成功/失败/停用及错误cause。
- JWT：有效token、错误密钥/算法/issuer、过期/缺exp/未来时间、身份不一致及空密钥。
- Gin中间件：9类认证/管理员请求、请求context传递和拒绝后不进入Handler（真实JWT，存储结果使用显式测试夹具）。
- Handler：1 MiB、未知字段/尾随数据/错误类型、分页边界、邮箱409、内部错误不回显。
- 真实HTTP+PG：公开登录、管理员创建/列表、无token401、普通用户403、旧token在角色降级后403/停用后401。
- Config新增9组认证配置；原健康、PG、迁移、生命周期与子进程测试同步维护。子进程明确使用测试DB且关闭autoseed，避免继承本机初始化配置。
- 前端18项会话/API测试通过；TypeScript及生产构建通过。登录/用户页按路由延迟加载；仍有一个约562 kB框架共享chunk的非阻断构建提示，未通过提高阈值隐藏。
- 桌面真实浏览器：系统健康与就绪、管理员登录、新建用户、重复邮箱、刷新回读、错误密码、退出清理、普通用户菜单/页面拒绝与真实API403、12用户的10+2分页切换通过。未进行手机端测试。
- 浏览器使用实际Go可执行文件和Vite，独立18091/15175端口、随机PG schema及测试账号；结束后清空测试登录态、停止这两个进程、删除精确测试schema与临时凭据。

## 集中门禁与评审状态

显式加载.env及ASTERFLOW_TEST_DATABASE_URL后集中make check退出0；文档、依赖一致性、全部10个Go包（2个无测试）、race/vet/server+migrate构建、前端18测试/类型检查/构建通过。Web工作流定义已增加，初版2a18e3e的push/PR两组go/web均SUCCESS，docs因5处空白FAILURE；修复后新head仍需重新确认远程结果。
服务端当前已知必需状态仍是docs/go，未声称web已加入GitHub分支保护。
独立AI只读工作区预审已完成，未发现已证实P1/P2。审阅了生产Go/SQL/生成代码、配置/路由/React/测试/CI；受审查环境Docker权限限制，未独立复跑PG/浏览器。不是GitHub独立Approve，不代替最终提交SHA的PR Review。非阻断建议：users在created_at同值时可追加id唯一排序；当前按固定参考单列排序，未将低概率单条写入边界夸大为已证实阻断缺陷。

## 本地复验

先执行make web-install安装锁定依赖。在已显式导出可信.env的终端运行：

```bash
make check
```

没有ASTERFLOW_TEST_DATABASE_URL时，真实PG用例会明确跳过，不能据此声称PG验收完成。
开发启动与前后端链路见[README](../../README.md)。本组未生产部署；后续完整能力按ROADMAP继续。

## PR #10 提交级 Review 与 CI 修复

[AI COMMENT Review](https://github.com/yz1qaq/asterflow/pull/10#pullrequestreview-5911499070)锚定2a18e3e8557d0c1efa6146ed9f5b65f713f25ca5，独立复核全部67文件，未发现已证实业务P1/P2。
远程go/web在push和PR两组均SUCCESS；docs在git diff --check HEAD^ HEAD处失败。5个文件为domain/iam.go和4个Web文件，只有EOF空行/行尾空格。

根因：先前本地git diff --check未覆盖未跟踪新文件，而CI针对已提交差异检查。本地已仅清理这5文件空白，未改变业务文本。
门禁新增全部Git候选文件的空白检查（包含尚未git add的新文件，跳过符号链接/二进制），并检查暂存差异。
新增未跟踪文件回归用例先失败、修复后通过；6项文档测试与完整候选检查通过。独立AI也复核了这次未提交门禁补丁，无已证实P1/P2。
修复尚未推送，不能把本地通过当成远程已绿；负责人需在同一分支追加提交，更新原PR并重新确认新head。
