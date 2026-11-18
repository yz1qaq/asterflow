package modelconfig

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/google/uuid"
)

type BindProjectRequest struct {
	WorkspaceID           string  `json:"-"`
	Env                   string  `json:"env"`
	ModelProfileVersionID string  `json:"model_profile_version_id"`
	MonthlyBudget         float64 `json:"monthly_budget"`
	RPMLimit              int     `json:"rpm_limit"`
	TPMLimit              int     `json:"tpm_limit"`
}

func (s *Service) BindProject(
	ctx context.Context,
	req BindProjectRequest,
) (*ProjectBinding, error) {
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	env := strings.TrimSpace(req.Env)
	if workspaceID == "" || env == "" {
		return nil, fmt.Errorf("%w: workspace_id and env are required", ErrInvalidArgument)
	}

	versionID, err := uuid.Parse(strings.TrimSpace(req.ModelProfileVersionID))
	if err != nil {
		return nil, fmt.Errorf("%w: model_profile_version_id must be a UUID", ErrInvalidArgument)
	}

	if math.IsNaN(req.MonthlyBudget) || math.IsInf(req.MonthlyBudget, 0) || req.MonthlyBudget < 0 {
		return nil, fmt.Errorf("%w: monthly_budget must be finite and nonnegative", ErrInvalidArgument)
	}

	if req.RPMLimit < 0 || req.RPMLimit > math.MaxInt32 ||
		req.TPMLimit < 0 || req.TPMLimit > math.MaxInt32 {
		return nil, fmt.Errorf(
			"%w: rpm_limit and tpm_limit must be between 0 and %d",
			ErrInvalidArgument, math.MaxInt32,
		)
	}

	version, err := s.store.GetProfileVersion(ctx, versionID.String())
	if err != nil {
		return nil, fmt.Errorf("get binding profile version: %w", err)
	}
	if _, err := s.GetProfile(ctx, workspaceID, version.ProfileID); err != nil {
		if errors.Is(err, ErrProfileNotFound) {
			return nil, ErrProfileVersionNotFound
		}
		return nil, err
	}

	binding := &ProjectBinding{
		WorkspaceID:           workspaceID,
		Env:                   env,
		ModelProfileVersionID: version.ID,
		MonthlyBudget:         req.MonthlyBudget,
		RPMLimit:              req.RPMLimit,
		TPMLimit:              req.TPMLimit,
	}
	if err := s.store.WithWorkspaceModelWrite(ctx, workspaceID, func(w WorkspaceModelWriter) error {
		if req.MonthlyBudget > 0 {
			if err := requireBindingPrices(ctx, w, workspaceID, version); err != nil {
				return err
			}
		}
		if err := w.UpsertProjectBinding(ctx, binding); err != nil {
			return fmt.Errorf("save project model binding: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return binding, nil
}

func requireBindingPrices(
	ctx context.Context,
	w WorkspaceModelWriter,
	workspaceID string,
	version *ModelProfileVersion,
) error {
	ids := append([]string{version.PrimaryDeploymentID}, version.FallbackDeploymentIDs...)
	seen := make(map[string]struct{}, len(ids))

	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}

		deployment, err := w.GetDeployment(ctx, id)
		if err != nil {
			return fmt.Errorf("get binding deployment: %w", err)
		}
		if deployment.WorkspaceID != workspaceID {
			return ErrDeploymentNotFound
		}
		if deployment.InputPricePerMillion == 0 && deployment.OutputPricePerMillion == 0 {
			return fmt.Errorf(
				"%w: monthly budget requires token prices on deployment %q",
				ErrInvalidArgument, deployment.Name,
			)
		}
	}
	return nil
}

func (s *Service) GetProjectBinding(
	ctx context.Context,
	workspaceID, env string,
) (*ProjectBinding, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf("%w: workspace_id is required", ErrInvalidArgument)
	}
	env = strings.TrimSpace(env)
	if env == "" {
		env = "dev"
	}
	return s.store.GetProjectBinding(ctx, workspaceID, env)
}

func (s *Service) ListWorkspaceProfileVersions(
	ctx context.Context,
	workspaceID string,
) ([]*ModelProfileVersion, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf("%w: workspace_id is required", ErrInvalidArgument)
	}
	return s.store.ListWorkspaceProfileVersions(ctx, workspaceID)
}
