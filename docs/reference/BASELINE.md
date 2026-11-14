# 技术基线与范围

产品名称AsterFlow；来源记录用于技术与许可追溯，不表示本仓库已实现相同功能。

- 上游：[Q1mi/kbot](https://github.com/Q1mi/kbot)
- 固定提交：baed6c354e1966cdae7bc5328e771dc50ae6c70b
- 固定子目录：kbot-final
- 文件清单：[upstream-files.json](upstream-files.json)，488个tracked文件及根级CI对象SHA，所有项有主责模块。
- 模块目录：[modules.json](modules.json)，19模块，覆盖FR-01至FR-18。
- 来源/许可：[THIRD_PARTY](../../THIRD_PARTY.md)。

## 范围规则

全部已实现的生产模块、API/协议、页面、SDK/CLI、业务预设、迁移/部署/验证都进入Backlog。辅助Memory实现纳入阅读/契约对照；Go生成文件由sqlc产生，不要求手工复制。原迁移26组按表/索引/约束/修复语义对齐，编号和文件数不是完成指标。

上游README明确列为演进的OpenSearch/Outbox、OpenAPI3生成、SSO/OIDC/API Key鉴权、Secret Manager、Kubernetes/HA/灾备不冒充当前已有功能；如后续新增需新需求。MinIO实际承担归档/导出，当前知识来源是Markdown，不凭对象存储就宣称PDF/Word摄取。

## 目标映射差异

- 上游kbot产品标识→AsterFlow；module→github.com/yz1qaq/asterflow；CLI kbotctl→asterctl；配置前缀计划ASTERFLOW_。协议资源/字段随任务评审，不做破坏语义的机械替换。
- projects/crossborder、projects/insurance→business_systems/crossborder、business_systems/insurance，独立module/进程/PG数据库。参考内存业务示例改为PG适配，业务行为不削减。
- internal/platform/coursepreset→internal/platform/preset，保留全部场景资产/初始化能力。
- 来源文件/测试名/目录拆分可不同；最终必须补目标路径和证据映射。清单中模块标记是分工，不是自动审查了每行代码。
- 不复制其他工程实现或沿用其“第N天完成”状态。本工程从需求/设计开始，所有功能验收从零记录。

## 验证边界

结构检查保证ID/清单/链接一致；行为仍由契约、真实PG/Redis/Runner、协议、桌面和部署验收证明。受控模型与外部模型质量分开；模拟飞书协议与真实租户验证分开。未验证外部项保持明确状态。
