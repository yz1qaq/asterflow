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
