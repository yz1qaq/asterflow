# AsterFlow 跨 AI 交接入口

这个目录是项目的可迁移工作记忆。两份仓库是代码和参考；本目录说明怎样接手、现在做到哪里。无需原会话、外部记忆目录、机器专用路径或某种 AI 插件。

1. 阅读 [AGENTS](../../AGENTS.md) 和 [带学 Skill](../../.agents/skills/asterflow-tutor/SKILL.md)。
2. 阅读 [当前状态](STATE.md) 和 [下一步](NEXT.md)。完整范围依 [产品需求](../product/PRD.md)、[交付路线图](../delivery/ROADMAP.md) 和当前任务单；每日验收依 [学习计划](LEARNING-PLAN.md)。
3. 用实际文件核对。Git 可用时，在 AsterFlow 根目录先执行 `git status --short --branch`、`git log -3 --oneline`，再检查 STATE/NEXT 指定的函数及测试。没有 Git 时，以目录文件为准，不猜分支或提交。目录名不固定，可以通过 `go.mod` 的 `github.com/yz1qaq/asterflow` 识别交付仓库。
4. kbot 是只读参考。固定参考提交 `baed6c354e1966cdae7bc5328e771dc50ae6c70b`，目录 `kbot-final/`。在两仓库并列时，从 AsterFlow 根目录可用：
   ```bash
   git -C ../kbot cat-file -t baed6c354e1966cdae7bc5328e771dc50ae6c70b:kbot-final
   git -C ../kbot show baed6c354e1966cdae7bc5328e771dc50ae6c70b:kbot-final/internal/platform/prompt/service.go
   ```
   kbot 没有 Git 时，可读带有 `REFERENCE-SNAPSHOT.md` 的指定快照，按 [基线说明](../reference/BASELINE.md) 核对。不能验证时明确记录，只先做不依赖参考的部分。
5. 用户给新的 AI 的文字在 [提示词](PROMPT.md)。即使软件不识别 Skill 格式，也可以直接打开 Markdown。

换软件前，保留最新工作区，尤其是未提交的业务文件和本目录。在同一台电脑，直接让新的 AI 软件打开两份本地目录即可。若需上传或跨设备复制，使用不含 `.env`、Git 元数据、数据库、密钥、本机日志和构建产物的源码副本；新环境用 `.env.example` 单独配置。无 Git 的副本不用于推送，带回原仓库后对照合并实际修改再提交。

交接时更新 STATE/NEXT：写明分支与检查点、已保存文件、实际运行的验证和结果、待做、下一步以及会改变答案的约束。候选代码放在本目录下并用非编译扩展名，接入后注明替代位置。普通原理问答无需重写进度。文件冲突时按用户最新授权与实际证据纠正，不能用这份交接覆盖未提交的改动。
