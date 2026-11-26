# G07 工具消息入库与跨轮回放

固定 Final 的 ADK 会合并流式消息，平台的会话存储保有 assistant 工具调用和 role=tool 消息。AsterFlow 让这对消息和用户消息、最终答复进入现有 CommitTurn 事务。

模型调用 ID 保存在 ProviderCallID 和 tool 消息的 ToolCallID，tool_calls 主键仍使用 AsterFlow UUID。版本 UUID 按快照里实际构造的名称查找。保留原始参数和返回字符串。给关系表 JSONB 写入合法对象和结果，参数非法时写空对象以满足既有约束，文字结果编码为 JSON 字符串。历史裁剪回退到用户消息边界，完整保留调用和结果；回放把非法参数正规化为对象。

学员已保存实现。正式工作区隔离 PostgreSQL race 直接验证独立 UUID、原始参数与 JSONB 对象、文本结果、固定工具版本、耗时与错误。桌面 Chrome 使用本地模型替身与真实 Crossborder PostgreSQL，验证同轮双工具、失败轨迹、刷新及下一轮调用 ID 回放。React 通过 93 项测试与构建。保存范围包含工具调用、结果和正文。详情见 [G07 验收](../../delivery/G07-VERIFICATION.md)。

## `internal/domain/conversation.go`

```diff
--- a/internal/domain/conversation.go
+++ b/internal/domain/conversation.go
@@ -1,6 +1,10 @@
 package domain

-import "time"
+import (
+	"encoding/json"
+	"strings"
+	"time"
+)

 type Conversation struct {
    ID                string     `json:"id"`
@@ -29,13 +33,25 @@
 }

 type ToolCall struct {
-	ID            string    `json:"id"`
-	MessageID     string    `json:"message_id"`
-	ToolID        string    `json:"tool_id,omitempty"`
-	ToolVersionID string    `json:"tool_version_id,omitempty"`
-	Args          string    `json:"args"`
-	Result        string    `json:"result,omitempty"`
-	Error         string    `json:"error,omitempty"`
-	LatencyMs     int       `json:"latency_ms"`
-	CreatedAt     time.Time `json:"created_at"`
+	ProviderCallID string    `json:"provider_call_id,omitempty"`
+	Name           string    `json:"name,omitempty"`
+	ID             string    `json:"id"`
+	MessageID      string    `json:"message_id"`
+	ToolID         string    `json:"tool_id,omitempty"`
+	ToolVersionID  string    `json:"tool_version_id,omitempty"`
+	Args           string    `json:"args"`
+	Result         string    `json:"result,omitempty"`
+	Error          string    `json:"error,omitempty"`
+	LatencyMs      int       `json:"latency_ms"`
+	CreatedAt      time.Time `json:"created_at"`
 }
+
+// ToolArgumentsObject supplies an object for the JSONB index and for model replay.
+// The transcript preserves the original raw arguments.
+func ToolArgumentsObject(raw string) string {
+	args := strings.TrimSpace(raw)
+	if strings.HasPrefix(args, "{") && json.Valid([]byte(args)) {
+		return args
+	}
+	return "{}"
+}
```

## `internal/runtime/engine/tools.go`

```diff
--- a/internal/runtime/engine/tools.go
+++ b/internal/runtime/engine/tools.go
@@ -16,37 +16,36 @@
    e.toolBuilder = builder
 }

-func (e *Executor) buildTools(ctx context.Context, in RunInput) ([]einotool.BaseTool, error) {
+func (e *Executor) buildTools(ctx context.Context, in RunInput) ([]einotool.BaseTool, map[string]string, error) {
    if len(in.ToolVersionIDs) == 0 {
-		return nil, nil
+		return nil, nil, nil
    }
    if e.toolBuilder == nil {
-		return nil, fmt.Errorf("tool builder is not configured")
+		return nil, nil, fmt.Errorf("tool builder is not configured")
    }
    tools := make([]einotool.BaseTool, 0, len(in.ToolVersionIDs))
-	seen := make(map[string]struct{}, len(in.ToolVersionIDs))
+	versions := make(map[string]string, len(in.ToolVersionIDs))

    for _, versionID := range in.ToolVersionIDs {
        // 沿用会话传入的固定版本和工作空间。
        built, err := e.toolBuilder.BuildByVersion(ctx, in.WorkspaceID, versionID)
        if err != nil {
-			return nil, fmt.Errorf("build tool version %s: %w", versionID, err)
+			return nil, nil, fmt.Errorf("build tool version %s: %w", versionID, err)
        }

        info, err := built.Info(ctx)
        if err != nil {
-			return nil, fmt.Errorf("read tool info for version %s: %w", versionID, err)
+			return nil, nil, fmt.Errorf("read tool info for version %s: %w", versionID, err)
        }

        // 模型按名称发起调用，重复的名字会让执行入口不唯一。
-		if _, exists := seen[info.Name]; exists {
-			return nil, fmt.Errorf("duplicate tool name %q in agent snapshot", info.Name)
+		if _, exists := versions[info.Name]; exists {
+			return nil, nil, fmt.Errorf("duplicate tool name %q in agent snapshot", info.Name)
        }
-		seen[info.Name] = struct{}{}
+		versions[info.Name] = versionID
        tools = append(tools, built)
    }

-	return tools, nil
+	return tools, versions, nil

 }
-
```

