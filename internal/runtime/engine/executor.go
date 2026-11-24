package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/yz1qaq/asterflow/internal/domain"
	"github.com/yz1qaq/asterflow/internal/runtime/llm"
)

type PromptRenderer interface {
	RenderVersionByID(ctx context.Context, workspaceID, versionID string, vars map[string]any) (string, error)
	RenderByVersion(ctx context.Context, workspaceID, promptID, versionID string, vars map[string]any) (string, error)
}

type Executor struct {
	promptRenderer PromptRenderer
	models         llm.Gateway
}

const maxHistoryMessages = 10

var ErrInvalidUserPrompt = errors.New("invalid user prompt")

func NewExecutor(prompts PromptRenderer, models llm.Gateway) *Executor {
	return &Executor{promptRenderer: prompts, models: models}
}

type RunInput struct {
	AgentVersionID      string            `json:"-"`
	TurnRevision        int64             `json:"-"`
	WorkspaceID         string            `json:"-"`
	ConversationID      string            `json:"-"`
	Classification      string            `json:"-"`
	RuntimeConfigJSON   string            `json:"-"`
	UserInput           string            `json:"-"`
	Vars                map[string]any    `json:"-"`
	History             []*domain.Message `json:"-"`
	UserPromptID        string            `json:"-"`
	UserPromptVersionID string            `json:"-"`
}

type RunResult struct {
	UserPromptExperiment string
	UserPromptVariant    string
	RenderedUserPrompt   string
	UserPromptVersionID  string
	Content              string
	Model                string
	Usage                llm.Usage
	NextSeq              int64
}

func (e *Executor) Run(ctx context.Context, in RunInput, emit Emitter) (*RunResult, error) {
	var runtime domain.ConversationRuntimeConfig
	if err := json.Unmarshal([]byte(in.RuntimeConfigJSON), &runtime); err != nil {
		return nil, fmt.Errorf("decode conversation runtime config: %w", err)
	}

	system := runtime.SystemPrompt
	systemVersion := runtime.SystemPromptVersionID
	modelVersion := runtime.ModelProfileVersionID

	user := in.UserInput
	userVersion := ""
	if in.Vars != nil {
		//以runtime的用户提示词模版version兜底
		userVersion = runtime.UserPromptVersionID
		//如果请求用户提示词模版存在,就用请求的
		if in.UserPromptVersionID != "" {
			userVersion = in.UserPromptVersionID
		}
		if userVersion == "" {
			return nil, fmt.Errorf("%w: no user prompt template is configured", ErrInvalidUserPrompt)
		}
		if e.promptRenderer == nil {
			return nil, fmt.Errorf("prompt renderer is not configured")
		}

		var rendered string
		var err error
		//渲染模版
		if in.UserPromptVersionID != "" {
			rendered, err = e.promptRenderer.RenderByVersion(
				ctx, in.WorkspaceID, in.UserPromptID, userVersion, in.Vars,
			)
		} else {
			rendered, err = e.promptRenderer.RenderVersionByID(
				ctx, in.WorkspaceID, userVersion, in.Vars,
			)
		}
		if err != nil {
			return nil, fmt.Errorf("render user prompt: %w", err)
		}
		if input := strings.TrimSpace(user); input != "" {
			rendered += "\n\n# 补充说明\n\n" + input
		}
		if strings.TrimSpace(rendered) == "" {
			return nil, fmt.Errorf("%w: rendered user prompt is empty", ErrInvalidUserPrompt)
		}
		user = rendered
	}

	seq := int64(0)
	send := func(kind EventType, data any) error {
		seq++
		return emit.Emit(ctx, Event{
			Seq:  seq,
			Type: kind,
			Data: EncodeData(data),
		})
	}
	if err := send(EventRunStarted, RunStarted{
		ConversationID: in.ConversationID, AgentVersionID: in.AgentVersionID, TurnRevision: in.TurnRevision,
	}); err != nil {
		return nil, err
	}

	if systemVersion != "" {
		if e.promptRenderer == nil {
			_ = send(EventRunFailed, RunFailed{
				Code:    "prompt_error",
				Message: "prompt renderer is not configured",
			})
			return nil, fmt.Errorf("prompt renderer is not configured")
		}

		rendered, err := e.promptRenderer.RenderVersionByID(
			ctx, in.WorkspaceID, systemVersion, nil,
		)
		if err != nil {
			_ = send(EventRunFailed, RunFailed{
				Code:    "prompt_error",
				Message: err.Error(),
			})
			return nil, fmt.Errorf("render system prompt: %w", err)
		}
		system = rendered
	}

	if err := send(EventPromptResolved, PromptResolved{
		SystemPrompt:    system,
		UserPrompt:      user,
		SystemVersionID: systemVersion,
		UserVersionID:   userVersion,
	}); err != nil {
		return nil, err
	}

	messages, err := buildModelMessages(in.History, user)
	if err != nil {
		_ = send(EventRunFailed, RunFailed{
			Code:    "history_error",
			Message: err.Error(),
		})
		return nil, err
	}

	if e.models == nil {
		err := fmt.Errorf("model gateway is not configured")
		_ = send(EventRunFailed, RunFailed{Code: "model_unavailable", Message: err.Error()})
		return nil, err
	}
	chatModel := &gatewayChatModel{
		gateway: e.models, workspaceID: in.WorkspaceID, classification: in.Classification,
		request: llm.GenerateRequest{
			ModelProfileVersionID: modelVersion,
			Temperature:           runtime.GenerationConfig.Temperature,
			TopP:                  runtime.GenerationConfig.TopP,
			MaxOutputTokens:       runtime.GenerationConfig.MaxOutputTokens,
			Stop:                  runtime.GenerationConfig.Stop,
			Seed:                  runtime.GenerationConfig.Seed,
		},
	}
	result, err := runADK(ctx, chatModel, system, messages, func(text string) error {
		return send(EventModelDelta, ModelDelta{Content: text})
	})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		code, message := "model_stream_error", "模型生成失败"
		if errors.Is(err, errModelStart) {
			code, message = "model_unavailable", "模型当前不可用"
		}
		_ = send(EventRunFailed, RunFailed{Code: code, Message: message})
		return nil, err
	}
	result.NextSeq = seq
	if userVersion != "" {
		result.RenderedUserPrompt = user
		result.UserPromptVersionID = userVersion
		if in.UserPromptVersionID == "" {
			result.UserPromptExperiment = runtime.UserPromptExperiment
			result.UserPromptVariant = runtime.UserPromptVariant
		}
	}
	return result, nil
}

// 把历史记录构建进消息
func buildModelMessages(history []*domain.Message, user string) ([]*schema.Message, error) {
	if len(history) > maxHistoryMessages {
		history = history[len(history)-maxHistoryMessages:]
	}

	messages := make([]*schema.Message, 0, len(history)+1)
	for _, message := range history {
		if message == nil {
			return nil, fmt.Errorf("conversation history contains a nil message")
		}
		if message.Role == "tool" || len(message.ToolCalls) > 0 || message.ToolCallID != nil {
			return nil, fmt.Errorf("tool history requires the tool runtime")
		}

		switch message.Role {
		case "user", "assistant":
			messages = append(messages, &schema.Message{
				Role:    schema.RoleType(message.Role),
				Content: message.Content,
			})
		case "system":
			// The system prompt comes from the pinned runtime config.
		default:
			return nil, fmt.Errorf("unsupported history role %q", message.Role)
		}
	}

	return append(messages, schema.UserMessage(user)), nil
}
