package modelconfig

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	pgstore "github.com/yz1qaq/asterflow/internal/infrastructure/postgres/sqlc"
)

var (
	ErrDeploymentNotFound   = errors.New("model deployment not found")
	ErrDeploymentNameExists = errors.New("model deployment name already exists")
)

//把float64转换为数据库的Numeric
func deploymentPricesToNumeric(
	input, output, cached float64,
) ([3]pgtype.Numeric, error) {
	var result [3]pgtype.Numeric

	for i, value := range [3]float64{input, output, cached} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return result, fmt.Errorf(
				"deployment price must be finite and nonnegative",
			)
		}

		if err := result[i].Scan(
			strconv.FormatFloat(value, 'f', -1, 64),
		); err != nil {
			return result, fmt.Errorf("encode deployment price: %w", err)
		}
	}

	return result, nil
}

//把row转换为deployment
func deploymentFromRow(
	row pgstore.ModelDeployment,
) (*ModelDeployment, error) {
	var prices [3]float64

	for i, value := range [3]pgtype.Numeric{
		row.InputPricePerMillion,
		row.OutputPricePerMillion,
		row.CachedInputPricePerMillion,
	} {
		price, err := value.Float64Value()
		if err != nil {
			return nil, fmt.Errorf("decode deployment price: %w", err)
		}

		if !price.Valid ||
			math.IsNaN(price.Float64) ||
			math.IsInf(price.Float64, 0) ||
			price.Float64 < 0 {
			return nil, fmt.Errorf("stored deployment price is invalid")
		}

		prices[i] = price.Float64
	}

	return &ModelDeployment{
		ID:                         row.ID.String(),
		WorkspaceID:                row.WorkspaceID,
		ProviderAccountID:          row.ProviderAccountID.String(),
		Name:                       row.Name,
		ModelName:                  row.ModelName,
		Region:                     row.Region,
		TimeoutMS:                  int(row.TimeoutMs),
		MaxRetries:                 int(row.MaxRetries),
		InputPricePerMillion:       prices[0],
		OutputPricePerMillion:      prices[1],
		CachedInputPricePerMillion: prices[2],
		Status:                     row.Status,
		CreatedBy:                  row.CreatedBy,
		CreatedAt:                  row.CreatedAt,
		UpdatedAt:                  row.UpdatedAt,
	}, nil
}

func (s *PostgresStore) CreateDeployment(
	ctx context.Context,
	deployment *ModelDeployment,
) error {
	if deployment == nil {
		return fmt.Errorf("model deployment is required")
	}

	id, err := uuid.Parse(deployment.ID)
	if err != nil {
		return fmt.Errorf("parse deployment id: %w", err)
	}

	accountID, err := uuid.Parse(deployment.ProviderAccountID)
	if err != nil {
		return fmt.Errorf("parse provider account id: %w", err)
	}

	if deployment.TimeoutMS <= 0 ||
		deployment.TimeoutMS > math.MaxInt32 ||
		deployment.MaxRetries < 0 ||
		deployment.MaxRetries > math.MaxInt32 {
		return fmt.Errorf(
			"deployment timeout or retries is outside the supported range",
		)
	}

	prices, err := deploymentPricesToNumeric(
		deployment.InputPricePerMillion,
		deployment.OutputPricePerMillion,
		deployment.CachedInputPricePerMillion,
	)
	if err != nil {
		return err
	}

	row, err := s.q.CreateDeployment(
		ctx,
		pgstore.CreateDeploymentParams{
			ID:                         id,
			WorkspaceID:                deployment.WorkspaceID,
			ProviderAccountID:          accountID,
			Name:                       deployment.Name,
			ModelName:                  deployment.ModelName,
			Region:                     deployment.Region,
			TimeoutMs:                  int32(deployment.TimeoutMS),
			MaxRetries:                 int32(deployment.MaxRetries),
			InputPricePerMillion:       prices[0],
			OutputPricePerMillion:      prices[1],
			CachedInputPricePerMillion: prices[2],
			Status:                     deployment.Status,
			CreatedBy:                  deployment.CreatedBy,
		},
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) &&
			pgErr.Code == "23505" &&
			pgErr.ConstraintName == "model_deployments_workspace_id_name_key" {
			return ErrDeploymentNameExists
		}
		return fmt.Errorf("create model deployment: %w", err)
	}

	saved, err := deploymentFromRow(row)
	if err != nil {
		return err
	}

	*deployment = *saved
	return nil
}

func (s *PostgresStore) GetDeployment(
	ctx context.Context,
	deploymentID string,
) (*ModelDeployment, error) {
	id, err := uuid.Parse(deploymentID)
	if err != nil {
		return nil, fmt.Errorf("parse deployment id: %w", err)
	}

	row, err := s.q.GetDeployment(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrDeploymentNotFound
		}
		return nil, fmt.Errorf("get model deployment: %w", err)
	}

	return deploymentFromRow(row)
}

func (s *PostgresStore) ListDeployments(
	ctx context.Context,
	workspaceID string,
) ([]*ModelDeployment, error) {
	rows, err := s.q.ListDeployments(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list model deployments: %w", err)
	}

	deployments := make([]*ModelDeployment, 0, len(rows))
	for _, row := range rows {
		deployment, err := deploymentFromRow(row)
		if err != nil {
			return nil, err
		}
		deployments = append(deployments, deployment)
	}

	return deployments, nil
}

func (s *PostgresStore) UpdateDeploymentPricing(
	ctx context.Context,
	deploymentID string,
	input, output, cached float64,
) error {
	id, err := uuid.Parse(deploymentID)
	if err != nil {
		return fmt.Errorf("parse deployment id: %w", err)
	}

	prices, err := deploymentPricesToNumeric(input, output, cached)
	if err != nil {
		return err
	}

	affected, err := s.q.UpdateDeploymentPricing(
		ctx,
		pgstore.UpdateDeploymentPricingParams{
			ID:                         id,
			InputPricePerMillion:       prices[0],
			OutputPricePerMillion:      prices[1],
			CachedInputPricePerMillion: prices[2],
		},
	)
	if err != nil {
		return fmt.Errorf("update model deployment pricing: %w", err)
	}
	if affected != 1 {
		return ErrDeploymentNotFound
	}

	return nil
}
