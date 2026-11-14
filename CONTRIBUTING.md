# 贡献与交付流程

先阅读[PRD](docs/product/PRD.md)、[架构](docs/architecture/ARCHITECTURE.md)、当前[状态](docs/delivery/STATUS.md)与任务。任务达到[Ready](docs/process/DEFINITION-OF-DONE.md)再开工。

## 创建分支

main只接收PR。下面以已领取ENG-001为例，文档评审完成后执行：

```bash
git switch main
git pull --ff-only origin main
git switch -c feat/ENG-001-config-health
```

助手分支使用codex/前缀；修复可用fix/ENG-xxx。一个PR只交付一项可独立验收任务，避免混入别的模块和本地环境产物。

## 开发与提交

先写短方案和接口约定，再实现/测试/联调。用git status、git diff检查范围；git commit仅产生本地提交，不等于进入main或上线。暂存明确路径，检查staged diff，避免提交.env、密钥、日志、数据库、node_modules或编译产物。

```bash
git diff --check
git status --short
git add <本任务的明确文件>
git diff --cached
git commit -m "feat(core): add configuration and health endpoint"
git push -u origin feat/ENG-001-config-health
```

方括号/尖括号处是说明占位，不直接复制运行；具体任务列出实际路径。当前可运行检查仅README中的Python文档工具，业务检查随任务增加。

## PR与评审

```bash
gh pr create --draft --base main --title "feat(core): add configuration and health endpoint" --body-file /tmp/pr-body.md
gh pr ready <PR_NUMBER>
gh pr checks <PR_NUMBER>
```

PR说明引用Issue/任务，描述触发场景、结果、测试证据、迁移/配置/回滚影响。body-file保留真实换行；不把token粘到PR或截图。
准备就绪后由[Reviewer](docs/process/CODE-REVIEW.md)按head审查。作者逐项回应、修复、推送同一分支，待复审；不在Review期间另建重复PR逃避讨论。

## 合并与同步

负责人检查CI/讨论/风险后在GitHub执行Squash，记录归属与验证。合并后本地回main执行git pull --ff-only；再开始下一任务。不要在旧功能分支继续堆下一功能。
本轮设计PR留待负责人审核，不自动合并。独立审批限制和服务端保护见[治理记录](docs/process/REPOSITORY-GOVERNANCE.md)。

## 本地防误推

```bash
git config --local core.hooksPath .githooks
```

hook阻止本地直接推main。它是额外防误操作工具，--no-verify或修改配置能绕过；服务端保护才是远程规则。不要为绕过失败CI使用强推main或关闭保护。
