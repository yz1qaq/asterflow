package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
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

func (m *gatewayChatModel) modelRequest(messages []*schema.Message) llm.GenerateRequest {
	req := m.request
	if len(messages) > 0 && messages[0].Role == schema.System {
		req.SystemPrompt = messages[0].Content
		messages = messages[1:]
	}
	req.Messages = make([]llm.Message, 0, len(messages))
	for _, message := range messages {
		req.Messages = append(req.Messages, llm.Message{Role: string(message.Role), Content: message.Content})
	}
	return req
}

func (m *gatewayChatModel) Generate(ctx context.Context, messages []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	response, err := m.gateway.Generate(ctx, m.workspaceID, m.classification, m.modelRequest(messages))
	if err != nil {
		return nil, err
	}
	if response == nil || len(response.ToolCalls) != 0 {
		return nil, fmt.Errorf("unexpected model response for an agent without tools")
	}
	return assistantResponse(response.Content, response.Model, response.Usage), nil
}

func (m *gatewayChatModel) Stream(ctx context.Context, messages []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	stream, err := m.gateway.Stream(ctx, m.workspaceID, m.classification, m.modelRequest(messages))
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
			if chunk == nil || len(chunk.ToolCalls) != 0 {
				writer.Send(nil, fmt.Errorf("unexpected model chunk for an agent without tools"))
				return
			}

			message := schema.AssistantMessage(chunk.Content, nil)
			if chunk.Done {
				message = assistantResponse(chunk.Content, chunk.Model, chunk.Usage)
			}
			if writer.Send(message, nil) || chunk.Done {
				return
			}
		}
	}()
	return reader, nil
}

func assistantResponse(content, name string, usage llm.Usage) *schema.Message {
	message := schema.AssistantMessage(content, nil)
	message.ResponseMeta = &schema.ResponseMeta{Usage: &schema.TokenUsage{
		PromptTokens: usage.PromptTokens, CompletionTokens: usage.CompletionTokens, TotalTokens: usage.TotalTokens,
	}}
	message.Extra = map[string]any{responseModelKey: name}
	return message
}

var errModelStart = errors.New("start model stream")

func runADK(
	ctx context.Context, chatModel model.BaseChatModel, system string, messages []*schema.Message,
	onDelta func(string) error,
) (*RunResult, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name: "asterflow", Description: "AsterFlow conversation agent", Instruction: system, Model: chatModel,
	})
	if err != nil {
		return nil, fmt.Errorf("create Eino agent: %w", err)
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, EnableStreaming: true})
	events := runner.Run(ctx, messages)

	var content strings.Builder
	var result RunResult
	var modelFinished bool
	consume := func(message *schema.Message) error {
		if message == nil || message.Role != schema.Assistant {
			return nil
		}
		if message.Content != "" {
			content.WriteString(message.Content)
			if err := onDelta(message.Content); err != nil {
				return err
			}
		}
		if meta := message.ResponseMeta; meta != nil && meta.Usage != nil {
			modelFinished = true
			result.Model, _ = message.Extra[responseModelKey].(string)
			result.Usage = llm.Usage{
				PromptTokens:     meta.Usage.PromptTokens,
				CompletionTokens: meta.Usage.CompletionTokens,
				TotalTokens:      meta.Usage.TotalTokens,
			}
		}
		return nil
	}

	for event, ok := events.Next(); ok; event, ok = events.Next() {
		if event.Err != nil {
			return nil, event.Err
		}
		if event.Action != nil && event.Action.Interrupted != nil {
			return nil, fmt.Errorf("agent interrupted")
		}
		if event.Output != nil && event.Output.MessageOutput != nil {
			if err := consumeADKMessage(event.Output.MessageOutput, consume); err != nil {
				return nil, err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !modelFinished {
		return nil, fmt.Errorf("agent completed before the model stream finished")
	}
	result.Content = content.String()
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
