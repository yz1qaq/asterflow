# G07 执行前加载工具版本配置

固定 Final 的工具试跑从最大版本号加载配置，Agent 快照则按固定版本 UUID 加载。AsterFlow 保留两种用途，显式校验空间，在同一结构体中返回工具元数据、版本 UUID 和解析后的配置。配置包含解密凭据，禁止序列化。后续试跑使用同一个 VersionID 写记录。发布状态仍由发布和 Agent 版本绑定阶段的规则校验。

学员已保存。最新与指定版本、跨空间拒绝及 JSON 序列化隐藏凭据已通过正式隔离 PostgreSQL race。

## 查询和 Store

在 `internal/infrastructure/postgres/queries/tool.sql` 末尾追加。

```sql
-- name: GetToolVersion :one
SELECT *
FROM tool_versions
WHERE id = $1;
```

在 `internal/platform/tool/postgres_store.go` 末尾追加。

```go
func (s *PostgresStore) GetToolVersion(ctx context.Context, versionID string) (*domain.ToolVersion, error) {
	id, err := uuid.Parse(versionID)
	if err != nil {
		return nil, ErrToolNotFound
	}
	row, err := s.q.GetToolVersion(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrToolNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get tool version: %w", err)
	}
	return toolVersionFromRow(row), nil
}
```

在 `internal/platform/tool/service.go` 的 Store 接口中，在 GetToolCurrentVersion 后增加。

```go
GetToolVersion(ctx context.Context, versionID string) (*domain.ToolVersion, error)
```

## 执行前配置

新建 `internal/platform/tool/execution_config.go`。

```go
package tool

import (
	"context"
	"errors"

	"github.com/yz1qaq/asterflow/internal/domain"
)

type ExecutionConfig struct {
	Tool      *domain.Tool `json:"tool"`
	VersionID string       `json:"version_id"`
	Config    *ToolConfig  `json:"-"`
}

func (s *Service) GetToolConfigForTest(
	ctx context.Context, workspaceID, toolID string,
) (*ExecutionConfig, error) {
	item, err := s.store.GetTool(ctx, toolID)
	if err != nil {
		return nil, err
	}
	if item.WorkspaceID != workspaceID {
		return nil, ErrToolNotFound
	}

	// 试跑取版本号最大的一版，后续执行和记账都使用这次读到的版本。
	version, err := s.store.GetToolCurrentVersion(ctx, item.ID)
	if err != nil {
		return nil, err
	}
	return s.resolveExecutionConfig(ctx, item, version)
}

func (s *Service) GetToolConfigByVersion(
	ctx context.Context, workspaceID, versionID string,
) (*ExecutionConfig, error) {
	version, err := s.store.GetToolVersion(ctx, versionID)
	if err != nil {
		return nil, err
	}
	item, err := s.store.GetTool(ctx, version.ToolID)
	if err != nil {
		return nil, err
	}
	if item.WorkspaceID != workspaceID {
		return nil, ErrToolNotFound
	}
	return s.resolveExecutionConfig(ctx, item, version)
}

func (s *Service) resolveExecutionConfig(
	ctx context.Context, item *domain.Tool, version *domain.ToolVersion,
) (*ExecutionConfig, error) {
	// 先完成空间校验，再解密凭据并组装执行器需要的配置。
	auth, err := s.authPlain(version)
	if err != nil {
		return nil, err
	}
	cfg, err := toolConfigFromStrings(item.SourceType, version.SchemaJSON, version.EndpointConfig, auth)
	if err != nil {
		return nil, err
	}
	if s.policy == nil {
		return nil, errors.New("tool endpoint policy is not configured")
	}
	if err := s.policy.validateConfig(ctx, cfg.SourceType, cfg.EndpointConfig); err != nil {
		return nil, err
	}

	return &ExecutionConfig{Tool: item, VersionID: version.ID, Config: cfg}, nil
}
```

运行 make sqlc-generate 与 go build ./...。配置回归已并入 TestRegisterTool；后续参见 [REST 执行器](G07-rest-executor.md)。
