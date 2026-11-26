# G07 工具试跑记录的 Store 和 Service

迁移 000013 已由学员保存，助手在既有迁移测试中验证 000012 与 000013 的事务、查询和回退。测试按各自需要的迁移文件构建目录，后续递增迁移版本不会改变被测回退目标。

学员已保存这一批 Store 与 Service，主动版本回归已并入现有迁移测试并通过。固定 Final 实际执行后写入 tool_test_runs，发布按具体 VersionID 查询成功记录。AsterFlow 在 Service 接收执行开始时持有的 VersionID，避免写记录时重读当前版本造成错绑。候选隔离 PostgreSQL race 验证了 v2 已存在时仍能给 v1 记成功、v2 失败不能通过成功查询、跨空间或错误的工具/版本组合被拒绝。

## Service 契约

在 `internal/platform/tool/service.go` 的现有错误变量块增加。

```go
ErrNoSuccessfulTestRun = errors.New("no successful test run for tool version")
```

在同一文件的 Store 接口末尾增加。

```go
CreateTestRun(ctx context.Context, run *domain.ToolTestRun) error
GetToolLastSuccessfulTestRunForVersion(ctx context.Context, versionID string) (*domain.ToolTestRun, error)
```

## Store

新增 `internal/platform/tool/test_run_store.go`，内容如下。

```go
package tool

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/yz1qaq/asterflow/internal/domain"
	pgstore "github.com/yz1qaq/asterflow/internal/infrastructure/postgres/sqlc"
)

func (s *PostgresStore) CreateTestRun(ctx context.Context, run *domain.ToolTestRun) error {
	if run == nil {
		return errors.New("tool test run is required")
	}
	if run.LatencyMs < 0 || run.LatencyMs > math.MaxInt32 {
		return errors.New("tool test run latency is out of range")
	}
	id, err := uuid.Parse(run.ID)
	if err != nil {
		return fmt.Errorf("parse test run id: %w", err)
	}
	toolID, err := uuid.Parse(run.ToolID)
	if err != nil {
		return fmt.Errorf("parse tool id: %w", err)
	}
	versionID, err := uuid.Parse(run.ToolVersionID)
	if err != nil {
		return fmt.Errorf("parse tool version id: %w", err)
	}

	// Error 为 nil 时，pgtype.Text 的零值会写成 SQL NULL。
	var message pgtype.Text
	if run.Error != nil {
		message = pgtype.Text{String: *run.Error, Valid: true}
	}
	row, err := s.q.CreateToolTestRun(ctx, pgstore.CreateToolTestRunParams{
		ID:            id,
		ToolID:        toolID,
		ToolVersionID: versionID,
		Input:         run.Input,
		Output:        run.Output,
		Status:        run.Status,
		LatencyMs:     int32(run.LatencyMs),
		Error:         message,
	})
	if err != nil {
		return fmt.Errorf("create tool test run: %w", err)
	}

	// 插入成功后，把数据库生成的创建时间一起写回。
	*run = *toolTestRunFromRow(row)
	return nil
}

func (s *PostgresStore) GetToolLastSuccessfulTestRunForVersion(
	ctx context.Context, versionID string,
) (*domain.ToolTestRun, error) {
	id, err := uuid.Parse(versionID)
	if err != nil {
		return nil, fmt.Errorf("parse tool version id: %w", err)
	}
	row, err := s.q.GetToolLastSuccessfulTestRunForVersion(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoSuccessfulTestRun
	}
	if err != nil {
		return nil, fmt.Errorf("get last successful tool test run: %w", err)
	}
	return toolTestRunFromRow(row), nil
}

func toolTestRunFromRow(row pgstore.ToolTestRun) *domain.ToolTestRun {
	run := &domain.ToolTestRun{
		ID:            row.ID.String(),
		ToolID:        row.ToolID.String(),
		ToolVersionID: row.ToolVersionID.String(),
		Input:         row.Input,
		Output:        row.Output,
		Status:        row.Status,
		LatencyMs:     int(row.LatencyMs),
		CreatedAt:     row.CreatedAt,
	}
	if row.Error.Valid {
		message := row.Error.String
		run.Error = &message
	}
	return run
}
```

## Service

在 `internal/platform/tool/service.go` 末尾追加，沿用现有 imports。

```go
func (s *Service) RecordTestRun(
	ctx context.Context,
	workspaceID, toolID, versionID string,
	input, output string,
	latencyMs int,
	runErr error,
) (*domain.ToolTestRun, error) {
	if err := s.EnsureToolWorkspace(ctx, toolID, workspaceID); err != nil {
		return nil, err
	}

	// 使用执行开始时选定的版本，避免并发追加版本后记错试跑归属。
	run := &domain.ToolTestRun{
		ID:            uuid.NewString(),
		ToolID:        toolID,
		ToolVersionID: versionID,
		Input:         input,
		Output:        output,
		Status:        "success",
		LatencyMs:     latencyMs,
	}
	if runErr != nil {
		message := runErr.Error()
		run.Status = "error"
		run.Error = &message
	}

	if err := s.store.CreateTestRun(ctx, run); err != nil {
		return nil, err
	}
	return run, nil
}
```

go build ./... 验证编译。RecordTestRun 回归见 cmd/migrate/tool_registry_test.go。保留 Final 的试跑、指定版本发布和七天窗口，发布前由同一 VersionID 的成功记录提供证据。
