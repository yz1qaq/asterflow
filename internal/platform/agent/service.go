package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/yz1qaq/asterflow/internal/domain"
)

var ErrInvalidArgument = errors.New("invalid agent argument")

// PromptVersionChecker verifies a pinned prompt version is visible in the workspace.
type PromptVersionChecker interface {
	EnsureVersion(ctx context.Context, workspaceID, versionID string) error
}

// ModelProfileVersionChecker verifies a pinned model profile version is visible in the workspace.
type ModelProfileVersionChecker interface {
	ValidateProfileVersion(ctx context.Context, workspaceID, versionID string) error
}

type Store interface {
	CreateAgentWithInitialVersion(context.Context, *domain.Agent, *domain.AgentVersion) error
	AppendAgentVersion(context.Context, string, *domain.AgentVersion) error
	GetAgent(context.Context, string, string) (*domain.Agent, error)
	ListAgents(context.Context, string) ([]*domain.Agent, error)
	GetAgentVersion(context.Context, string, string) (*domain.AgentVersion, error)
	ListAgentVersions(context.Context, string, string) ([]*domain.AgentVersion, error)
	GetAgentEnv(context.Context, string, string, string) (*EnvBinding, error)
	ListAgentEnvs(context.Context, string, string) ([]*EnvBinding, error)
	SetAgentEnv(context.Context, string, string, string, string) error
}

type Service struct {
	store   Store
	prompts PromptVersionChecker
	models  ModelProfileVersionChecker
}

func NewService(store Store, prompts PromptVersionChecker, models ModelProfileVersionChecker) *Service {
	return &Service{store: store, prompts: prompts, models: models}
}

// storedSnapshot is the immutable payload persisted in agent_versions.snapshot_json.
type storedSnapshot struct {
	ID                    string                  `json:"id"`
	SystemPrompt          string                  `json:"system_prompt,omitempty"`
	SystemPromptVersionID string                  `json:"system_prompt_version_id,omitempty"`
	UserPromptVersionID   string                  `json:"user_prompt_version_id,omitempty"`
	PromptEnv             string                  `json:"prompt_env,omitempty"`
	ModelProfileVersionID string                  `json:"model_profile_version_id,omitempty"`
	GenerationConfig      domain.GenerationConfig `json:"generation_config"`
}

type VersionInput struct {
	SystemPrompt          string                  `json:"system_prompt"`
	SystemPromptVersionID string                  `json:"system_prompt_version_id"`
	UserPromptVersionID   string                  `json:"user_prompt_version_id"`
	PromptEnv             string                  `json:"prompt_env"`
	ModelProfileVersionID string                  `json:"model_profile_version_id"`
	GenerationConfig      domain.GenerationConfig `json:"generation_config"`
}

