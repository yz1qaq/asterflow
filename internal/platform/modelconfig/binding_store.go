package modelconfig

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	pgstore "github.com/yz1qaq/asterflow/internal/infrastructure/postgres/sqlc"
)

var ErrProjectBindingNotFound = errors.New("project model binding not found")

func projectBindingFromRow(row pgstore.ProjectModelBinding) (*ProjectBinding, error) {
	budget, err := row.MonthlyBudget.Float64Value()
	if err != nil {
		return nil, fmt.Errorf("decode monthly budget: %w", err)
	}
	if !budget.Valid || math.IsNaN(budget.Float64) ||
		math.IsInf(budget.Float64, 0) || budget.Float64 < 0 {
		return nil, fmt.Errorf("stored monthly budget is invalid")
	}

	return &ProjectBinding{
		WorkspaceID:           row.WorkspaceID,
		Env:                   row.Env,
		ModelProfileVersionID: row.ModelProfileVersionID.String(),
		MonthlyBudget:         budget.Float64,
		RPMLimit:              int(row.RpmLimit),
		TPMLimit:              int(row.TpmLimit),
		UpdatedAt:             row.UpdatedAt,
	}, nil

}

func (s *PostgresStore) UpsertProjectBinding(
	ctx context.Context,
	binding *ProjectBinding,
) error {
	if binding == nil {
		return fmt.Errorf("project model binding is required")
	}
	versionID, err := uuid.Parse(binding.ModelProfileVersionID)
	if err != nil {
		return fmt.Errorf("parse profile version id: %w", err)
	}

	value := binding.MonthlyBudget
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return fmt.Errorf("monthly budget must be finite and nonnegative")
	}
	if binding.RPMLimit < 0 || binding.RPMLimit > math.MaxInt32 ||
		binding.TPMLimit < 0 || binding.TPMLimit > math.MaxInt32 {
		return fmt.Errorf("rpm and tpm limits must be between 0 and %d", math.MaxInt32)
	}
	var budget pgtype.Numeric
	if err := budget.Scan(strconv.FormatFloat(value, 'f', -1, 64)); err != nil {
		return fmt.Errorf("encode monthly budget: %w", err)
	}
	row, err := s.q.UpsertProjectBinding(ctx, pgstore.UpsertProjectBindingParams{
		WorkspaceID:           binding.WorkspaceID,
		Env:                   binding.Env,
		ModelProfileVersionID: versionID,
		MonthlyBudget:         budget,
		RpmLimit:              int32(binding.RPMLimit),
		TpmLimit:              int32(binding.TPMLimit),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrProfileVersionNotFound
	}
	if err != nil {
		return fmt.Errorf("upsert project model binding: %w", err)
	}
	saved, err := projectBindingFromRow(row)
	if err != nil {
		return err
	}
	*binding = *saved
	return nil
}

func (s *PostgresStore) GetProjectBinding(
	ctx context.Context,
	workspaceID, env string,
) (*ProjectBinding, error) {
	row, err := s.q.GetProjectBinding(ctx, pgstore.GetProjectBindingParams{
		WorkspaceID: workspaceID,
		Env:         env,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrProjectBindingNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get project model binding: %w", err)
	}
	return projectBindingFromRow(row)
}

func (s *PostgresStore) ListWorkspaceProfileVersions(
	ctx context.Context,
	workspaceID string,
) ([]*ModelProfileVersion, error) {
	rows, err := s.q.ListWorkspaceProfileVersions(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list workspace profile versions: %w", err)
	}

	versions := make([]*ModelProfileVersion, 0, len(rows))
	for _, row := range rows {
		version, err := profileVersionFromRow(row)
		if err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}
	return versions, nil
}
