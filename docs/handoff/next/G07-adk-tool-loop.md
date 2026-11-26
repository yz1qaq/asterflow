# G07 Eino ADK 执行固定版本工具

固定 Final 把工具列表交给 ADK ToolsConfig，顺序执行，默认最多六次模型迭代，并利用 patchtoolcalls 补齐缺失结果。AsterFlow 沿用 ADK，现有 gatewayChatModel 读取 WithTools 选项，将工具定义和消息元数据带给模型网关；Eino 负责工具执行和同轮结果回喂。

本批为学员手写候选。正式工作区的 Eino 模型适配器回归已并入既有测试并通过。候选 ADK 在真实桌面 Chrome、自带模型协议替身、隔离平台 PostgreSQL 和 Crossborder 独立 PostgreSQL 测出工具定义下发、分片参数拼接、真实订单状态、相同调用 ID 回喂、最终正文与两次生成的合计用量。现有服务端普通无工具串流保持逐片输出；有工具的 Agent 收齐一条模型消息再仅发最终正文，避免把发起工具调用时的旁白混进最终文本。工具事件、工具调用结果持久化和跨轮回放在同组后续实现。

## `internal/runtime/engine/adk.go`

```diff
--- a/internal/runtime/engine/adk.go
+++ b/internal/runtime/engine/adk.go
@@ -8,14 +8,16 @@
    "strings"

    "github.com/cloudwego/eino/adk"
+	"github.com/cloudwego/eino/adk/middlewares/patchtoolcalls"
    "github.com/cloudwego/eino/components/model"
+	einotool "github.com/cloudwego/eino/components/tool"
+	"github.com/cloudwego/eino/compose"
    "github.com/cloudwego/eino/schema"
    "github.com/yz1qaq/asterflow/internal/runtime/llm"
 )

 const responseModelKey = "asterflow_model"

-// gatewayChatModel adapts one pinned AsterFlow invocation to Eino's model contract.
 type gatewayChatModel struct {
    gateway        llm.Gateway
    workspaceID    string
@@ -25,32 +27,49 @@

 var _ model.BaseChatModel = (*gatewayChatModel)(nil)

-func (m *gatewayChatModel) modelRequest(messages []*schema.Message) llm.GenerateRequest {
+func (m *gatewayChatModel) modelRequest(messages []*schema.Message, opts ...model.Option) llm.GenerateRequest {
    req := m.request
+	// ADK 通过 WithTools 传来本轮可用的工具定义。
+	req.Tools = model.GetCommonOptions(nil, opts...).Tools
    if len(messages) > 0 && messages[0].Role == schema.System {
        req.SystemPrompt = messages[0].Content
        messages = messages[1:]
    }
+
    req.Messages = make([]llm.Message, 0, len(messages))
    for _, message := range messages {
-		req.Messages = append(req.Messages, llm.Message{Role: string(message.Role), Content: message.Content})
+		// 保留助手的工具请求、工具结果及其关联 ID。
+		req.Messages = append(req.Messages, llm.Message{
+			Role: string(message.Role), Content: message.Content,
+			ToolCalls: message.ToolCalls, ToolCallID: message.ToolCallID,
+			ReasoningContent: message.ReasoningContent,
+		})
    }
    return req
 }

-func (m *gatewayChatModel) Generate(ctx context.Context, messages []*schema.Message, _ ...model.Option) (*schema.Message, error) {
-	response, err := m.gateway.Generate(ctx, m.workspaceID, m.classification, m.modelRequest(messages))
+func (m *gatewayChatModel) Generate(
+	ctx context.Context, messages []*schema.Message, opts ...model.Option,
+) (*schema.Message, error) {
+	req := m.modelRequest(messages, opts...)
+	response, err := m.gateway.Generate(ctx, m.workspaceID, m.classification, req)
    if err != nil {
        return nil, err
    }
-	if response == nil || len(response.ToolCalls) != 0 {
-		return nil, fmt.Errorf("unexpected model response for an agent without tools")
-	}
-	return assistantResponse(response.Content, response.Model, response.Usage), nil
-}
-
-func (m *gatewayChatModel) Stream(ctx context.Context, messages []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
-	stream, err := m.gateway.Stream(ctx, m.workspaceID, m.classification, m.modelRequest(messages))
+	if response == nil || (len(req.Tools) == 0 && len(response.ToolCalls) != 0) {
+		return nil, fmt.Errorf("unexpected model response")
+	}
+	message := schema.AssistantMessage(response.Content, response.ToolCalls)
+	message.ReasoningContent = response.ReasoningContent
+	setModelResponseMeta(message, response.Model, response.Usage)
+	return message, nil
+}
+
+func (m *gatewayChatModel) Stream(
+	ctx context.Context, messages []*schema.Message, opts ...model.Option,
+) (*schema.StreamReader[*schema.Message], error) {
+	req := m.modelRequest(messages, opts...)
+	stream, err := m.gateway.Stream(ctx, m.workspaceID, m.classification, req)
    if err != nil {
        return nil, fmt.Errorf("%w: %w", errModelStart, err)
    }
@@ -71,14 +90,16 @@
                writer.Send(nil, err)
                return
            }
-			if chunk == nil || len(chunk.ToolCalls) != 0 {
-				writer.Send(nil, fmt.Errorf("unexpected model chunk for an agent without tools"))
+			if chunk == nil || (len(req.Tools) == 0 && len(chunk.ToolCalls) != 0) {
+				writer.Send(nil, fmt.Errorf("unexpected model stream chunk"))
                return
            }

-			message := schema.AssistantMessage(chunk.Content, nil)
+			// 带 index 的 ToolCalls 片段交给 ADK，由框架把参数拼起来。
+			message := schema.AssistantMessage(chunk.Content, chunk.ToolCalls)
+			message.ReasoningContent = chunk.ReasoningContent
            if chunk.Done {
-				message = assistantResponse(chunk.Content, chunk.Model, chunk.Usage)
+				setModelResponseMeta(message, chunk.Model, chunk.Usage)
            }
            if writer.Send(message, nil) || chunk.Done {
                return
@@ -88,78 +109,107 @@
    return reader, nil
 }

-func assistantResponse(content, name string, usage llm.Usage) *schema.Message {
-	message := schema.AssistantMessage(content, nil)
+func setModelResponseMeta(message *schema.Message, name string, usage llm.Usage) {
    message.ResponseMeta = &schema.ResponseMeta{Usage: &schema.TokenUsage{
        PromptTokens: usage.PromptTokens, CompletionTokens: usage.CompletionTokens, TotalTokens: usage.TotalTokens,
    }}
    message.Extra = map[string]any{responseModelKey: name}
-	return message
 }

 var errModelStart = errors.New("start model stream")

 func runADK(
    ctx context.Context, chatModel model.BaseChatModel, system string, messages []*schema.Message,
-	onDelta func(string) error,
+	tools []einotool.BaseTool, onDelta func(string) error,
 ) (*RunResult, error) {
    ctx, cancel := context.WithCancel(ctx)
    defer cancel()

-	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
+	config := &adk.ChatModelAgentConfig{
        Name: "asterflow", Description: "AsterFlow conversation agent", Instruction: system, Model: chatModel,
-	})
+		MaxIterations: 6,
+	}
+	if len(tools) > 0 {
+		// 按顺序执行工具，并用 Final 的中间件补齐缺失的工具结果。
+		patch, err := patchtoolcalls.New(ctx, nil)
+		if err != nil {
+			return nil, fmt.Errorf("create tool-call patch middleware: %w", err)
+		}
+		config.Handlers = []adk.ChatModelAgentMiddleware{patch}
+		config.ToolsConfig = adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{
+			Tools: tools, ExecuteSequentially: true,
+			UnknownToolsHandler: func(_ context.Context, name, _ string) (string, error) {
+				return fmt.Sprintf("error: tool %q is not available", name), nil
+			},
+		}}
+	}
+
+	agent, err := adk.NewChatModelAgent(ctx, config)
    if err != nil {
        return nil, fmt.Errorf("create Eino agent: %w", err)
    }
    runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, EnableStreaming: true})
    events := runner.Run(ctx, messages)

-	var content strings.Builder
    var result RunResult
-	var modelFinished bool
-	consume := func(message *schema.Message) error {
-		if message == nil || message.Role != schema.Assistant {
-			return nil
-		}
-		if message.Content != "" {
-			content.WriteString(message.Content)
-			if err := onDelta(message.Content); err != nil {
-				return err
-			}
-		}
-		if meta := message.ResponseMeta; meta != nil && meta.Usage != nil {
-			modelFinished = true
-			result.Model, _ = message.Extra[responseModelKey].(string)
-			result.Usage = llm.Usage{
-				PromptTokens:     meta.Usage.PromptTokens,
-				CompletionTokens: meta.Usage.CompletionTokens,
-				TotalTokens:      meta.Usage.TotalTokens,
-			}
-		}
-		return nil
-	}
-
+	var answerFinished bool
    for event, ok := events.Next(); ok; event, ok = events.Next() {
+		if event == nil {
+			continue
+		}
        if event.Err != nil {
            return nil, event.Err
        }
        if event.Action != nil && event.Action.Interrupted != nil {
            return nil, fmt.Errorf("agent interrupted")
        }
-		if event.Output != nil && event.Output.MessageOutput != nil {
-			if err := consumeADKMessage(event.Output.MessageOutput, consume); err != nil {
-				return nil, err
-			}
-		}
-	}
+		if event.Output == nil || event.Output.MessageOutput == nil {
+			continue
+		}
+
+		var text strings.Builder
+		var hasCalls, modelFinished bool
+		err := consumeADKMessage(event.Output.MessageOutput, func(message *schema.Message) error {
+			if message == nil || message.Role != schema.Assistant {
+				return nil
+			}
+			hasCalls = hasCalls || len(message.ToolCalls) > 0
+			text.WriteString(message.Content)
+			if meta := message.ResponseMeta; meta != nil && meta.Usage != nil {
+				modelFinished = true
+				result.Model, _ = message.Extra[responseModelKey].(string)
+				// 一轮聊天可能请求模型多次，用量逐次累加。
+				result.Usage.PromptTokens += meta.Usage.PromptTokens
+				result.Usage.CompletionTokens += meta.Usage.CompletionTokens
+				result.Usage.TotalTokens += meta.Usage.TotalTokens
+			}
+			if len(tools) == 0 && message.Content != "" {
+				return onDelta(message.Content)
+			}
+			return nil
+		})
+		if err != nil {
+			return nil, err
+		}
+
+		// 这条助手消息结束且没有提出新工具调用，才是本轮的最终回复。
+		if modelFinished && !hasCalls {
+			answerFinished = true
+			result.Content = text.String()
+			if len(tools) > 0 && result.Content != "" {
+				if err := onDelta(result.Content); err != nil {
+					return nil, err
+				}
+			}
+		}
+	}
+
    if err := ctx.Err(); err != nil {
        return nil, err
    }
-	if !modelFinished {
+	if !answerFinished {
        return nil, fmt.Errorf("agent completed before the model stream finished")
    }
-	result.Content = content.String()
    return &result, nil
 }

```

