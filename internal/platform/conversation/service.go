package conversation

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/yz1qaq/asterflow/internal/domain"
)

var ErrInvalidArgument = errors.New("invalid conversation argument")

type AgentVersionChecker interface {
	EnsureVersion(ctx context.Context, workspaceID, agentID, versionID string) error
}

type Store interface {
	CreateConversation(context.Context, *domain.Conversation) error
	GetConversation(context.Context, string, string) (*domain.Conversation, error)
	ListConversations(context.Context, string, string, int32, int32) ([]*domain.Conversation, error)
	ListMessages(context.Context, string, string) ([]*domain.Message, error)
}

type Service struct {
	store  Store
	agents AgentVersionChecker
}

func NewService(store Store, agents AgentVersionChecker) *Service {
	return &Service{store: store, agents: agents}
}

type CreateConversationRequest struct {
	WorkspaceID       string `json:"-"`
	UserID            string `json:"-"`
	AgentID           string `json:"agent_id"`
	AgentVersionID    string `json:"agent_version_id"`
	Classification    string `json:"classification"`
	RuntimeConfigJSON string `json:"runtime_config_json"`
}

func (s *Service) Create(ctx context.Context, req CreateConversationRequest) (*domain.Conversation, error) {
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	agentID := strings.TrimSpace(req.AgentID)
	versionID := strings.TrimSpace(req.AgentVersionID)
	if workspaceID == "" || req.UserID == "" || agentID == "" || versionID == "" {
		return nil, fmt.Errorf("%w: workspace, user, agent and agent_version_id are required", ErrInvalidArgument)
	}
	if s.agents == nil {
		return nil, fmt.Errorf("agent resolver is not configured")
	}
	if err := s.agents.EnsureVersion(ctx, workspaceID, agentID, versionID); err != nil {
		return nil, err
	}
	config := strings.TrimSpace(req.RuntimeConfigJSON)
	if config == "" {
		config = "{}"
	}
	conv := &domain.Conversation{
		ID: uuid.NewString(), WorkspaceID: workspaceID,
		AgentID: canonical(agentID), AgentVersionID: canonical(versionID),
		UserID: req.UserID, Status: "active",
		Classification:    strings.TrimSpace(req.Classification),
		RuntimeConfigJSON: config,
	}
	if err := s.store.CreateConversation(ctx, conv); err != nil {
		return nil, err
	}
	return conv, nil
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
