# 仓库治理与实际配置

记录日期2026-11-14。仓库：[yz1qaq/asterflow](https://github.com/yz1qaq/asterflow)，公开，main为默认分支。初始化README是bootstrap提交；本轮需求/架构通过codex/project-foundation分支PR提出，待负责人审阅，不自动合并。

## 已经实际配置并通过API回读

- main.required_status_checks：strict=true，要求docs。
- main.required_pull_request_reviews：存在，dismiss_stale_reviews=true，required_approving_review_count=0。
- enforce_admins=true；required_conversation_resolution=true；required_linear_history=true。
- allow_force_pushes=false；allow_deletions=false。
- 仓库只启用Squash合并，关闭普通Merge/Rebase，合并后删除分支，允许更新PR分支。
- 本地pre-push hook另防误推main；可被绕过，不代替上面的服务端规则。

配置从私有改为公开后生效。私有时同一API曾返回403（套餐限制）；这是配置历史，不是当前仍未受保护。公开变更由所有者明确选择。

## 不能误称的保证

独立批准人数0：当前只有作者账号，GitHub不允许作者批准自己的PR。服务端要求PR和CI/解决讨论，但不保证“另一名人类已经看过”。
AI可以留下COMMENT review并锚定当前head，标注发现和证据。邀请真实有权限协作者后，再评审是否设为1个独立批准/Code Owner；不创建虚构账号或自批准记录。

当前只存在docs工作流。Go/前端/PG/Docker等job随源码与依赖交付后增加，并同步升级required checks，不能用文档CI绿色代替产品测试通过。
CI检查失败必须修复；不能为合并关闭保护或将required check改成空。仓库所有者仍可修改设置，这不是组织级不可绕过安全边界。

## 核验命令

```bash
gh api repos/yz1qaq/asterflow
gh api repos/yz1qaq/asterflow/branches/main/protection
gh pr checks <PR_NUMBER>
git config --local --get core.hooksPath
```

官方文档：[分支保护](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches)、[PR评审](https://docs.github.com/en/pull-requests/how-tos/review-pull-requests/reviewing-proposed-changes-in-a-pull-request)。