## `internal/runtime/engine/adk.go`

```diff
--- a/internal/runtime/engine/adk.go
+++ b/internal/runtime/engine/adk.go
@@ -178,8 +178,12 @@
        }

        var text strings.Builder
+		var chunks []*schema.Message
        var hasCalls, modelFinished bool
        err := consumeADKMessage(event.Output.MessageOutput, func(message *schema.Message) error {
+			if len(tools) > 0 && message != nil {
+				chunks = append(chunks, message)
+			}
            if message == nil || message.Role != schema.Assistant {
                return nil
            }
@@ -204,6 +208,14 @@
            return nil, err
        }

+		// 收集工具请求和结果消息，正文仍由最终助手消息单独返回。
+		if len(chunks) > 0 && (hasCalls || event.Output.MessageOutput.Role == schema.Tool) {
+			message, err := schema.ConcatMessages(chunks)
+			if err != nil {
+				return nil, fmt.Errorf("merge tool message: %w", err)
+			}
+			result.ToolMessages = append(result.ToolMessages, message)
+		}
        // 模型结束这条消息且没有提出工具调用时，收下最终正文。
        if modelFinished && !hasCalls {
            answerFinished = true
```

## `internal/runtime/engine/executor.go`

```diff
--- a/internal/runtime/engine/executor.go
+++ b/internal/runtime/engine/executor.go
@@ -47,6 +47,9 @@
 }

 type RunResult struct {
+	ToolMessages         []*schema.Message
+	ToolResults          []ToolResultEvent
+	ToolVersions         map[string]string
    UserPromptExperiment string
    UserPromptVariant    string
    RenderedUserPrompt   string
@@ -169,7 +172,7 @@
    }

    // 会话快照决定本轮可供 ADK 调用的工具。
-	tools, err := e.buildTools(ctx, in)
+	tools, versions, err := e.buildTools(ctx, in)
    if err != nil {
        if ctx.Err() != nil {
            return nil, ctx.Err()
@@ -191,7 +194,14 @@
            Seed:                  runtime.GenerationConfig.Seed,
        },
    }
-	result, err := runADK(ctx, chatModel, system, messages, tools, toolEventMiddleware(send), func(text string) error {
+	var toolResults []ToolResultEvent
+	sendToolEvent := func(kind EventType, data any) error {
+		if outcome, ok := data.(ToolResultEvent); ok {
+			toolResults = append(toolResults, outcome)
+		}
+		return send(kind, data)
+	}
+	result, err := runADK(ctx, chatModel, system, messages, tools, toolEventMiddleware(sendToolEvent), func(text string) error {
        return send(EventModelDelta, ModelDelta{Content: text})
    })
    if err != nil {
@@ -206,6 +216,8 @@
        return nil, err
    }
    result.NextSeq = seq
+	result.ToolResults = toolResults
+	result.ToolVersions = versions
    if userVersion != "" {
        result.RenderedUserPrompt = user
        result.UserPromptVersionID = userVersion
@@ -217,33 +229,64 @@
    return result, nil
 }

-// 把历史记录构建进消息
 func buildModelMessages(history []*domain.Message, user string) ([]*schema.Message, error) {
    if len(history) > maxHistoryMessages {
-		history = history[len(history)-maxHistoryMessages:]
+		start := len(history) - maxHistoryMessages
+		// 回到本轮用户消息，避免裁剪后从一条孤立的工具结果开始。
+		for start > 0 && history[start] != nil && history[start].Role != "user" {
+			start--
+		}
+		history = history[start:]
    }

    messages := make([]*schema.Message, 0, len(history)+1)
+	pending := make(map[string]struct{})
    for _, message := range history {
        if message == nil {
            return nil, fmt.Errorf("conversation history contains a nil message")
        }
-		if message.Role == "tool" || len(message.ToolCalls) > 0 || message.ToolCallID != nil {
-			return nil, fmt.Errorf("tool history requires the tool runtime")
-		}
-
+		if len(pending) != 0 && message.Role != "tool" {
+			return nil, fmt.Errorf("history tool calls are missing results")
+		}
        switch message.Role {
-		case "user", "assistant":
-			messages = append(messages, &schema.Message{
-				Role:    schema.RoleType(message.Role),
-				Content: message.Content,
-			})
+		case "user":
+			messages = append(messages, schema.UserMessage(message.Content))
+		case "assistant":
+			var calls []schema.ToolCall
+			for _, call := range message.ToolCalls {
+				id := call.ProviderCallID
+				if id == "" {
+					id = call.ID
+				}
+				_, exists := pending[id]
+				if id == "" || call.Name == "" || exists {
+					return nil, fmt.Errorf("invalid history tool call %q", id)
+				}
+				pending[id] = struct{}{}
+				calls = append(calls, schema.ToolCall{
+					ID: id, Type: "function",
+					Function: schema.FunctionCall{Name: call.Name, Arguments: domain.ToolArgumentsObject(call.Args)},
+				})
+			}
+			messages = append(messages, schema.AssistantMessage(message.Content, calls))
+		case "tool":
+			if message.ToolCallID == nil {
+				return nil, fmt.Errorf("history tool result is missing its call ID")
+			}
+			id := *message.ToolCallID
+			if _, exists := pending[id]; !exists {
+				return nil, fmt.Errorf("history tool result has no matching call %q", id)
+			}
+			delete(pending, id)
+			messages = append(messages, schema.ToolMessage(message.Content, id))
        case "system":
-			// The system prompt comes from the pinned runtime config.
+			// 系统提示词由本会话固定的运行时配置提供。
        default:
            return nil, fmt.Errorf("unsupported history role %q", message.Role)
        }
    }
-
+	if len(pending) != 0 {
+		return nil, fmt.Errorf("history tool calls are missing results")
+	}
    return append(messages, schema.UserMessage(user)), nil
 }
```

