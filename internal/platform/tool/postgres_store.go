package tool

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yz1qaq/asterflow/internal/domain"
	pgstore "github.com/yz1qaq/asterflow/internal/infrastructure/postgres/sqlc"
)

type toolDB interface {
	pgstore.DBTX
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

type PostgresStore struct {
	q  *pgstore.Queries
	db toolDB
}

func NewPostgresStore(db toolDB) *PostgresStore {
	return &PostgresStore{db: db, q: pgstore.New(db)}
}

func (s *PostgresStore) CreateToolWithVersion(
	ctx context.Context,
	t *domain.Tool,
	v *domain.ToolVersion,
) error {
	toolID, err := uuid.Parse(t.ID)
	if err != nil {
		return fmt.Errorf("parse tool id: %w", err)
	}

	versionID, err := uuid.Parse(v.ID)
	if err != nil {
		return fmt.Errorf("parse tool version id: %w", err)
	}

	if err := s.q.CreateToolWithVersion(ctx, pgstore.CreateToolWithVersionParams{
		ToolID:              toolID,
		WorkspaceID:         t.WorkspaceID,
		Name:                t.Name,
		Description:         t.Description,
		SourceType:          t.SourceType,
		Sensitive:           t.Sensitive,
		ClassificationMax:   "internal",
		ToolCreatedBy:       t.CreatedBy,
		VersionID:           versionID,
		SchemaJson:          v.SchemaJSON,
		EndpointConfig:      v.EndpointConfig,
		AuthConfig:          v.AuthConfig,
		AuthConfigEncrypted: v.AuthConfigEncrypted,
		RetryPolicy:         v.RetryPolicy,
		VersionStatus:       v.Status,
		VersionCreatedBy:    v.CreatedBy,
	}); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "tools_workspace_name_unique" {
			return ErrToolAlreadyExists
		}
		return fmt.Errorf("create tool with version: %w", err)
	}

	return nil
}

func (s *PostgresStore) ListTools(ctx context.Context, workspaceID string) ([]*domain.Tool, error) {
	rows, err := s.q.ListTools(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list tools: %w", err)
	}

	tools := make([]*domain.Tool, 0, len(rows))
	for _, row := range rows {
		tools = append(tools, toolFromRow(row))
	}
	return tools, nil
}

func toolFromRow(row pgstore.Tool) *domain.Tool {
	return &domain.Tool{
		ID:          row.ID.String(),
		WorkspaceID: row.WorkspaceID,
		Name:        row.Name,
		SourceType:  row.SourceType,
		Description: row.Description,
		Sensitive:   row.Sensitive,
		CreatedBy:   row.CreatedBy,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}

func (s *PostgresStore) ListToolVersions(ctx context.Context, toolID string) ([]*domain.ToolVersion, error) {
	id, err := uuid.Parse(toolID)
	if err != nil {
		return nil, fmt.Errorf("parse tool id: %w", err)
	}

	rows, err := s.q.ListToolVersions(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list tool versions: %w", err)
	}

	versions := make([]*domain.ToolVersion, 0, len(rows))
	for _, row := range rows {
		versions = append(versions, toolVersionFromRow(row))
	}
	return versions, nil
}

func toolVersionFromRow(row pgstore.ToolVersion) *domain.ToolVersion {
	legacyAuth := strings.TrimSpace(row.AuthConfig)
	return &domain.ToolVersion{
		ID:                  row.ID.String(),
		ToolID:              row.ToolID.String(),
		Version:             int(row.Version),
		SchemaJSON:          row.SchemaJson,
		EndpointConfig:      row.EndpointConfig,
		AuthConfig:          row.AuthConfig,
		AuthConfigEncrypted: append([]byte(nil), row.AuthConfigEncrypted...),
		HasAuth:             len(row.AuthConfigEncrypted) > 0 || (legacyAuth != "" && legacyAuth != "{}"),
		RetryPolicy:         row.RetryPolicy,
		Status:              row.Status,
		CreatedBy:           row.CreatedBy,
		CreatedAt:           row.CreatedAt,
	}
}

func (s *PostgresStore) GetTool(ctx context.Context, toolID string) (*domain.Tool, error) {
	id, err := uuid.Parse(toolID)
	if err != nil {
		return nil, ErrToolNotFound
	}

	row, err := s.q.GetTool(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrToolNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get tool: %w", err)
	}
	return toolFromRow(row), nil
}

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

func (s *PostgresStore) AppendToolVersion(
	ctx context.Context, workspaceID string, baseVersion int, v *domain.ToolVersion,
) error {
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
	// baseVersion 为 0 时，调用方提供的是独立配置，无需校验继承来源。
	if baseVersion > 0 && int(next)-1 != baseVersion {
		return ErrToolVersionConflict
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

func (s *PostgresStore) PublishToolVersion(ctx context.Context, toolID, versionID string) error {
	tid, err := uuid.Parse(toolID)
	if err != nil {
		return ErrToolNotFound
	}
	vid, err := uuid.Parse(versionID)
	if err != nil {
		return ErrToolNotFound
	}

	// SQL 只允许 draft 变为 published。并发请求中只有一个能更新这一行。
	affected, err := s.q.PublishToolVersion(ctx, pgstore.PublishToolVersionParams{
		ToolID: tid, VersionID: vid,
	})
	if err != nil {
		return fmt.Errorf("publish tool version: %w", err)
	}
	if affected == 0 {
		return ErrToolVersionNotDraft
	}
	return nil
}

func (s *PostgresStore) GetToolLatestPublishedVersionID(ctx context.Context, toolID string) (string, error) {
	id, err := uuid.Parse(toolID)
	if err != nil {
		return "", ErrToolNotFound
	}

	versionID, err := s.q.GetToolLatestPublishedVersionID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoPublishedToolVersion
	}
	if err != nil {
		return "", fmt.Errorf("get latest published tool version: %w", err)
	}
	return versionID.String(), nil
}
