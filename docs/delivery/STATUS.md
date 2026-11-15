# 项目交付状态

更新时间2026-11-15。G01（ENG-004+005用户与登录）的本地实现、真实HTTP/PG和桌面Web验收已完成，等待负责人提交PR及最终head的远程CI/Review；未合并本组。

| 项目 | 当前事实 |
| --- | --- |
| 本地 | feat/ENG-004-005-user-auth，G01代码/测试/React/配置/文档尚未由负责人提交 |
| 已合并 | 设计PR #3、ENG-001 PR #4、ENG-002 PR #6、ENG-003 PR #8 |
| ENG-003提交 | head ed6c1e6aabf547869c40e0f891cd45377209fae5；Squash 8b64cb52e0e61bcd905b71d5a0981919bf7b8680；树均56e68c145a76b93b38ab7d92f5e9abdc4bc6f055 |
| ENG-003流程 | 远程docs/go成功；有工作区预审/修复复审，但合并前正式GitHub Reviews为空。合并后固定head复核无新增发现，不补称前置Review或独立Approve |
| 数据库 | 000001已应用；IAM七表及框架版本表。测试仅使用随机隔离schema，不对开发public执行down |
| G01 / Issue #9 | PG用户存储、bcrypt/管理员初始化、JWT、Gin认证/管理员鉴权、登录/创建/列表、配置/启动装配、React登录/用户/系统状态均已保存并本地验收 |
| 集中门禁 | 显式加载.env及测试URL后make check通过：文档、依赖一致性、全部Go包race/vet/build、前端18测试/类型检查/生产构建 |
| 真实链路 | 独立schema中HTTP+PG验证账号/权限变更；实际Go进程+Vite桌面浏览器登录/创建/重复/刷新/退出/member403/10+2分页通过，测试进程/schema已清理 |
| G01预审 | 独立AI只读工作区预审未发现已证实P1/P2；未独立复跑PG/浏览器，不等于固定PR head Review或GitHub独立Approve |
| 远程待办 | 本组尚无PR/最终head的Checks；新增web工作流是定义，当前已知分支保护必需项仍为docs/go |
| 已知非阻断项 | 前端一个约562 kB共享框架chunk提示；created_at同值排序可补唯一tie-breaker，见G01验收记录 |
| 下一步 | 负责人本机复验界面后在当前分支提交同一组PR；确认docs/go/web及最终head Review，再由负责人决定合并 |
| 后续 | 按ROADMAP推进G02空间/成员/事务权限；G01不代表完整FR-01或全部产品已交付 |

完整证据见[G01验收记录](G01-VERIFICATION.md)。没有生产部署声明。
