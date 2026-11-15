# 仓库治理与实际配置

更新日期2026-11-15。仓库：[yz1qaq/asterflow](https://github.com/yz1qaq/asterflow)，公开，main为默认分支。初始化README是bootstrap提交；需求/架构PR #3已由负责人合并；ENG-001已通过PR #4由负责人合并，docs/go检查通过，但没有合并前正式PR Review。ENG-002已通过PR #6合并，固定HEAD AI COMMENT和docs/go通过；当前ENG-003分支已修复query校验并通过本地验收，准备作者提交/PR，不自动合并。

## 已经实际配置并通过API回读

- main.required_status_checks：strict=true，要求docs和go（2026-11-15已通过API回读）。
- main.required_pull_request_reviews：存在，dismiss_stale_reviews=true，required_approving_review_count=0。
- enforce_admins=true；required_conversation_resolution=true；required_linear_history=true。
- allow_force_pushes=false；allow_deletions=false。
- 仓库只启用Squash合并，关闭普通Merge/Rebase，合并后删除分支，允许更新PR分支。
- 本地pre-push hook另防误推main；可被绕过，不代替上面的服务端规则。

配置从私有改为公开后生效。私有时同一API曾返回403（套餐限制）；这是配置历史，不是当前仍未受保护。公开变更由所有者明确选择。

## 不能误称的保证

独立批准人数0：当前只有作者账号，GitHub不允许作者批准自己的PR。服务端要求PR和CI/解决讨论，但不保证“另一名人类已经看过”。
AI可以留下COMMENT review并锚定当前head，标注发现和证据。邀请真实有权限协作者后，再评审是否设为1个独立批准/Code Owner；不创建虚构账号或自批准记录。

本功能分支已有docs和go工作流：Go包含依赖一致性、race、vet、build。main服务端已要求两项，但本分支尚未push，远程Go结果尚未产生；工作流文件与保护要求不等于Checks通过。ENG-002已在main加入独立PG服务和明确测试URL并远程通过；当前ENG-003新增迁移场景会在同一Go工作流执行，其远程结果待本分支push确认；前端/Runner业务门禁后续按对应实现引入，不能用本地Go/文档绿色代替远程或其他依赖验收。
CI检查失败必须修复；不能为合并关闭保护或将required check改成空。仓库所有者仍可修改设置，这不是组织级不可绕过安全边界。

## 核验命令

```bash
gh api repos/yz1qaq/asterflow
gh api repos/yz1qaq/asterflow/branches/main/protection
gh pr checks <PR_NUMBER>
git config --local --get core.hooksPath
```

官方文档：[分支保护](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches)、[PR评审](https://docs.github.com/en/pull-requests/how-tos/review-pull-requests/reviewing-proposed-changes-in-a-pull-request)。
