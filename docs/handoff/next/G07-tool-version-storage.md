# G07 追加工具草稿的存储层

2026-11-25 在固定 Final 的独立演示实例验证了同一工具下 v1 和 v2 的 UUID 不同。追加时仅提供新 Schema，端点继承 v1。首次发布 v2 因无成功试跑被拒绝，试跑记录归属 v2，重新发布后 v2 为 published，v1 保持 draft。固定版要求成功记录属于指定版本，且距今不超过七天。

下面是学员本批要手写的实现。候选副本已通过 sqlc 生成、服务端编译和隔离 PostgreSQL race，正式工作区仍以学员保存后的代码为准。采用现有 Agent、Prompt 的行锁事务取号模式，解决固定版先取号后写入在并发时可能撞上唯一约束的问题。

## SQL

在 `internal/infrastructure/postgres/queries/tool.sql` 的 `GetTool` 下面追加，然后执行 `make sqlc-generate`。

```sql
-- name: GetToolCurrentVersion :one
SELECT * FROM tool_versions
WHERE tool_id = $1
ORDER BY version DESC
LIMIT 1;

-- name: LockToolForVersion :one
SELECT id FROM tools
WHERE id = sqlc.arg(tool_id)::uuid
  AND workspace_id = sqlc.arg(workspace_id)::text
FOR UPDATE;

-- name: NextToolVersion :one
SELECT COALESCE(MAX(version), 0) + 1 AS next_version
FROM tool_versions
WHERE tool_id = $1;

-- name: InsertToolVersion :one
INSERT INTO tool_versions (
    id, tool_id, version, schema_json, endpoint_config, auth_config,
    auth_config_encrypted, retry_policy, status, created_by
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'draft', $9)
RETURNING *;
```

## Store

在 `internal/platform/tool/postgres_store.go` 替换原来的 `PostgresStore` 和 `NewPostgresStore` 定义。使用文件已有的 `context`、`pgx` 和 `pgstore`。

```go
type toolDB interface {
	pgstore.DBTX
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

type PostgresStore struct {
	db toolDB
	q  *pgstore.Queries
}

func NewPostgresStore(db toolDB) *PostgresStore {
	return &PostgresStore{db: db, q: pgstore.New(db)}
}
```

在同一文件 `GetTool` 后追加。

```go
func (s *PostgresStore) GetToolCurrentVersion(ctx context.Context, toolID string) (*domain.ToolVersion, error) {
	id, err := uuid.Parse(toolID)
	if err != nil {
		return nil, ErrToolNotFound
	}
	row, err := s.q.GetToolCurrentVersion(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrToolNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get current tool version: %w", err)
	}
	return toolVersionFromRow(row), nil
}

func (s *PostgresStore) AppendToolVersion(ctx context.Context, workspaceID string, v *domain.ToolVersion) error {
	if v == nil {
		return errors.New("tool version is required")
	}
	toolID, err := uuid.Parse(v.ToolID)
	if err != nil {
		return ErrToolNotFound
	}
	versionID, err := uuid.Parse(v.ID)
	if err != nil {
		return fmt.Errorf("parse tool version id: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin tool version append: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	// 锁住当前空间的工具主记录，让同一工具的追加操作排队取号。
	if _, err := q.LockToolForVersion(ctx, pgstore.LockToolForVersionParams{
		ToolID: toolID, WorkspaceID: workspaceID,
	}); errors.Is(err, pgx.ErrNoRows) {
		return ErrToolNotFound
	} else if err != nil {
		return fmt.Errorf("lock tool for version: %w", err)
	}

	// 拿到锁之后再发一条 SQL，能读取前一个事务刚提交的版本号。
	next, err := q.NextToolVersion(ctx, toolID)
	if err != nil {
		return fmt.Errorf("get next tool version: %w", err)
	}
	row, err := q.InsertToolVersion(ctx, pgstore.InsertToolVersionParams{
		ID:                  versionID,
		ToolID:              toolID,
		Version:             next,
		SchemaJson:          v.SchemaJSON,
		EndpointConfig:      v.EndpointConfig,
		AuthConfig:          v.AuthConfig,
		AuthConfigEncrypted: v.AuthConfigEncrypted,
		RetryPolicy:         v.RetryPolicy,
		CreatedBy:           v.CreatedBy,
	})
	if err != nil {
		return fmt.Errorf("insert tool version: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tool version append: %w", err)
	}

	// 提交成功后把数据库生成的版本号和创建时间写回给调用方。
	*v = *toolVersionFromRow(row)
	return nil
}
```

## 启动装配

在 `cmd/server/main.go` 的工具初始化中，将 `tool.NewPostgresStore(pgstore.New(pool))` 改为 `tool.NewPostgresStore(pool)`，移除该文件的 `pgstore` import。

学员可先验证 `go build ./cmd/server`。学员保存后，助手更新已有三个测试文件的构造调用并把并发回归并入 `cmd/migrate/tool_create_test.go`。迁移测试用 `pgx.Conn`，HTTP 测试用 `pgxpool.Pool`。并发测试覆盖同一工具八个请求连续取得 v2–v9、旧 v1 保留、跨空间拒绝、失败不写回、按版本号取最新草稿。

随后接 Service、Gin 和 React。Service 以最大版本号的配置作继承来源、按工作空间查工具、复用校验与加密。发布安排在工具试跑及指定版本的成功记录之后。
