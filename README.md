# AsterFlow · 星流

**企业级 Agent 编排与治理平台。**

AsterFlow 将模型、提示词、技能、工具、知识库与多Agent编排纳入版本化控制面，为业务提供可追踪、可审批、可评估的Agent执行能力。

## 项目状态

需求与架构已通过PR #3；ENG-001配置/Gin/HTTP生命周期已通过PR #4合并。ENG-002已在功能分支完成PostgreSQL连接池、启动装配和/readyz，本地真实PG及故障恢复验收通过；待作者提交/PR、远程CI和正式Review，尚未合并或部署发布。功能状态以[交付状态](docs/delivery/STATUS.md)为准，不将设计目标表述为现有能力。

目标范围包括：身份与空间权限、模型管理、Prompt治理、五源工具、Skills、知识检索、Agent/Team、审批与A2UI、Guard、Eval、审计观测、外部渠道、SDK/CLI、独立业务场景及部署。

## 文档导航

| 文档 | 用途 |
| --- | --- |
| [产品需求](docs/product/PRD.md) | 用户、场景、功能与验收 |
| [架构设计](docs/architecture/ARCHITECTURE.md) | 系统边界、分层、运行与部署 |
| [模块职责](docs/architecture/MODULES.md) | 模块依赖、责任和交付边界 |
| [数据模型](docs/architecture/DATA-MODEL.md) | 实体、版本、事务和租约 |
| [接口约定](docs/architecture/API-CONVENTIONS.md) | HTTP、事件、错误和权限 |
| [迭代Backlog](docs/delivery/BACKLOG.md) | Epic、任务顺序与验收 |
| [协作规范](CONTRIBUTING.md) | 分支、提交、PR与合并 |
| [代码评审](docs/process/CODE-REVIEW.md) | 评审维度、问题分级和复审 |
| [完成标准](docs/process/DEFINITION-OF-DONE.md) | 交付必须具备的证据 |
| [首项工程任务](docs/tasks/ENG-001.md) | 配置与HTTP基础服务任务 |
| [需求追踪](docs/reference/TRACEABILITY.md) | 需求到模块、任务和证据的映射 |
| [架构决策](docs/architecture/adr/0001-reference-and-boundaries.md) | 技术基线与设计取舍 |

## 文档检查

Python 3.9+，无需第三方依赖：

```bash
python3 scripts/check_repo.py
python3 -m unittest discover -s scripts -p 'test_*.py' -v
git diff --check
```

当前后端命令和执行路径如下；前端尚未迁移。

## 后端启动与检查

本机需要make、Python3.9+及可用Go。Makefile以命令级固定Go1.26.6，不修改全局环境。ENG-002已要求ASTERFLOW_DATABASE_URL；先按下方本地PG说明准备可信的.env，在每个新终端显式导出。Go不会自动加载.env。

```bash
set -a
source .env
set +a
make run
```

默认监听:18090，另开终端：

```bash
curl -i http://127.0.0.1:18090/healthz
curl -i http://127.0.0.1:18090/readyz
```

PG正常时两者均200、text/plain、正文ok。/healthz仅存活；/readyz每次Ping数据库，最多2秒（依赖遵循ctx），失败503/not ready，不回显内部错误。POST为405，未知路径或尾斜杠为404。在服务终端Ctrl+C停止；SIGTERM同样触发最多5秒优雅关闭，超时关闭连接。普通Handler需配合context取消。

```bash
ASTERFLOW_HTTP_ADDR=127.0.0.1:18091 GIN_MODE=release make run
make check
make build
./bin/asterflow
```

make check执行文档、依赖一致性、race测试、vet和构建；build生成的bin目录已忽略。HTTP地址未设置时用默认，显式空白或非法host:port/端口1–65535以错误退出。数据库URL必需，启动连接失败退出1；运行期间的PG故障由readyz返回503，healthz仍报告存活。没有Redis或业务能力就绪检查。

## 当前启动与退出路径

```mermaid
flowchart TD
    A[main] --> B[start: 配置 / 信号ctx]
    B --> C[postgres.Open: 5秒检查]
    C -->|失败| X[错误返回 / 退出1]
    C -->|成功| D[NewRouter: 注入pool]
    D --> E[run: HTTP监听 / Serve]
    E --> H[healthz: 存活200]
    E --> R[readyz: request ctx / 2秒Ping]
    R -->|正常| OK[200 ok]
    R -->|失败| NO[503 not ready]
    SIG[SIGINT / SIGTERM] --> F[ctx取消]
    F --> G[Shutdown最多5秒 / 超时Close]
    G --> I[等待run返回]
    I --> P[pool.Close]
    P --> S[stop信号监听 / 返回main]
```

[本地验收记录](docs/delivery/ENG-002-VERIFICATION.md)列明已运行和待执行范围。本次不含业务表、迁移、认证、模型或Agent，不能代替完整产品验收。

## 本地PostgreSQL

Docker需运行。首次复制.env.example到.env并替换开发密码，确保两个URL中的密码与ASTERFLOW_POSTGRES_PASSWORD一致；由助手准备的本地环境已生成忽略文件.env，不要覆盖它。真实凭据不可提交。

```bash
make db-up
make db-status
set -a
source .env
set +a
make test-postgres
```

Go不自动读取.env；source是当前终端显式加载本机可信配置。测试只读查询当前独立开发库，无建表/迁移。test-postgres要求显式测试URL；普通go test没有该变量时会跳过真实PG测试。
默认只绑定127.0.0.1:25432，Compose项目asterflow使用独立容器和数据卷。镜像固定为官方PG16/pgvector多架构索引digest，支持AMD64/ARM64，本机已验证PG16.16；CI使用同一索引，远程结果仍待推送确认。数据未与旧项目共享。

```bash
make db-stop
make db-up
make db-down
```

stop停止数据库，up恢复启动；down移除本项目容器和网络并保留数据卷。修改.env密码不会修改已有数据卷中的角色密码。主程序已装配Open，先连接PG再启动HTTP；healthz不探测数据库，readyz实时检查。PG恢复后可重新就绪，无须重启Go服务。
任务范围见[ENG-002](docs/tasks/ENG-002.md)，真实故障与集中检查证据见[验收记录](docs/delivery/ENG-002-VERIFICATION.md)。

## 协作

采用Issue → 功能分支 → PR → CI → Review → 修复/复审 → Squash合并。main只接收评审后的变更。新克隆可启用本地防误推hook：

```bash
git config --local core.hooksPath .githooks
```

服务端保护、单账号评审限制和实际配置见[仓库治理](docs/process/REPOSITORY-GOVERNANCE.md)。

## 来源与许可

技术参考与固定基线见[第三方说明](THIRD_PARTY.md)和[基线记录](docs/reference/BASELINE.md)。项目采用[Apache-2.0](LICENSE)，保留适用的原项目来源与版权说明。