## `internal/runtime/engine/executor.go`

```diff
--- a/internal/runtime/engine/executor.go
+++ b/internal/runtime/engine/executor.go
@@ -167,6 +167,16 @@
        _ = send(EventRunFailed, RunFailed{Code: "model_unavailable", Message: err.Error()})
        return nil, err
    }
+	// 会话快照决定本轮可供 ADK 调用的工具。
+	tools, err := e.buildTools(ctx, in)
+	if err != nil {
+		if ctx.Err() != nil {
+			return nil, ctx.Err()
+		}
+		_ = send(EventRunFailed, RunFailed{Code: "tool_build_error", Message: "工具加载失败"})
+		return nil, err
+	}
+
    chatModel := &gatewayChatModel{
        gateway: e.models, workspaceID: in.WorkspaceID, classification: in.Classification,
        request: llm.GenerateRequest{
@@ -178,7 +188,7 @@
            Seed:                  runtime.GenerationConfig.Seed,
        },
    }
-	result, err := runADK(ctx, chatModel, system, messages, func(text string) error {
+	result, err := runADK(ctx, chatModel, system, messages, tools, func(text string) error {
        return send(EventModelDelta, ModelDelta{Content: text})
    })
    if err != nil {
```

## 验收与后续

学员已保存，正式编排回归通过。后续接入 [实时工具事件](G07-live-tool-events.md)，通过同一会话事务持久化工具轨迹及跨轮历史，再验收刷新保留。供应商连接测试仍使用单独的探测入口；清理旧模型实现时保留探测禁重定向行为。