type CreateAgentRequest struct {
	WorkspaceID string `json:"-"`
	CreatedBy   string `json:"-"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Template    string `json:"template"`
	VersionInput
}

func (s *Service) buildSnapshot(ctx context.Context, workspaceID, agentID string, in VersionInput) (string, error) {
	system := strings.TrimSpace(in.SystemPrompt)
	systemVersion := strings.TrimSpace(in.SystemPromptVersionID)
	userVersion := strings.TrimSpace(in.UserPromptVersionID)
	modelVersion := strings.TrimSpace(in.ModelProfileVersionID)
	env := strings.TrimSpace(in.PromptEnv)
	if env == "" {
		env = "dev"
	}
	if system != "" && systemVersion != "" {
		return "", fmt.Errorf("%w: provide either system_prompt or system_prompt_version_id", ErrInvalidArgument)
	}
	if systemVersion != "" {
		if s.prompts == nil {
			return "", fmt.Errorf("prompt resolver is not configured")
		}
		if err := s.prompts.EnsureVersion(ctx, workspaceID, systemVersion); err != nil {
			return "", err
		}
	}
	if userVersion != "" {
		if s.prompts == nil {
			return "", fmt.Errorf("prompt resolver is not configured")
		}
		if err := s.prompts.EnsureVersion(ctx, workspaceID, userVersion); err != nil {
			return "", err
		}
	}
	if modelVersion != "" {
		if s.models == nil {
			return "", fmt.Errorf("model resolver is not configured")
		}
		if err := s.models.ValidateProfileVersion(ctx, workspaceID, modelVersion); err != nil {
			return "", err
		}
	}

	snapshot, err := json.Marshal(storedSnapshot{
		ID:                    agentID,
		SystemPrompt:          system,
		SystemPromptVersionID: systemVersion,
		UserPromptVersionID:   userVersion,
		PromptEnv:             env,
		ModelProfileVersionID: modelVersion,
		GenerationConfig:      in.GenerationConfig,
	})
	if err != nil {
		return "", fmt.Errorf("marshal agent snapshot: %w", err)
	}
	return string(snapshot), nil
}

func (s *Service) CreateAgent(ctx context.Context, req CreateAgentRequest) (*domain.Agent, *domain.AgentVersion, error) {
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	name := strings.TrimSpace(req.Name)
	if workspaceID == "" || name == "" {
		return nil, nil, fmt.Errorf("%w: workspace_id and name are required", ErrInvalidArgument)
	}
	agentID := uuid.NewString()
	snapshot, err := s.buildSnapshot(ctx, workspaceID, agentID, req.VersionInput)
	if err != nil {
		return nil, nil, err
	}
	created := &domain.Agent{
		ID:          agentID,
		WorkspaceID: workspaceID,
		Name:        name,
		Description: strings.TrimSpace(req.Description),
		Template:    strings.TrimSpace(req.Template),
		CreatedBy:   req.CreatedBy,
	}
	version := &domain.AgentVersion{
		ID:           uuid.NewString(),
		AgentID:      agentID,
		SnapshotJSON: snapshot,
		CreatedBy:    req.CreatedBy,
	}
	if err := s.store.CreateAgentWithInitialVersion(ctx, created, version); err != nil {
		return nil, nil, err
	}
	return created, version, nil
}

func (s *Service) AppendVersion(
	ctx context.Context, workspaceID, agentID string, in VersionInput, createdBy string,
) (*domain.AgentVersion, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	id, err := uuid.Parse(strings.TrimSpace(agentID))
	if err != nil {
		return nil, fmt.Errorf("%w: agent_id must be a UUID", ErrInvalidArgument)
	}
	if _, err := s.store.GetAgent(ctx, workspaceID, id.String()); err != nil {
		return nil, err
	}
	snapshot, err := s.buildSnapshot(ctx, workspaceID, id.String(), in)
	if err != nil {
		return nil, err
	}
	version := &domain.AgentVersion{
		ID:           uuid.NewString(),
		AgentID:      id.String(),
		SnapshotJSON: snapshot,
		CreatedBy:    createdBy,
	}
	if err := s.store.AppendAgentVersion(ctx, workspaceID, version); err != nil {
		return nil, err
	}
	return version, nil
}

func (s *Service) GetAgent(ctx context.Context, workspaceID, agentID string) (*domain.Agent, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	id, err := uuid.Parse(strings.TrimSpace(agentID))
	if err != nil {
		return nil, fmt.Errorf("%w: agent_id must be a UUID", ErrInvalidArgument)
	}
	return s.store.GetAgent(ctx, workspaceID, id.String())
}

func (s *Service) ListAgents(ctx context.Context, workspaceID string) ([]*domain.Agent, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf("%w: workspace_id is required", ErrInvalidArgument)
	}
	return s.store.ListAgents(ctx, workspaceID)
}

func (s *Service) ListVersions(ctx context.Context, workspaceID, agentID string) ([]*domain.AgentVersion, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	id, err := uuid.Parse(strings.TrimSpace(agentID))
	if err != nil {
		return nil, fmt.Errorf("%w: agent_id must be a UUID", ErrInvalidArgument)
	}
	if _, err := s.store.GetAgent(ctx, workspaceID, id.String()); err != nil {
		return nil, err
	}
	return s.store.ListAgentVersions(ctx, workspaceID, id.String())
}

func (s *Service) ListEnvBindings(ctx context.Context, workspaceID, agentID string) ([]*EnvBinding, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	id, err := uuid.Parse(strings.TrimSpace(agentID))
	if err != nil {
		return nil, fmt.Errorf("%w: agent_id must be a UUID", ErrInvalidArgument)
	}
	if _, err := s.store.GetAgent(ctx, workspaceID, id.String()); err != nil {
		return nil, err
	}
	return s.store.ListAgentEnvs(ctx, workspaceID, id.String())
}

func (s *Service) Promote(ctx context.Context, workspaceID, agentID, env, versionID string) error {
	workspaceID, env = strings.TrimSpace(workspaceID), strings.TrimSpace(env)
	if workspaceID == "" || env == "" {
		return fmt.Errorf("%w: workspace_id and env are required", ErrInvalidArgument)
	}
	id, err := uuid.Parse(strings.TrimSpace(agentID))
	if err != nil {
		return fmt.Errorf("%w: agent_id must be a UUID", ErrInvalidArgument)
	}
	vid, err := uuid.Parse(strings.TrimSpace(versionID))
	if err != nil {
		return fmt.Errorf("%w: version_id must be a UUID", ErrInvalidArgument)
	}
	return s.store.SetAgentEnv(ctx, workspaceID, id.String(), env, vid.String())
}

func (s *Service) GetEnvBinding(ctx context.Context, workspaceID, agentID, env string) (*EnvBinding, error) {
	workspaceID, env = strings.TrimSpace(workspaceID), strings.TrimSpace(env)
	if workspaceID == "" || env == "" {
		return nil, fmt.Errorf("%w: workspace_id and env are required", ErrInvalidArgument)
	}
	id, err := uuid.Parse(strings.TrimSpace(agentID))
	if err != nil {
		return nil, fmt.Errorf("%w: agent_id must be a UUID", ErrInvalidArgument)
	}
	return s.store.GetAgentEnv(ctx, workspaceID, id.String(), env)
}

// EnsureVersion verifies a version exists in the workspace and belongs to the agent.
func (s *Service) EnsureVersion(ctx context.Context, workspaceID, agentID, versionID string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return fmt.Errorf("%w: workspace_id is required", ErrInvalidArgument)
	}
	id, err := uuid.Parse(strings.TrimSpace(agentID))
	if err != nil {
		return fmt.Errorf("%w: agent_id must be a UUID", ErrInvalidArgument)
	}
	version, err := s.store.GetAgentVersion(ctx, workspaceID, strings.TrimSpace(versionID))
	if err != nil {
		return err
	}
	if version.AgentID != id.String() {
		return ErrAgentVersionNotFound
	}
	return nil
}
