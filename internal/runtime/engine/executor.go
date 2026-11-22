package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yz1qaq/asterflow/internal/domain"
	"github.com/yz1qaq/asterflow/internal/runtime/llm"
)

type PromptRenderer interface {
	RenderVersionByID(ctx context.Context, workspaceID, versionID string, vars map[string]any) (string, error)
}

type Executor struct {
	prompts PromptRenderer
	models  llm.Gateway
}

func NewExecutor(prompts PromptRenderer, models llm.Gateway) *Executor {
	return &Executor{prompts: prompts, models: models}
}

type RunInput struct {
	WorkspaceID       string         `json:"-"`
	ConversationID    string         `json:"-"`
	Classification    string         `json:"-"`
	RuntimeConfigJSON string         `json:"-"`
	UserInput         string         `json:"-"`
	Vars              map[string]any `json:"-"`
}

type RunResult struct {
	Content string
	Model   string
	Usage   llm.Usage
	NextSeq int64 // last emitted seq, so the caller can continue the stream
}

// Run executes one turn. It emits started/prompt_resolved/delta (and failed or
// cancelled). run_completed is emitted by the caller after the turn is committed,
// so clients never see "completed" for a message that failed to persist.
func (e *Executor) Run(ctx context.Context, in RunInput, emit Emitter) (*RunResult, error) {
	var runtime domain.ConversationRuntimeConfig
	if err := json.Unmarshal([]byte(in.RuntimeConfigJSON), &runtime); err != nil {
		return nil, fmt.Errorf("decode conversation runtime config: %w", err)
	}

	system := runtime.SystemPrompt
	systemVersion := runtime.SystemPromptVersionID
	userVersion := runtime.UserPromptVersionID
	modelVersion := runtime.ModelProfileVersionID

	seq := int64(0)
	send := func(kind EventType, data any) error {
		seq++
		return emit.Emit(ctx, Event{Seq: seq, Type: kind, Data: EncodeData(data)})
	}
	if err := send(EventRunStarted, RunStarted{ConversationID: in.ConversationID}); err != nil {
		return nil, err
	}

	if systemVersion != "" {
		if e.prompts == nil {
			_ = send(EventRunFailed, RunFailed{Code: "prompt_error", Message: "prompt renderer is not configured"})
			return nil, fmt.Errorf("prompt renderer is not configured")
		}
		rendered, err := e.prompts.RenderVersionByID(ctx, in.WorkspaceID, systemVersion, nil)
		if err != nil {
			_ = send(EventRunFailed, RunFailed{Code: "prompt_error", Message: err.Error()})
			return nil, fmt.Errorf("render system prompt: %w", err)
		}
		system = rendered
	}

	user := in.UserInput
	if userVersion != "" {
		if e.prompts == nil {
			_ = send(EventRunFailed, RunFailed{Code: "prompt_error", Message: "prompt renderer is not configured"})
			return nil, fmt.Errorf("prompt renderer is not configured")
		}
		rendered, err := e.prompts.RenderVersionByID(ctx, in.WorkspaceID, userVersion, in.Vars)
		if err != nil {
			_ = send(EventRunFailed, RunFailed{Code: "prompt_error", Message: err.Error()})
			return nil, fmt.Errorf("render user prompt: %w", err)
		}
		user = rendered
	}

	if err := send(EventPromptResolved, PromptResolved{
		SystemPrompt: system, UserPrompt: user,
		SystemVersionID: systemVersion, UserVersionID: userVersion,
	}); err != nil {
		return nil, err
	}

	stream, err := e.models.Stream(ctx, in.WorkspaceID, in.Classification, llm.GenerateRequest{
		ModelProfileVersionID: modelVersion,
		SystemPrompt:          system,
		Messages:              []llm.Message{{Role: "user", Content: user}},
		Temperature:           runtime.GenerationConfig.Temperature,
		TopP:                  runtime.GenerationConfig.TopP,
		MaxOutputTokens:       runtime.GenerationConfig.MaxOutputTokens,
		Stop:                  runtime.GenerationConfig.Stop,
		Seed:                  runtime.GenerationConfig.Seed,
	})
	if err != nil {
		_ = send(EventRunFailed, RunFailed{Code: "model_unavailable", Message: err.Error()})
		return nil, err
	}
	defer func() { _ = stream.Close() }()

	var content strings.Builder
	for {
		chunk, err := stream.Recv(ctx)
		if err != nil {
			if ctx.Err() != nil {
				_ = send(EventRunCancelled, RunCancelled{Reason: ctx.Err().Error()})
				return nil, ctx.Err()
			}
			_ = send(EventRunFailed, RunFailed{Code: "model_stream_error", Message: err.Error()})
			return nil, err
		}
		if chunk.Content != "" {
			content.WriteString(chunk.Content)
			if err := send(EventModelDelta, ModelDelta{Content: chunk.Content}); err != nil {
				return nil, err
			}
		}
		if chunk.Done {
			return &RunResult{Content: content.String(), Model: chunk.Model, Usage: chunk.Usage, NextSeq: seq}, nil
		}
	}
}
