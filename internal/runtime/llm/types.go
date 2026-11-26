package llm

import (
	"context"
	"errors"

	"github.com/cloudwego/eino/schema"
)

var (
	ErrModelUnavailable     = errors.New("no usable model deployment")
	ErrClassificationDenied = errors.New("model deployment does not allow this classification")
	ErrQuotaExceeded        = errors.New("model usage quota exceeded")
)

// ToolCall 使用 Eino 的结构，保留流式工具调用的 index。
type ToolCall = schema.ToolCall

type Message struct {
	Role             string     `json:"role"`
	Content          string     `json:"content"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
}

// GenerateRequest is the business-level model call. It never carries API keys;
// the gateway resolves deployments from the model profile version.
type GenerateRequest struct {
	ModelProfileVersionID string             `json:"model_profile_version_id"`
	SystemPrompt          string             `json:"system_prompt"`
	Messages              []Message          `json:"messages"`
	Temperature           *float32           `json:"temperature,omitempty"`
	TopP                  *float32           `json:"top_p,omitempty"`
	MaxOutputTokens       *int               `json:"max_output_tokens,omitempty"`
	Stop                  []string           `json:"stop,omitempty"`
	Seed                  *int               `json:"seed,omitempty"`
	Tools                 []*schema.ToolInfo `json:"-"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type GenerateResponse struct {
	Content          string     `json:"content"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	Usage            Usage      `json:"usage"`
	Model            string     `json:"model"`
	DeploymentID     string     `json:"deployment_id"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
}

type StreamChunk struct {
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Done      bool       `json:"done"`
	// Filled by adapters on the final chunk (Done=true); optional on deltas.
	Model            string `json:"model,omitempty"`
	DeploymentID     string `json:"deployment_id,omitempty"`
	Usage            Usage  `json:"usage,omitempty"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

// Stream is consumed until Done or error; Close releases provider resources.
type Stream interface {
	Recv(ctx context.Context) (*StreamChunk, error)
	Close() error
}

// Gateway resolves a model profile version to deployments and applies
// classification/quota/failover policy. Implementations must respect ctx cancellation.
type Gateway interface {
	Generate(ctx context.Context, workspaceID, classification string, req GenerateRequest) (*GenerateResponse, error)
	Stream(ctx context.Context, workspaceID, classification string, req GenerateRequest) (Stream, error)
}