## `internal/platform/conversation/chat.go`

```diff
--- a/internal/platform/conversation/chat.go
+++ b/internal/platform/conversation/chat.go
@@ -162,6 +162,11 @@
        Content:        result.Content,
    }

+	messages, err := buildTurnMessages(userMessage, assistantMessage, history, result)
+	if err != nil {
+		return nil, err
+	}
+
    if cause := context.Cause(turnCtx); cause != nil {
        return nil, cause
    }
@@ -170,7 +175,7 @@
    commitCtx, cancelCommit := context.WithTimeout(ctx, turnCommitTimeout)
    err = s.store.CommitTurn(
        commitCtx, workspaceID, conv.ID, token,
-		[]*domain.Message{userMessage, assistantMessage},
+		messages,
        "active", userPromptSnapshot,
    )
    cancelCommit()
```

## `internal/platform/conversation/store.go`

```diff
--- a/internal/platform/conversation/store.go
+++ b/internal/platform/conversation/store.go
@@ -290,13 +290,17 @@
            if err != nil {
                return fmt.Errorf("parse tool call id: %w", err)
            }
-			args := call.Args
-			if args == "" {
-				args = "{}"
-			}
+			args := domain.ToolArgumentsObject(call.Args)
            var result []byte
            if call.Result != "" {
                result = []byte(call.Result)
+				if !json.Valid(result) {
+					// JSONB 能保存普通文本，但需要先把它编码成 JSON 字符串。
+					result, err = json.Marshal(call.Result)
+					if err != nil {
+						return fmt.Errorf("encode tool result: %w", err)
+					}
+				}
            }
            if _, err := q.InsertToolCall(ctx, pgstore.InsertToolCallParams{
                ID: callRow, MessageID: mid,
```

## 新建 `internal/platform/conversation/turn_messages.go`

```go
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
				// SQL 主键与模型生成的调用 ID 分别保存。
				message.ToolCalls[i] = domain.ToolCall{
					ID: uuid.NewString(), MessageID: message.ID,
					ProviderCallID: call.ID, Name: call.Function.Name,
					ToolVersionID: result.ToolVersions[call.Function.Name], Args: call.Function.Arguments,
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

	// 同一会话按时间读取消息。确保本轮从上一轮之后开始，并保留本轮的顺序。
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
```

## 验收

学员保存后助手将采用新的 buildTools 签名更新既有 Engine 测试，并合入 PostgreSQL 关系表、回放窗口及隔离浏览器回归。以正式源码重跑 G07 整组验收后再判断学习日完成。继续验证真实供应商和工具覆盖。
