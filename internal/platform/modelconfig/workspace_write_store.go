package modelconfig

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	pgstore "github.com/yz1qaq/asterflow/internal/infrastructure/postgres/sqlc"
)

type WorkspaceModelWriter interface {
	GetDeployment(context.Context, string) (*ModelDeployment, error)
	UpdateDeploymentPricing(context.Context, string, float64, float64, float64) error
	HasBudgetBindingForDeployment(context.Context, string, string) (bool, error)
	UpsertProjectBinding(context.Context, *ProjectBinding) error
}

func (s *PostgresStore) WithWorkspaceModelWrite(
	ctx context.Context,
	workspaceID string,
	write func(WorkspaceModelWriter) error,
) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin workspace model write: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.q.WithTx(tx)
	if _, err := q.LockWorkspace(ctx, workspaceID); err != nil {
		return fmt.Errorf("lock workspace for model write: %w", err)
	}

	txStore := &PostgresStore{q: q}
	if err := write(txStore); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit workspace model write: %w", err)
	}
	return nil
}

// 当前空间有没有一个月预算大于 0 的环境，绑定的版本把这个部署用作主部署或备用部署？ 有一个就返回 true
func (s *PostgresStore) HasBudgetBindingForDeployment(
	ctx context.Context,
	workspaceID, deploymentID string,
) (bool, error) {
	id, err := uuid.Parse(deploymentID)
	if err != nil {
		return false, fmt.Errorf("parse deployment id: %w", err)
	}

	hasBinding, err := s.q.HasBudgetBindingForDeployment(
		ctx, pgstore.HasBudgetBindingForDeploymentParams{
			WorkspaceID:  workspaceID,
			DeploymentID: id,
		},
	)
	if err != nil {
		return false, fmt.Errorf("check deployment budget bindings: %w", err)
	}
	return hasBinding, nil
}
