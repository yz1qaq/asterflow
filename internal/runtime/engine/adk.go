package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/patchtoolcalls"
	"github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/yz1qaq/asterflow/internal/runtime/llm"
)

const responseModelKey = "asterflow_model"

// gatewayChatModel adapts one pinned AsterFlow invocation to Eino's model contract.
type gatewayChatModel struct {
	gateway        llm.Gateway
	workspaceID    string
	classification string
	request        llm.GenerateRequest
}

var _ model.BaseChatModel = (*gatewayChatModel)(nil)

// 把内部的llm.message转换为schema.message
func (m *gatewayChatModel) modelRequest(
	messages []*schema.Message, opts ...model.Option) llm.GenerateRequest {
	req := m.request
	// ADK 通过 WithTools 传来本轮可用的工具定义。
	req.Tools = model.GetCommonOptions(nil, opts...).Tools
	//提取系统提示词
	if len(messages) > 0 && messages[0].Role == schema.System {
		req.SystemPrompt = messages[0].Content
		messages = messages[1:]
	}

	//初始化消息切片
	req.Messages = make([]llm.Message, 0, len(messages))
	for _, message := range messages {
		// 保留助手的工具请求、工具结果及其关联 ID。
		req.Messages = append(req.Messages, llm.Message{
			Role: string(message.Role), Content: message.Content,
			ToolCalls: message.ToolCalls, ToolCallID: message.ToolCallID,
			ReasoningContent: message.ReasoningContent,
		})
	}
	return req
}

func (m *gatewayChatModel) Generate(
	ctx context.Context, messages []*schema.Message, opts ...model.Option,
) (*schema.Message, error) {
	req := m.modelRequest(messages, opts...)
	response, err := m.gateway.Generate(ctx, m.workspaceID, m.classification, req)
	if err != nil {
		return nil, err
	}
	if response == nil || (len(req.Tools) == 0 && len(response.ToolCalls) != 0) {
		return nil, fmt.Errorf("unexpected model response")
	}

	message := schema.AssistantMessage(response.Content, response.ToolCalls)
	message.ReasoningContent = response.ReasoningContent
	setModelResponseMeta(message, response.Model, response.Usage)
	return message, nil
}

func (m *gatewayChatModel) Stream(
	ctx context.Context, messages []*schema.Message, opts ...model.Option,
) (*schema.StreamReader[*schema.Message], error) {
	req := m.modelRequest(messages, opts...)
	stream, err := m.gateway.Stream(ctx, m.workspaceID, m.classification, req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errModelStart, err)
	}
	if stream == nil {
		return nil, fmt.Errorf("%w: gateway returned no stream", errModelStart)
	}

	reader, writer := schema.Pipe[*schema.Message](1)
	go func() {
		defer writer.Close()
		defer func() { _ = stream.Close() }()

		for {
			chunk, err := stream.Recv(ctx)
			if err != nil {
				if errors.Is(err, io.EOF) {
					err = io.ErrUnexpectedEOF
				}
				writer.Send(nil, err)
				return
			}
			if chunk == nil || (len(req.Tools) == 0 && len(chunk.ToolCalls) != 0) {
				writer.Send(nil, fmt.Errorf("unexpected model stream chunk"))
				return
			}

			// 把带 index 的调用片段交给 ADK，由框架拼接参数。
			message := schema.AssistantMessage(chunk.Content, chunk.ToolCalls)
			message.ReasoningContent = chunk.ReasoningContent
			if chunk.Done {
				setModelResponseMeta(message, chunk.Model, chunk.Usage)
			}
			if writer.Send(message, nil) || chunk.Done {
				return
			}
		}
	}()
	return reader, nil
}

func setModelResponseMeta(message *schema.Message, name string, usage llm.Usage) {
	message.ResponseMeta = &schema.ResponseMeta{Usage: &schema.TokenUsage{
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.TotalTokens,
	}}
	message.Extra = map[string]any{responseModelKey: name}
}

var errModelStart = errors.New("start model stream")

