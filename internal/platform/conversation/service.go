package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/yz1qaq/asterflow/internal/domain"
)

var ErrInvalidArgument = errors.New("invalid conversation argument")

type AgentVersionChecker interface {
	GetOwnedVersion(ctx context.Context, workspaceID, agentID, versionID string) (*domain.AgentVersion, error)
}

// PromptResolver pins prompt-id references to concrete versions at conversation
// creation time. It applies the environment pointer and active experiment.
type PromptResolver interface {
	ResolveVersion(ctx context.Context, workspaceID, promptID, env, userID string) (string, error)
}

type Store interface {
	CreateConversation(context.Context, *domain.Conversation) error
	GetConversation(context.Context, string, string) (*domain.Conversation, error)
	ListConversations(context.Context, string, string, int32, int32) ([]*domain.Conversation, error)
	ListMessages(context.Context, string, string) ([]*domain.Message, error)

	ClaimTurn(ctx context.Context, workspaceID, conversationID string, leaseSeconds int32) (string, int64, error)
	CommitTurn(ctx context.Context, workspaceID, conversationID, token string, messages []*domain.Message, status string) error
	ReleaseTurn(ctx context.Context, workspaceID, conversationID, token, status string) error
}

type Service struct {
	store           Store
	agentsChecker   AgentVersionChecker
	promptsResolver PromptResolver
	engine          Runner
}

func NewService(store Store, agents AgentVersionChecker, prompts PromptResolver, engine Runner) *Service {
	return &Service{store: store, agentsChecker: agents, promptsResolver: prompts, engine: engine}
}

type CreateConversationRequest struct {
	WorkspaceID    string `json:"-"`
	UserID         string `json:"-"`
	AgentID        string `json:"agent_id"`
	AgentVersionID string `json:"agent_version_id"`
	Classification string `json:"classification"`
}

func (s *Service) Create(ctx context.Context, req CreateConversationRequest) (*domain.Conversation, error) {
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	agentID := strings.TrimSpace(req.AgentID)
	versionID := strings.TrimSpace(req.AgentVersionID)
	if workspaceID == "" || req.UserID == "" || agentID == "" || versionID == "" {
		return nil, fmt.Errorf("%w: workspace, user, agent and agent_version_id are required", ErrInvalidArgument)
	}
	if s.agentsChecker == nil {
		return nil, fmt.Errorf("agent resolver is not configured")
	}
	agentVersion, err := s.agentsChecker.GetOwnedVersion(ctx, workspaceID, agentID, versionID)
	if err != nil {
		return nil, err
	}

	var snapshot domain.AgentSnapshot
	if err := json.Unmarshal([]byte(agentVersion.SnapshotJSON), &snapshot); err != nil {
		return nil, fmt.Errorf("decode agent snapshot: %w", err)
	}
	runtime, err := s.resolveRuntimeConfig(ctx, workspaceID, snapshot, req.UserID)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(runtime)
	if err != nil {
		return nil, fmt.Errorf("encode runtime config: %w", err)
	}

	conv := &domain.Conversation{
		ID: uuid.NewString(), WorkspaceID: workspaceID,
		AgentID: canonical(agentID), AgentVersionID: canonical(versionID),
		UserID: req.UserID, Status: "active",
		Classification:    strings.TrimSpace(req.Classification),
		RuntimeConfigJSON: string(encoded),
	}
	if err := s.store.CreateConversation(ctx, conv); err != nil {
		return nil, err
	}
	return conv, nil
}

func (s *Service) resolveRuntimeConfig(
	ctx context.Context, workspaceID string, snapshot domain.AgentSnapshot, userID string,
) (*domain.ConversationRuntimeConfig, error) {
	runtime := &domain.ConversationRuntimeConfig{
		Environment:           snapshot.PromptEnv,
		SystemPrompt:          snapshot.SystemPrompt,
		ModelProfileVersionID: snapshot.ModelProfileVersionID,
		GenerationConfig:      snapshot.GenerationConfig,
	}

	if snapshot.SystemPromptVersionID != "" {
		runtime.SystemPromptVersionID = snapshot.SystemPromptVersionID
	} else if snapshot.SystemPromptID != "" {
		if s.promptsResolver == nil {
			return nil, fmt.Errorf("prompt resolver is not configured")
		}
		versionID, err := s.promptsResolver.ResolveVersion(ctx, workspaceID, snapshot.SystemPromptID, snapshot.PromptEnv, userID)
		if err != nil {
			return nil, err
		}
		runtime.SystemPromptVersionID = versionID
	}

	if snapshot.UserPromptVersionID != "" {
		runtime.UserPromptVersionID = snapshot.UserPromptVersionID
	} else if snapshot.UserPromptID != "" {
		if s.promptsResolver == nil {
			return nil, fmt.Errorf("prompt resolver is not configured")
		}
		versionID, err := s.promptsResolver.ResolveVersion(ctx, workspaceID, snapshot.UserPromptID, snapshot.PromptEnv, userID)
		if err != nil {
			return nil, err
		}
		runtime.UserPromptVersionID = versionID
	}
	return runtime, nil
}

func canonical(value string) string {
	parsed, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return strings.TrimSpace(value)
	}
	return parsed.String()
}

func (s *Service) Get(ctx context.Context, workspaceID, conversationID, userID string, allowAll bool) (*domain.Conversation, error) {
	if _, err := uuid.Parse(strings.TrimSpace(conversationID)); err != nil {
		return nil, fmt.Errorf("%w: conversation_id must be a UUID", ErrInvalidArgument)
	}
	conversation, err := s.store.GetConversation(ctx, strings.TrimSpace(workspaceID), strings.TrimSpace(conversationID))
	if err != nil {
		return nil, err
	}
	if !allowAll && conversation.UserID != userID {
		return nil, ErrConversationNotFound
	}
	return conversation, nil
}

func (s *Service) List(ctx context.Context, workspaceID, userID string, allowAll bool, limit, offset int32) ([]*domain.Conversation, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf("%w: workspace_id is required", ErrInvalidArgument)
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	owner := userID
	if allowAll {
		owner = ""
	}
	return s.store.ListConversations(ctx, workspaceID, owner, limit, offset)
}

func (s *Service) Messages(ctx context.Context, workspaceID, conversationID, userID string, allowAll bool) ([]*domain.Message, error) {
	if _, err := s.Get(ctx, workspaceID, conversationID, userID, allowAll); err != nil {
		return nil, err
	}
	return s.store.ListMessages(ctx, strings.TrimSpace(workspaceID), strings.TrimSpace(conversationID))
}
