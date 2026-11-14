# 第三方来源与许可

架构与技术参考：[Q1mi/kbot](https://github.com/Q1mi/kbot)，固定提交`baed6c354e1966cdae7bc5328e771dc50ae6c70b`下的`kbot-final`目录。原项目使用Apache-2.0许可，文本保留在[LICENSE](LICENSE)。

AsterFlow是独立仓库和产品标识。当前仅提交需求、设计及协作工具，没有复制生产Go业务实现。后续复用或改写参考源码时，保留适用来源、版权和NOTICE要求，并在对应文件/PR记录修改。

技术范围覆盖固定基线全部已实现能力，见[基线](docs/reference/BASELINE.md)。部署示例中的独立业务系统采用独立目录/进程/PostgreSQL数据库，作为明确记录的持久化适配；不改变平台完整功能目标。上游尚未实现的演进项不作为已实现功能宣传。
