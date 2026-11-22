package modelconfig

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/yz1qaq/asterflow/internal/runtime/llm"
)

var ErrModelUnavailable = errors.New("no usable model deployment")

// ResolveProfile turns a pinned model profile version into an ordered, callable
// deployment list. API keys are decrypted here and never leave the process.
func (s *Service) ResolveProfile(
	ctx context.Context, workspaceID, versionID string,
) (*llm.ResolvedModelProfile, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf("workspace_id is required")
	}

	version, err := s.store.GetProfileVersion(ctx, strings.TrimSpace(versionID))
	if err != nil {
		return nil, err
	}
	profile, err := s.store.GetProfile(ctx, version.ProfileID)
	if err != nil {
		return nil, err
	}
	if profile.WorkspaceID != workspaceID {
		return nil, ErrProfileVersionNotFound
	}

	deploymentIDs := make([]string, 0, 1+len(version.FallbackDeploymentIDs))
	deploymentIDs = append(deploymentIDs, version.PrimaryDeploymentID)
	deploymentIDs = append(deploymentIDs, version.FallbackDeploymentIDs...)

	resolved := &llm.ResolvedModelProfile{
		VersionID:         version.ID,
		ClassificationMax: version.ClassificationMax,
	}
	for _, deploymentID := range deploymentIDs {
		deployment, err := s.store.GetDeployment(ctx, deploymentID)
		if errors.Is(err, ErrDeploymentNotFound) {
			continue // dangling fallback: skip instead of blocking the whole profile
		}
		if err != nil {
			return nil, err
		}
		if deployment.WorkspaceID != workspaceID {
			return nil, ErrDeploymentNotFound
		}
		if deployment.Status != "active" {
			continue
		}

		account, err := s.store.GetProviderAccount(ctx, deployment.ProviderAccountID)
		if err != nil {
			return nil, err
		}
		if account.WorkspaceID != workspaceID {
			return nil, ErrProviderAccountNotFound
		}
		if account.Status != "active" {
			continue
		}

		apiKey := ""
		if len(account.APIKeyCiphertext) > 0 {
			if s.cipher == nil {
				return nil, fmt.Errorf("credential cipher is not configured")
			}
			apiKey, err = s.cipher.Decrypt(account.APIKeyCiphertext)
			if err != nil {
				return nil, fmt.Errorf("decrypt provider credential: %w", err)
			}
		}

		timeout := deployment.TimeoutMS
		if timeout <= 0 {
			timeout = DefaultDeploymentTimeoutMS
		}
		resolved.Deployments = append(resolved.Deployments, llm.ResolvedDeployment{
			ID:                         deployment.ID,
			ProviderID:                 account.ID,
			ProviderKind:               account.Kind,
			BaseURL:                    account.BaseURL,
			APIKey:                     apiKey,
			Model:                      deployment.ModelName,
			TimeoutMS:                  timeout,
			MaxRetries:                 deployment.MaxRetries,
			InputPricePerMillion:       deployment.InputPricePerMillion,
			OutputPricePerMillion:      deployment.OutputPricePerMillion,
			CachedInputPricePerMillion: deployment.CachedInputPricePerMillion,
		})
	}

	if len(resolved.Deployments) == 0 {
		return nil, llm.ErrModelUnavailable
	}
	return resolved, nil
}