func runADK(
	ctx context.Context, chatModel model.BaseChatModel, system string, messages []*schema.Message,
	tools []einotool.BaseTool, sendToolEvent func(EventType, any) error, onDelta func(string) error,
) (*RunResult, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	config := &adk.ChatModelAgentConfig{
		Name: "asterflow", Description: "AsterFlow conversation agent",
		Instruction: system, Model: chatModel, MaxIterations: 6,
	}
	if len(tools) > 0 {
		// 沿用 Final 的顺序执行，以及补齐缺失工具结果的中间件。
		patch, err := patchtoolcalls.New(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("create tool-call patch middleware: %w", err)
		}
		config.Handlers = []adk.ChatModelAgentMiddleware{patch}
		config.ToolsConfig = adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{
			Tools: tools, ExecuteSequentially: true,
			ToolCallMiddlewares: []compose.ToolMiddleware{toolEventMiddleware(sendToolEvent)},
			UnknownToolsHandler: func(toolCtx context.Context, name, args string) (string, error) {
				// 未知工具走 Eino 的回退路径，从上下文取原来的调用 ID。
				callID := compose.GetToolCallID(toolCtx)
				if err := sendToolEvent(EventToolCall, ToolCallEvent{
					CallID: callID, Name: name, Arguments: args,
				}); err != nil {
					return "", err
				}
				// 用同一个 ID 标记失败，供实时界面和历史记录配对。
				if err := sendToolEvent(EventToolResult, ToolResultEvent{
					CallID: callID, Name: name, Status: "error", Error: "工具不在本轮可用列表中",
				}); err != nil {
					return "", err
				}
				output := fmt.Sprintf("error: tool %q is not available", name)
				// 回退函数绕过 ADK 的工具事件包装，显式发送消息供既有入库逻辑收集。
				message := schema.ToolMessage(output, callID, schema.WithToolName(name))
				if err := adk.SendEvent(toolCtx, adk.EventFromMessage(message, nil, schema.Tool, name)); err != nil {
					return "", err
				}
				// 返回同一条结果，由 Eino 放进下一次模型请求。
				return output, nil
			},
		}}
	}

	agent, err := adk.NewChatModelAgent(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create Eino agent: %w", err)
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, EnableStreaming: true})
	events := runner.Run(ctx, messages)

	var result RunResult
	var answerFinished bool
	for event, ok := events.Next(); ok; event, ok = events.Next() {
		if event == nil {
			continue
		}
		if event.Err != nil {
			return nil, event.Err
		}
		if event.Action != nil && event.Action.Interrupted != nil {
			return nil, fmt.Errorf("agent interrupted")
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}

		var text strings.Builder
		var chunks []*schema.Message
		var hasCalls, modelFinished bool
		err := consumeADKMessage(event.Output.MessageOutput, func(message *schema.Message) error {
			if len(tools) > 0 && message != nil {
				chunks = append(chunks, message)
			}
			if message == nil || message.Role != schema.Assistant {
				return nil
			}

			hasCalls = hasCalls || len(message.ToolCalls) > 0
			text.WriteString(message.Content)
			if meta := message.ResponseMeta; meta != nil && meta.Usage != nil {
				modelFinished = true
				result.Model, _ = message.Extra[responseModelKey].(string)

				// 工具调用前后都可能请求模型，用量要逐次累加。
				result.Usage.PromptTokens += meta.Usage.PromptTokens
				result.Usage.CompletionTokens += meta.Usage.CompletionTokens
				result.Usage.TotalTokens += meta.Usage.TotalTokens
			}
			if len(tools) == 0 && message.Content != "" {
				return onDelta(message.Content)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}

		// 合并同一条消息的片段，保存工具请求和对应的结果消息。
		if len(chunks) > 0 && (hasCalls || event.Output.MessageOutput.Role == schema.Tool) {
			message, err := schema.ConcatMessages(chunks)
			if err != nil {
				return nil, fmt.Errorf("merge tool message: %w", err)
			}
			result.ToolMessages = append(result.ToolMessages, message)
		}

		// 模型结束这条消息且没有提出工具调用时，收下最终正文。
		if modelFinished && !hasCalls {
			answerFinished = true
			result.Content = text.String()
			if len(tools) > 0 && result.Content != "" {
				if err := onDelta(result.Content); err != nil {
					return nil, err
				}
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !answerFinished {
		return nil, fmt.Errorf("agent completed before the model stream finished")
	}
	return &result, nil
}

func consumeADKMessage(variant *adk.MessageVariant, consume func(*schema.Message) error) error {
	if !variant.IsStreaming {
		return consume(variant.Message)
	}

	stream := variant.MessageStream
	defer stream.Close()
	for {
		message, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := consume(message); err != nil {
			return err
		}
	}
}
