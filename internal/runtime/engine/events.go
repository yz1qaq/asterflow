package engine

import (
	"context"
	"encoding/json"

	"github.com/yz1qaq/asterflow/internal/runtime/llm"
)

type EventType string

const (
	EventRunStarted     EventType = "run_started"
	EventPromptResolved EventType = "prompt_resolved"
	EventModelDelta     EventType = "model_delta"
	EventToolCall       EventType = "tool_call"
	EventToolResult     EventType = "tool_result"

	EventRunCompleted EventType = "run_completed"
	EventRunFailed    EventType = "run_failed"
	EventRunCancelled EventType = "run_cancelled"
)

// Event is one ordered execution event. Seq is assigned by the emitter and
// strictly increases within a run; SSE clients rely on it for ordering.
type Event struct {
	Seq  int64           `json:"seq"`
	Type EventType       `json:"type"` //前端只对认识的种类渲染
	Data json.RawMessage `json:"data,omitempty"`
}

type Emitter interface {
	Emit(ctx context.Context, event Event) error
}

type RunStarted struct {
	ConversationID string `json:"conversation_id"`
	AgentVersionID string `json:"agent_version_id"`
	TurnRevision   int64  `json:"turn_revision"`
}

type PromptResolved struct {
	SystemPrompt    string `json:"system_prompt"`
	UserPrompt      string `json:"user_prompt"`
	SystemVersionID string `json:"system_prompt_version_id,omitempty"`
	UserVersionID   string `json:"user_prompt_version_id,omitempty"`
}

type ModelDelta struct {
	Content string `json:"content"`
}

type ToolCallEvent struct {
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolResultEvent struct {
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Output    string `json:"output,omitempty"`
	Error     string `json:"error,omitempty"`
	LatencyMs int64  `json:"latency_ms"`
}

type RunCompleted struct {
	AssistantMessageID string    `json:"assistant_message_id"`
	Usage              llm.Usage `json:"usage"`
	Model              string    `json:"model"`
}

type RunFailed struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type RunCancelled struct {
	Reason string `json:"reason"`
}

func EncodeData(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return encoded
}
