# 贡献与交付流程

先阅读[PRD](docs/product/PRD.md)、[架构](docs/architecture/ARCHITECTURE.md)、[合并交付计划](docs/delivery/ROADMAP.md)、[当前状态](docs/delivery/STATUS.md)与本组任务。达到[Ready](docs/process/DEFINITION-OF-DONE.md)后开工。

## 分支与需求组

自ENG-004起，按G01至G22需求组交付。组内多个ENG子任务共用一个Issue、一条功能分支和一个PR，包含相关后端/前端/测试/文档。001/002保留既有历史，003先按原流程收尾。
下例在G01开始、其前置交付完成后由负责人执行：

```bash
git switch main
git pull --ff-only origin main
git switch -c feat/ENG-004-005-user-auth
```

助手分支默认codex/，修复可用fix/ENG-xxx。功能分支以该组ENG范围及功能名命名；不按Store/Service/Handler分别建分支或PR，不混入下一需求组实现。

## 开发与提交

按本组已确认契约连续实现Go/SQL/API和对应Web。真实故障及时定位，组末集中运行相关依赖/桌面联调及检查。人类负责生产Go/SQL，助手负责增量前端、归组测试与文档/运维。
组可以跨天；可以在同一分支保留中间commit，push只更新同一个PR。git commit是本地记录，不等于进入main或上线。

```bash
git diff --check
git status --short
git add <本需求组的明确文件>
git diff --cached
git commit -m "feat(iam): add user authentication"
git push -u origin feat/ENG-004-005-user-auth
```

尖括号处为占位，实际提交前用任务中的真实文件列表。凭据、日志、数据库、node_modules和构建产物不入库。
当前make check包含文档/依赖/race/vet/构建；相关PG集成须显式提供测试URL，前端/其他依赖检查随对应实现加入，不用跳过当通过。

## PR与评审

```bash
gh pr create --draft --base main --title "feat(iam): add user authentication" --body-file /tmp/pr-body.md
gh pr ready <PR_NUMBER>
gh pr checks <PR_NUMBER>
```

PR描述关联一个需求组及其ENG子任务/Issue，说明最终行为、验证、迁移/配置/回退影响。body-file保留实际换行，不粘贴token。
准备就绪后由[Reviewer](docs/process/CODE-REVIEW.md)按真实head SHA审查；修复推送到同一分支，重新确认检查与Review，不为每个子任务/小修改重复开PR。

## 合并与同步

负责人在CI/讨论/Review完成后决定Squash合并；助手不自动提交、推送或合并。主分支得到该功能组的完整交付，随后同步main，再开始下一组。
[治理记录](docs/process/REPOSITORY-GOVERNANCE.md)说明服务端保护与单账号限制，AI COMMENT不等于独立人类Approve。功能组扩大不意味着降低审查/隔离标准。

## 本地防误推

```bash
git config --local core.hooksPath .githooks
```

hook为额外保护，可被绕过，不能代替服务端main保护；不得为合并关闭required checks。
