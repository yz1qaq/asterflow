# Crossborder 跨境业务服务

独立 Go 模块、HTTP 进程和 PostgreSQL 数据库，提供订单与库存查询。AsterFlow 通过 REST 工具访问该服务。领域模型和 HTTP 契约参考 [Q1mi/kbot 固定版本的跨境业务示例](https://github.com/Q1mi/kbot/tree/baed6c354e1966cdae7bc5328e771dc50ae6c70b/kbot-final/projects/crossborder)，持久化使用 PostgreSQL。

## 本地启动

按 [AsterFlow 启动说明](../README.md#本地postgresql) 准备项目根目录的 `.env`，从仓库根目录执行。首次运行创建 `crossborder` 数据库，后续可复用。

```bash
make db-up
docker compose --env-file .env -f deploy/compose.yaml exec -T postgres \
  psql -U asterflow -d postgres -v ON_ERROR_STOP=1 <<'SQL'
SELECT 'CREATE DATABASE crossborder'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'crossborder')
\gexec
SQL
```

把 `CROSSBORDER_DATABASE_URL` 设为本机连接串。沿用 `.env` 里 `ASTERFLOW_DATABASE_URL` 的用户、密码和端口，数据库名改为 `crossborder`。

```bash
export CROSSBORDER_DATABASE_URL='postgres://asterflow:YOUR_PASSWORD@127.0.0.1:25432/crossborder?sslmode=disable'
(cd crossborder && env -u GOROOT GOTOOLCHAIN=go1.26.6 go run ./cmd/migrate -up)
docker compose --env-file .env -f deploy/compose.yaml exec -T postgres \
  psql -U asterflow -d crossborder -v ON_ERROR_STOP=1 < crossborder/seed/demo.sql
(cd crossborder && env -u GOROOT GOTOOLCHAIN=go1.26.6 go run ./cmd/simulator)
```

默认监听 `127.0.0.1:8091`。按 Ctrl+C 停止。重启运行最后一条命令。需要换端口时，可设置 `CROSSBORDER_ADDR=127.0.0.1:8092`，同时调整工具端点。种子 SQL 可重复执行，保留已有记录。

## 测试查询

```bash
curl -fsS http://127.0.0.1:8091/healthz
curl -fsS http://127.0.0.1:8091/tools/get_order \
  -H 'Content-Type: application/json' -d '{"order_id":"TTS-20260801-1001"}'
curl -fsS http://127.0.0.1:8091/tools/get_inventory \
  -H 'Content-Type: application/json' -d '{"sku":"SKU-BLACK-M-01"}'
```

种子数据中的订单处于待发货状态。该 SKU 在洛杉矶仓 `WH-US-LAX` 的可用量为 18，在深圳仓 `WH-CN-SZ` 为 0。

在 AsterFlow 本地开发环境，将 `127.0.0.1` 加入 `ASTERFLOW_TOOL_ALLOWED_HOSTS` 后重启平台。分别以这两个端点创建 REST 工具，试跑并发布，再把工具选入 Agent 版本。

```mermaid
flowchart LR
    Platform["AsterFlow REST 执行器"] --> API["Crossborder HTTP"]
    API --> Service["业务 Service"]
    Service --> Store["PostgresStore"]
    Store --> Queries["sqlc Queries"]
    Queries --> PG[("crossborder 数据库")]
```

使用独立开发测试库时，可设 `CROSSBORDER_TEST_DATABASE_URL` 后在本目录运行 `go test -race -count=1 ./...`。见 [G07 验收记录](../docs/delivery/G07-VERIFICATION.md)。
