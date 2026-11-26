package conversation

import (
	"fmt"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	"github.com/yz1qaq/asterflow/internal/domain"
	"github.com/yz1qaq/asterflow/internal/runtime/engine"
)

func buildTurnMessages(
	user, answer *domain.Message, history []*domain.Message, result *engine.RunResult,
) ([]*domain.Message, error) {
	messages := []*domain.Message{user}
	pending := make(map[string]*domain.ToolCall)
	results := make(map[string][]engine.ToolResultEvent)
	for _, outcome := range result.ToolResults {
		results[outcome.CallID] = append(results[outcome.CallID], outcome)
	}

	for _, source := range result.ToolMessages {
		message := &domain.Message{
			ID: uuid.NewString(), ConversationID: user.ConversationID,
			Role: string(source.Role), Content: source.Content,
		}

		switch source.Role {
		case schema.Assistant:
			if len(pending) != 0 {
				return nil, fmt.Errorf("tool calls are missing results")
			}
			message.ToolCalls = make([]domain.ToolCall, len(source.ToolCalls))
			for i, call := range source.ToolCalls {
				if call.ID == "" || call.Function.Name == "" || pending[call.ID] != nil {
					return nil, fmt.Errorf("invalid assistant tool call %q", call.ID)
				}

				// 未知工具不在本轮装配结果里，两个字段保持空值。
				identity := result.ToolIdentities[call.Function.Name]

				// SQL 主键与模型生成的调用 ID 分别保存。
				message.ToolCalls[i] = domain.ToolCall{
					ID: uuid.NewString(), MessageID: message.ID,
					ProviderCallID: call.ID, Name: call.Function.Name,
					ToolID: identity.ToolID, ToolVersionID: identity.VersionID,
					Args: call.Function.Arguments,
				}
				pending[call.ID] = &message.ToolCalls[i]
			}

		case schema.Tool:
			callID := source.ToolCallID
			call := pending[callID]
			if call == nil {
				return nil, fmt.Errorf("tool result has no matching call %q", callID)
			}

			message.ToolCallID = &callID
			call.Result = source.Content
			if outcomes := results[callID]; len(outcomes) != 0 {
				call.LatencyMs = int(outcomes[0].LatencyMs)
				call.Error = outcomes[0].Error
				results[callID] = outcomes[1:]
			}
			delete(pending, callID)

		default:
			return nil, fmt.Errorf("unexpected intermediate message role %q", source.Role)
		}

		messages = append(messages, message)
	}

	if len(pending) != 0 {
		return nil, fmt.Errorf("tool calls are missing results")
	}
	messages = append(messages, answer)

	// 保证本轮消息排在上一轮之后，且本轮内顺序一致。
	base := time.Now().UTC()
	if len(history) != 0 {
		last := history[len(history)-1].CreatedAt
		if next := last.Add(time.Microsecond); next.After(base) {
			base = next
		}
	}
	for i, message := range messages {
		message.CreatedAt = base.Add(time.Duration(i) * time.Microsecond)
	}
	return messages, nil
}
