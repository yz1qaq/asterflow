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
	toolBuilder    VersionedToolBuilder
}

const maxHistoryMessages = 10

var ErrInvalidUserPrompt = errors.New("invalid user prompt")

func NewExecutor(prompts PromptRenderer, models llm.Gateway) *Executor {
	return &Executor{promptRenderer: prompts, models: models}
}

type RunInput struct {
	AgentVersionID      string            `json:"-"`
	ToolVersionIDs      []string          `json:"-"`
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
	ToolMessages         []*schema.Message
	ToolResults          []ToolResultEvent
	ToolIdentities       map[string]domain.ToolIdentity
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

	// 会话快照决定本轮可供 ADK 调用的工具。
	tools, toolIdentities, err := e.buildTools(ctx, in)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		_ = send(EventRunFailed, RunFailed{
			Code: "tool_build_error", Message: "工具加载失败",
		})
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
	var toolResults []ToolResultEvent
	sendToolEvent := func(kind EventType, data any) error {
		if outcome, ok := data.(ToolResultEvent); ok {
			toolResults = append(toolResults, outcome)
		}
		return send(kind, data)
	}
	result, err := runADK(ctx, chatModel, system, messages, tools, sendToolEvent, func(text string) error {
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
	result.ToolResults = toolResults
	result.ToolIdentities = toolIdentities

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

func buildModelMessages(history []*domain.Message, user string) ([]*schema.Message, error) {
	if len(history) > maxHistoryMessages {
		start := len(history) - maxHistoryMessages
		// 回到本轮用户消息，避免裁剪出一条孤立的工具结果。
		for start > 0 && history[start] != nil && history[start].Role != "user" {
			start--
		}
		history = history[start:]
	}

	messages := make([]*schema.Message, 0, len(history)+1)
	pending := make(map[string]struct{})

	for _, message := range history {
		if message == nil {
			return nil, fmt.Errorf("conversation history contains a nil message")
		}
		if len(pending) != 0 && message.Role != "tool" {
			return nil, fmt.Errorf("history tool calls are missing results")
		}

		switch message.Role {
		case "user":
			messages = append(messages, schema.UserMessage(message.Content))

		case "assistant":
			var calls []schema.ToolCall
			for _, call := range message.ToolCalls {
				id := call.ProviderCallID
				if id == "" {
					id = call.ID
				}
				_, exists := pending[id]
				if id == "" || call.Name == "" || exists {
					return nil, fmt.Errorf("invalid history tool call %q", id)
				}

				pending[id] = struct{}{}
				calls = append(calls, schema.ToolCall{
					ID: id, Type: "function",
					Function: schema.FunctionCall{
						Name:      call.Name,
						Arguments: domain.ToolArgumentsObject(call.Args),
					},
				})
			}
			messages = append(messages, schema.AssistantMessage(message.Content, calls))

		case "tool":
			if message.ToolCallID == nil {
				return nil, fmt.Errorf("history tool result is missing its call ID")
			}
			id := *message.ToolCallID
			if _, exists := pending[id]; !exists {
				return nil, fmt.Errorf("history tool result has no matching call %q", id)
			}
			delete(pending, id)
			messages = append(messages, schema.ToolMessage(message.Content, id))

		case "system":
			// 系统提示词由本会话固定的运行时配置提供。
		default:
			return nil, fmt.Errorf("unsupported history role %q", message.Role)
		}
	}

	if len(pending) != 0 {
		return nil, fmt.Errorf("history tool calls are missing results")
	}
	return append(messages, schema.UserMessage(user)), nil
}
