# G07 工具试跑记录迁移

固定 Final 的试跑会真实调用工具，将结果保存到 tool_test_runs。发布查询按 tool_version_id 查最新一次成功试跑。固定版执行完成后再次查当前版本；如果试跑过程中有人追加草稿，可能写错 VersionID。AsterFlow 后续执行时把选中的 VersionID 和执行结果一起交给记录层。保留 Final 的最新草稿试跑与指定版本发布流程。

学员已保存本批迁移、查询和领域类型。正式工作区隔离 PostgreSQL race 已通过。覆盖有效试跑、同一版本最新成功、另一版本不能复用成功、工具版本组合错配、非法状态/耗时/错误、级联删除与回退重建。下面保留手写代码供对照。

## 迁移

新增 `migrations/000013_tool_test_runs.up.sql`。

```sql
ALTER TABLE tool_versions
    ADD CONSTRAINT tool_versions_tool_id_id_unique UNIQUE (tool_id, id);

CREATE TABLE tool_test_runs (
    id UUID PRIMARY KEY,
    tool_id UUID NOT NULL,
    tool_version_id UUID NOT NULL,
    input TEXT NOT NULL DEFAULT '',
    output TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('success', 'error')),
    latency_ms INT NOT NULL DEFAULT 0 CHECK (latency_ms >= 0),
    error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- 试跑的工具 ID 和版本 ID 必须指向同一条版本记录。
    FOREIGN KEY (tool_id, tool_version_id)
        REFERENCES tool_versions(tool_id, id) ON DELETE CASCADE,

    CHECK ((status = 'success' AND error IS NULL)
        OR (status = 'error' AND error IS NOT NULL))
);

CREATE INDEX tool_test_runs_version_status_created_at
    ON tool_test_runs (tool_version_id, status, created_at DESC);
```

新增 `migrations/000013_tool_test_runs.down.sql`。

```sql
DROP TABLE tool_test_runs;

ALTER TABLE tool_versions
    DROP CONSTRAINT tool_versions_tool_id_id_unique;
```

## Go 领域类型

在 `internal/domain/tool.go` 的 `ToolVersion` 下追加，使用现有 time import。

```go
type ToolTestRun struct {
	ID            string    `json:"id"`
	ToolID        string    `json:"tool_id"`
	ToolVersionID string    `json:"tool_version_id"`
	Input         string    `json:"input"`
	Output        string    `json:"output"`
	Status        string    `json:"status"`
	LatencyMs     int       `json:"latency_ms"`
	Error         *string   `json:"error,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}
```

## 查询

在 `internal/infrastructure/postgres/queries/tool.sql` 的 `InsertToolVersion` 下追加。

```sql
-- name: CreateToolTestRun :one
INSERT INTO tool_test_runs (
    id, tool_id, tool_version_id, input, output, status, latency_ms, error
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetToolLastSuccessfulTestRunForVersion :one
SELECT *
FROM tool_test_runs
WHERE tool_version_id = $1
  AND status = 'success'
ORDER BY created_at DESC
LIMIT 1;
```

运行 `make sqlc-generate` 和 `go build ./cmd/server`。助手接手现有 `cmd/migrate/tool_registry_test.go` 的试跑回归，并更新以前只回退 000012 的测试，使其定位到要验证的迁移版本。后续补 Store 与执行入口，沿用真实 REST/internal SDK 调用、目标限制和试跑版本绑定。
