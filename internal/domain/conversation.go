package domain

import (
	"encoding/json"
	"strings"
	"time"
)

type Conversation struct {
	ID                string     `json:"id"`
	WorkspaceID       string     `json:"workspace_id"`
	AgentID           string     `json:"agent_id"`
	AgentVersionID    string     `json:"agent_version_id"`
	UserID            string     `json:"user_id"`
	Status            string     `json:"status"`
	Classification    string     `json:"classification"`
	RuntimeConfigJSON string     `json:"runtime_config_json,omitempty"`
	StartedAt         time.Time  `json:"started_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	TurnToken         string     `json:"-"`
	TurnLeaseUntil    *time.Time `json:"-"`
	TurnRevision      int64      `json:"-"`
}

type Message struct {
	ID             string     `json:"id"`
	ConversationID string     `json:"conversation_id"`
	Role           string     `json:"role"`
	Content        string     `json:"content"`
	ToolCalls      []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID     *string    `json:"tool_call_id,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

type ToolCall struct {
	ID             string    `json:"id"`
	MessageID      string    `json:"message_id"`
	ToolID         string    `json:"tool_id,omitempty"`
	ToolVersionID  string    `json:"tool_version_id,omitempty"`
	Args           string    `json:"args"`
	Result         string    `json:"result,omitempty"`
	Error          string    `json:"error,omitempty"`
	LatencyMs      int       `json:"latency_ms"`
	CreatedAt      time.Time `json:"created_at"`
	ProviderCallID string    `json:"provider_call_id,omitempty"`
	Name           string    `json:"name,omitempty"`
}

// ToolArgumentsObject 为 SQL 和模型回放提供合法的 JSON 对象。
// 消息记录仍会保留模型生成的原始参数字符串。
func ToolArgumentsObject(raw string) string {
	args := strings.TrimSpace(raw)
	if strings.HasPrefix(args, "{") && json.Valid([]byte(args)) {
		return args
	}
	return "{}"
}
