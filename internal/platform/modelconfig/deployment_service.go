package modelconfig

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/google/uuid"
)

type CreateDeploymentRequest struct {
	WorkspaceID                string  `json:"-"`
	ProviderAccountID          string  `json:"provider_account_id"`
	Name                       string  `json:"name"`
	ModelName                  string  `json:"model_name"`
	Region                     string  `json:"region"`
	TimeoutMS                  int     `json:"timeout_ms"`
	MaxRetries                 int     `json:"max_retries"`
	InputPricePerMillion       float64 `json:"input_price_per_million"`
	OutputPricePerMillion      float64 `json:"output_price_per_million"`
	CachedInputPricePerMillion float64 `json:"cached_input_price_per_million"`
	CreatedBy                  string  `json:"-"`
}

type UpdateDeploymentPricingRequest struct {
	WorkspaceID                string  `json:"-"`
	InputPricePerMillion       float64 `json:"input_price_per_million"`
	OutputPricePerMillion      float64 `json:"output_price_per_million"`
	CachedInputPricePerMillion float64 `json:"cached_input_price_per_million"`
}

var ErrDeploymentPriceRequired = errors.New(
	"deployment pricing is required by a positive-budget binding",
)

// 判断价格是否合法
func validateDeploymentPrices(input, output, cached float64) error {
	for _, price := range [3]float64{input, output, cached} {
		if math.IsNaN(price) || math.IsInf(price, 0) || price < 0 {
			return fmt.Errorf(
				"%w: model prices must be finite and nonnegative",
				ErrInvalidArgument,
			)
		}
	}
	return nil
}

func (s *Service) CreateDeployment(
	ctx context.Context,
	req CreateDeploymentRequest,
) (*ModelDeployment, error) {
	req.WorkspaceID = strings.TrimSpace(req.WorkspaceID)
	req.ProviderAccountID = strings.TrimSpace(req.ProviderAccountID)
	req.Name = strings.TrimSpace(req.Name)
	req.ModelName = strings.TrimSpace(req.ModelName)
	req.Region = strings.TrimSpace(req.Region)

	if req.WorkspaceID == "" || req.Name == "" || req.ModelName == "" {
		return nil, fmt.Errorf(
			"%w: workspace_id, name and model_name are required",
			ErrInvalidArgument,
		)
	}

	if _, err := uuid.Parse(req.ProviderAccountID); err != nil {
		return nil, fmt.Errorf(
			"%w: provider_account_id must be a UUID",
			ErrInvalidArgument,
		)
	}

	if req.TimeoutMS <= 0 {
		req.TimeoutMS = DefaultDeploymentTimeoutMS
	}
	if req.MaxRetries < 0 {
		req.MaxRetries = 0
	}
	if req.TimeoutMS > math.MaxInt32 || req.MaxRetries > math.MaxInt32 {
		return nil, fmt.Errorf(
			"%w: timeout or retries exceeds the supported range",
			ErrInvalidArgument,
		)
	}

	if err := validateDeploymentPrices(
		req.InputPricePerMillion,
		req.OutputPricePerMillion,
		req.CachedInputPricePerMillion,
	); err != nil {
		return nil, err
	}

	account, err := s.store.GetProviderAccount(ctx, req.ProviderAccountID)
	if err != nil {
		return nil, fmt.Errorf("get provider account: %w", err)
	}
	if account.WorkspaceID != req.WorkspaceID {
		return nil, ErrProviderAccountNotFound
	}

	deployment := &ModelDeployment{
		ID:                         uuid.NewString(),
		WorkspaceID:                req.WorkspaceID,
		ProviderAccountID:          account.ID,
		Name:                       req.Name,
		ModelName:                  req.ModelName,
		Region:                     req.Region,
		TimeoutMS:                  req.TimeoutMS,
		MaxRetries:                 req.MaxRetries,
		InputPricePerMillion:       req.InputPricePerMillion,
		OutputPricePerMillion:      req.OutputPricePerMillion,
		CachedInputPricePerMillion: req.CachedInputPricePerMillion,
		Status:                     "active",
		CreatedBy:                  req.CreatedBy,
	}
	if err := s.store.CreateDeployment(ctx, deployment); err != nil {
		return nil, fmt.Errorf("create model deployment: %w", err)
	}
	return deployment, nil
}

func (s *Service) ListDeployments(
	ctx context.Context,
	workspaceID string,
) ([]*ModelDeployment, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf(
			"%w: workspace_id is required",
			ErrInvalidArgument,
		)
	}

	return s.store.ListDeployments(ctx, workspaceID)
}

func (s *Service) UpdateDeploymentPricing(
	ctx context.Context,
	deploymentID string,
	req UpdateDeploymentPricingRequest,
) (*ModelDeployment, error) {
	req.WorkspaceID = strings.TrimSpace(req.WorkspaceID)
	deploymentID = strings.TrimSpace(deploymentID)

	if req.WorkspaceID == "" {
		return nil, fmt.Errorf(
			"%w: workspace_id is required",
			ErrInvalidArgument,
		)
	}
	if _, err := uuid.Parse(deploymentID); err != nil {
		return nil, fmt.Errorf(
			"%w: deployment_id must be a UUID",
			ErrInvalidArgument,
		)
	}

	if err := validateDeploymentPrices(
		req.InputPricePerMillion,
		req.OutputPricePerMillion,
		req.CachedInputPricePerMillion,
	); err != nil {
		return nil, err
	}

	deployment, err := s.store.GetDeployment(ctx, deploymentID)
	if err != nil {
		return nil, fmt.Errorf("get model deployment: %w", err)
	}
	if deployment.WorkspaceID != req.WorkspaceID {
		return nil, ErrDeploymentNotFound
	}

	var updated *ModelDeployment
	err = s.store.WithWorkspaceModelWrite(ctx, req.WorkspaceID, func(w WorkspaceModelWriter) error {
		//如果输入和输出价格都是0的时候,查询是否有正预算绑定这个部署
		if req.InputPricePerMillion == 0 && req.OutputPricePerMillion == 0 {
			inUse, err := w.HasBudgetBindingForDeployment(ctx, req.WorkspaceID, deployment.ID)
			if err != nil {
				return err
			}
			if inUse {
				return ErrDeploymentPriceRequired
			}
		}

		if err := w.UpdateDeploymentPricing(
			ctx,
			deployment.ID,
			req.InputPricePerMillion,
			req.OutputPricePerMillion,
			req.CachedInputPricePerMillion,
		); err != nil {
			return fmt.Errorf("update model deployment pricing: %w", err)
		}

		saved, err := w.GetDeployment(ctx, deployment.ID)
		if err != nil {
			return fmt.Errorf("read updated deployment pricing: %w", err)
		}
		updated = saved
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}
