package modelconfig

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type CreateProfileRequest struct {
	WorkspaceID           string   `json:"-"`
	Name                  string   `json:"name"`
	Description           string   `json:"description"`
	PrimaryDeploymentID   string   `json:"primary_deployment_id"`
	FallbackDeploymentIDs []string `json:"fallback_deployment_ids"`
	ClassificationMax     string   `json:"classification_max"`
	CreatedBy             string   `json:"-"`
}

func (s *Service) resolveProfileDeployments(
	ctx context.Context,
	workspaceID, primary string,
	fallbacks []string,
) (string, []string, error) {
	ids := append([]string{primary}, fallbacks...)
	resolved := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))

	for _, raw := range ids {
		//把首选和次选都解析为uuid
		id, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			return "", nil, fmt.Errorf(
				"%w: deployment ids must be UUIDs",
				ErrInvalidArgument,
			)
		}

		canonical := id.String()
		if _, exists := seen[canonical]; !exists {
			deployment, err := s.store.GetDeployment(ctx, canonical)
			if err != nil {
				return "", nil, fmt.Errorf(
					"get profile deployment: %w",
					err,
				)
			}
			if deployment.WorkspaceID != workspaceID {
				return "", nil, ErrDeploymentNotFound
			}
			seen[canonical] = struct{}{}
		}

		resolved = append(resolved, canonical)
	}

	return resolved[0], resolved[1:], nil
}

func (s *Service) CreateProfile(
	ctx context.Context,
	req CreateProfileRequest,
) (*ModelProfile, *ModelProfileVersion, error) {
	req.WorkspaceID = strings.TrimSpace(req.WorkspaceID)
	req.Name = strings.TrimSpace(req.Name)
	req.Description = strings.TrimSpace(req.Description)
	req.ClassificationMax = strings.TrimSpace(req.ClassificationMax)

	if req.WorkspaceID == "" || req.Name == "" {
		return nil, nil, fmt.Errorf(
			"%w: workspace_id and name are required",
			ErrInvalidArgument,
		)
	}
	if req.ClassificationMax == "" {
		req.ClassificationMax = "internal"
	}
	primary, fallbacks, err := s.resolveProfileDeployments(
		ctx,
		req.WorkspaceID,
		req.PrimaryDeploymentID,
		req.FallbackDeploymentIDs,
	)
	if err != nil {
		return nil, nil, err
	}
	profile := &ModelProfile{
		ID:          uuid.NewString(),
		WorkspaceID: req.WorkspaceID,
		Name:        req.Name,
		Description: req.Description,
		CreatedBy:   req.CreatedBy,
	}

	version := &ModelProfileVersion{
		ID:                    uuid.NewString(),
		ProfileID:             profile.ID,
		Version:               1,
		PrimaryDeploymentID:   primary,
		FallbackDeploymentIDs: fallbacks,
		ClassificationMax:     req.ClassificationMax,
		CreatedBy:             req.CreatedBy,
	}
	if err := s.store.CreateProfileWithVersion(
		ctx,
		profile,
		version,
	); err != nil {
		return nil, nil, fmt.Errorf("create model profile: %w", err)
	}

	return profile, version, nil
}

func (s *Service) GetProfile(
	ctx context.Context,
	workspaceID, profileID string,
) (*ModelProfile, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf(
			"%w: workspace_id is required",
			ErrInvalidArgument,
		)
	}

	id, err := uuid.Parse(strings.TrimSpace(profileID))
	if err != nil {
		return nil, fmt.Errorf(
			"%w: profile_id must be a UUID",
			ErrInvalidArgument,
		)
	}
	profile, err := s.store.GetProfile(ctx, id.String())
	if err != nil {
		return nil, fmt.Errorf("get model profile: %w", err)
	}
	if profile.WorkspaceID != workspaceID {
		return nil, ErrProfileNotFound
	}

	return profile, nil
}

func (s *Service) ListProfiles(
	ctx context.Context,
	workspaceID string,
) ([]*ModelProfile, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf(
			"%w: workspace_id is required",
			ErrInvalidArgument,
		)
	}

	return s.store.ListProfiles(ctx, workspaceID)
}

func (s *Service) ListProfileVersions(
	ctx context.Context,
	workspaceID, profileID string,
) ([]*ModelProfileVersion, error) {
	profile, err := s.GetProfile(ctx, workspaceID, profileID)
	if err != nil {
		return nil, err
	}

	return s.store.ListProfileVersions(ctx, profile.ID)
}

type CreateProfileVersionRequest struct {
	WorkspaceID           string   `json:"-"`
	PrimaryDeploymentID   string   `json:"primary_deployment_id"`
	FallbackDeploymentIDs []string `json:"fallback_deployment_ids"`
	ClassificationMax     string   `json:"classification_max"`
	CreatedBy             string   `json:"-"`
}

func (s *Service) CreateProfileVersion(
	ctx context.Context,
	profileID string,
	req CreateProfileVersionRequest,
) (*ModelProfileVersion, error) {
	profile, err := s.GetProfile(ctx, req.WorkspaceID, profileID)
	if err != nil {
		return nil, err
	}

	primary, fallbacks, err := s.resolveProfileDeployments(
		ctx,
		profile.WorkspaceID,
		req.PrimaryDeploymentID,
		req.FallbackDeploymentIDs,
	)
	if err != nil {
		return nil, err
	}

	classification := strings.TrimSpace(req.ClassificationMax)
	if classification == "" {
		classification = "internal"
	}

	version := &ModelProfileVersion{
		ID:                    uuid.NewString(),
		ProfileID:             profile.ID,
		PrimaryDeploymentID:   primary,
		FallbackDeploymentIDs: fallbacks,
		ClassificationMax:     classification,
		CreatedBy:             req.CreatedBy,
	}

	if err := s.store.CreateProfileVersion(ctx, version); err != nil {
		return nil, fmt.Errorf("create model profile version: %w", err)
	}

	return version, nil
}
