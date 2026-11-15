# 项目交付状态

更新时间2026-11-15。G01（ENG-004+005用户与登录）的本地实现、真实HTTP/PG和桌面Web验收已完成，PR #10已创建，初版head 2a18e3e的go/web成功、docs因5处空白失败；格式及门禁修复已在本地完成，待负责人提交/推送，未合并本组。

| 项目 | 当前事实 |
| --- | --- |
| 本地 | feat/ENG-004-005-user-auth，G01已提交2a18e3e8557d0c1efa6146ed9f5b65f713f25ca5；本地格式/门禁/证据更新待提交 |
| 已合并 | 设计PR #3、ENG-001 PR #4、ENG-002 PR #6、ENG-003 PR #8 |
| ENG-003提交 | head ed6c1e6aabf547869c40e0f891cd45377209fae5；Squash 8b64cb52e0e61bcd905b71d5a0981919bf7b8680；树均56e68c145a76b93b38ab7d92f5e9abdc4bc6f055 |
| ENG-003流程 | 远程docs/go成功；有工作区预审/修复复审，但合并前正式GitHub Reviews为空。合并后固定head复核无新增发现，不补称前置Review或独立Approve |
| 数据库 | 000001已应用；IAM七表及框架版本表。测试仅使用随机隔离schema，不对开发public执行down |
| G01 / Issue #9 | PG用户存储、bcrypt/管理员初始化、JWT、Gin认证/管理员鉴权、登录/创建/列表、配置/启动装配、React登录/用户/系统状态均已保存并本地验收 |
| 集中门禁 | 显式加载.env及测试URL后make check通过：文档、依赖一致性、全部Go包race/vet/build、前端18测试/类型检查/生产构建 |
| 真实链路 | 独立schema中HTTP+PG验证账号/权限变更；实际Go进程+Vite桌面浏览器登录/创建/重复/刷新/退出/member403/10+2分页通过，测试进程/schema已清理 |
| G01 Review | 已对2a18e3e全部67文件完成独立AI提交级复核并发布COMMENT Review 5911499070；业务未发现已证实P1/P2，CI文档失败需修复。不是独立人类Approve |
| 远程待办 | PR #10的2a18e3e在push/PR两组go/web均SUCCESS，docs均FAILURE；当前已知分支保护必需项仍为docs/go。修复后新head需重新确认全部Checks |
| 已知非阻断项 | 前端一个约562 kB共享框架chunk提示；created_at同值排序可补唯一tie-breaker，见G01验收记录 |
| 下一步 | 负责人在原分支提交本地格式/门禁修复并push，更新同一PR #10；确认新head的docs/go/web与Review后再决定合并 |
| 后续 | 按ROADMAP推进G02空间/成员/事务权限；G01不代表完整FR-01或全部产品已交付 |

完整证据见[G01验收记录](G01-VERIFICATION.md)。没有生产部署声明。
