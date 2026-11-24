package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yz1qaq/asterflow/internal/domain"
	"github.com/yz1qaq/asterflow/internal/runtime/engine"
)

type Runner interface {
	Run(ctx context.Context, in engine.RunInput, emit engine.Emitter) (*engine.RunResult, error)
}

type ChatTurnRequest struct {
	WorkspaceID         string         `json:"-"`
	UserID              string         `json:"-"`
	ConversationID      string         `json:"-"`
	Input               string         `json:"input"`
	Vars                map[string]any `json:"vars"`
	UserPromptVersionID string         `json:"user_prompt_version_id"`
}

func (s *Service) RunTurn(
	ctx context.Context,
	req ChatTurnRequest,
	emit engine.Emitter,
) (_ *domain.Message, err error) {
	events := &turnEmitter{next: emit}
	defer func() {
		if err != nil {
			events.fail(ctx, err)
		}
	}()

	if s.engine == nil {
		return nil, fmt.Errorf("agent executor is not configured")
	}

	input := strings.TrimSpace(req.Input)
	if input == "" && req.Vars == nil {
		return nil, fmt.Errorf("%w: input is required", ErrInvalidArgument)
	}

	requestedVersion := canonical(req.UserPromptVersionID)
	if requestedVersion != "" && req.Vars == nil {
		return nil, fmt.Errorf(
			"%w: vars is required when selecting a user prompt version",
			ErrInvalidArgument,
		)
	}

	conv, err := s.Get(ctx, req.WorkspaceID, req.ConversationID, req.UserID, false)
	if err != nil {
		return nil, err
	}

	token, revision, err := s.store.ClaimTurn(ctx, conv.WorkspaceID, conv.ID, s.turnLeaseSeconds)
	if err != nil {
		return nil, err
	}

	workspaceID, conversationID := conv.WorkspaceID, conv.ID
	committed := false
	defer func() {
		if committed {
			return
		}
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = s.store.ReleaseTurn(releaseCtx, workspaceID, conversationID, token, "active")
	}()

	turnCtx, cancelTurn := context.WithCancelCause(ctx)
	defer cancelTurn(nil)
	stopRenewal := s.renewTurn(turnCtx, cancelTurn, conv.WorkspaceID, conv.ID, token)
	defer stopRenewal()

	// Reload the runtime config after claiming the turn, so a previous commit is visible.
	conv, err = s.store.GetConversation(turnCtx, conv.WorkspaceID, conv.ID)
	if err != nil {
		return nil, err
	}
	history, err := s.store.ListMessages(turnCtx, conv.WorkspaceID, conv.ID)
	if err != nil {
		return nil, fmt.Errorf("load conversation history: %w", err)
	}
	if req.Vars != nil && len(history) != 0 {
		return nil, fmt.Errorf(
			"%w: user prompt variables are only allowed on the first turn",
			ErrInvalidArgument,
		)
	}

	var userPromptID string
	if requestedVersion != "" {
		userPromptID, err = s.boundUserPromptID(turnCtx, conv)
		if err != nil {
			return nil, err
		}
	}

	result, err := s.engine.Run(turnCtx, engine.RunInput{
		AgentVersionID:      conv.AgentVersionID,
		TurnRevision:        revision,
		WorkspaceID:         workspaceID,
		ConversationID:      conv.ID,
		Classification:      conv.Classification,
		RuntimeConfigJSON:   conv.RuntimeConfigJSON,
		UserInput:           input,
		Vars:                req.Vars,
		History:             history,
		UserPromptID:        userPromptID,
		UserPromptVersionID: requestedVersion,
	}, events)
	if cause := context.Cause(turnCtx); cause != nil {
		return nil, cause
	}
	if err != nil {
		return nil, err
	}

	var userPromptSnapshot []byte
	if result.RenderedUserPrompt != "" {
		input = result.RenderedUserPrompt
		userPromptSnapshot, err = json.Marshal(domain.UserPromptSnapshot{
			VersionID:         result.UserPromptVersionID,
			ExperimentID:      result.UserPromptExperiment,
			ExperimentVariant: result.UserPromptVariant,
			Variables:         req.Vars,
			Rendered:          result.RenderedUserPrompt,
		})
		if err != nil {
			return nil, fmt.Errorf("encode user prompt snapshot: %w", err)
		}
	}

	userMessage := &domain.Message{
		ID:             uuid.NewString(),
		ConversationID: conv.ID,
		Role:           "user",
		Content:        input,
	}
	assistantMessage := &domain.Message{
		ID:             uuid.NewString(),
		ConversationID: conv.ID,
		Role:           "assistant",
		Content:        result.Content,
	}

	if cause := context.Cause(turnCtx); cause != nil {
		return nil, cause
	}
	// The transaction still checks the token and expiry. A renewal racing with
	// a successful commit must not cancel the transaction when it clears the token.
	commitCtx, cancelCommit := context.WithTimeout(ctx, turnCommitTimeout)
	err = s.store.CommitTurn(
		commitCtx, workspaceID, conv.ID, token,
		[]*domain.Message{userMessage, assistantMessage},
		"active", userPromptSnapshot,
	)
	cancelCommit()
	stopRenewal()
	if err != nil {
		return nil, err
	}
	committed = true

	if err := events.Emit(ctx, engine.Event{
		Seq:  events.lastSeq + 1,
		Type: engine.EventRunCompleted,
		Data: engine.EncodeData(engine.RunCompleted{
			AssistantMessageID: assistantMessage.ID,
			Usage:              result.Usage,
			Model:              result.Model,
		}),
	}); err != nil {
		return nil, err
	}

	return assistantMessage, nil
}

// 会话固定的agent版本读取模版ID
func (s *Service) boundUserPromptID(
	ctx context.Context, conv *domain.Conversation,
) (string, error) {
	if s.agentsChecker == nil {
		return "", fmt.Errorf("agent resolver is not configured")
	}

	version, err := s.agentsChecker.GetOwnedVersion(
		ctx, conv.WorkspaceID, conv.AgentID, conv.AgentVersionID,
	)
	if err != nil {
		return "", err
	}

	var snapshot domain.AgentSnapshot
	if err := json.Unmarshal([]byte(version.SnapshotJSON), &snapshot); err != nil {
		return "", fmt.Errorf("decode agent snapshot: %w", err)
	}
	if snapshot.UserPromptID == "" {
		return "", fmt.Errorf(
			"%w: agent version has no user prompt template for version selection",
			ErrInvalidArgument,
		)
	}
	return snapshot.UserPromptID, nil
}
