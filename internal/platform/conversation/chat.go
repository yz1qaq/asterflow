package conversation

import (
	"context"
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
	WorkspaceID    string         `json:"-"`
	UserID         string         `json:"-"`
	ConversationID string         `json:"-"`
	Input          string         `json:"input"`
	Vars           map[string]any `json:"vars"`
}

// RunTurn claims the conversation lease, executes one turn and commits both the
// user message and the assistant reply in one transaction. run_completed is
// emitted only after CommitTurn succeeds.
func (s *Service) RunTurn(ctx context.Context, req ChatTurnRequest, emit engine.Emitter) (*domain.Message, error) {
	if s.engine == nil {
		return nil, fmt.Errorf("agent executor is not configured")
	}
	input := strings.TrimSpace(req.Input)
	if input == "" {
		return nil, fmt.Errorf("%w: input is required", ErrInvalidArgument)
	}
	conv, err := s.Get(ctx, req.WorkspaceID, req.ConversationID, req.UserID, false)
	if err != nil {
		return nil, err
	}

	token, _, err := s.store.ClaimTurn(ctx, req.WorkspaceID, conv.ID, 60)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.store.ReleaseTurn(releaseCtx, req.WorkspaceID, conv.ID, token, "active")
	}()

	result, err := s.engine.Run(ctx, engine.RunInput{
		WorkspaceID:       req.WorkspaceID,
		ConversationID:    conv.ID,
		Classification:    conv.Classification,
		RuntimeConfigJSON: conv.RuntimeConfigJSON,
		UserInput:         input,
		Vars:              req.Vars,
	}, emit)
	if err != nil {
		return nil, err
	}

	userMessage := &domain.Message{ID: uuid.NewString(), ConversationID: conv.ID, Role: "user", Content: input}
	assistantMessage := &domain.Message{ID: uuid.NewString(), ConversationID: conv.ID, Role: "assistant", Content: result.Content}
	if err := s.store.CommitTurn(ctx, req.WorkspaceID, conv.ID, token,
		[]*domain.Message{userMessage, assistantMessage}, "active"); err != nil {
		return nil, err
	}
	committed = true

	if err := emit.Emit(ctx, engine.Event{
		Seq:  result.NextSeq + 1,
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
