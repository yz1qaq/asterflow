package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	sdkopenai "github.com/meguminnnnnnnnn/go-openai"
)

type EinoOpenAIClient struct {
	http *http.Client
}

func NewEinoOpenAIClient() *EinoOpenAIClient {
	return &EinoOpenAIClient{http: &http.Client{}}
}

func (c *EinoOpenAIClient) newModel(
	ctx context.Context, dep ResolvedDeployment, req GenerateRequest,
) (*einoopenai.ChatModel, error) {
	if strings.TrimSpace(dep.BaseURL) == "" {
		return nil, fmt.Errorf("model deployment base URL is required")
	}

	// 网关先选部署，再把部署地址、凭据和生成参数交给 Eino。
	return einoopenai.NewChatModel(ctx, &einoopenai.ChatModelConfig{
		BaseURL: dep.BaseURL, APIKey: dep.APIKey, Model: dep.Model, HTTPClient: c.http,
		Temperature: req.Temperature, TopP: req.TopP, MaxTokens: req.MaxOutputTokens,
		Stop: req.Stop, Seed: req.Seed,
	})
}

func einoMessages(req GenerateRequest) []*schema.Message {
	messages := make([]*schema.Message, 0, len(req.Messages)+1)
	if strings.TrimSpace(req.SystemPrompt) != "" {
		messages = append(messages, schema.SystemMessage(req.SystemPrompt))
	}
	for _, msg := range req.Messages {
		// 保留助手的调用和工具结果的关联 ID，供模型继续推理。
		messages = append(messages, &schema.Message{
			Role: schema.RoleType(msg.Role), Content: msg.Content,
			ToolCalls: msg.ToolCalls, ToolCallID: msg.ToolCallID,
			ReasoningContent: msg.ReasoningContent,
		})
	}
	return messages
}

func (c *EinoOpenAIClient) Generate(
	ctx context.Context, dep ResolvedDeployment, req GenerateRequest,
) (*GenerateResponse, error) {
	chat, err := c.newModel(ctx, dep, req)
	if err != nil {
		return nil, err
	}

	modelName := dep.Model
	msg, err := chat.Generate(ctx, einoMessages(req), einoModelOptions(req.Tools, &modelName)...)
	if err != nil {
		return nil, einoProviderError(err)
	}
	return &GenerateResponse{
		Model: modelName, DeploymentID: dep.ID,
		Content: msg.Content, ToolCalls: msg.ToolCalls,
		ReasoningContent: msg.ReasoningContent, Usage: einoUsage(msg),
	}, nil
}

func (c *EinoOpenAIClient) Stream(
	ctx context.Context, dep ResolvedDeployment, req GenerateRequest,
) (Stream, error) {
	chat, err := c.newModel(ctx, dep, req)
	if err != nil {
		return nil, err
	}

	streamCtx, cancel := context.WithCancel(ctx)
	stream := &einoStream{cancel: cancel, modelName: dep.Model, deploymentID: dep.ID}
	reader, err := chat.Stream(streamCtx, einoMessages(req), einoModelOptions(req.Tools, &stream.modelName)...)
	if err != nil {
		cancel()
		return nil, einoProviderError(err)
	}
	stream.reader = reader
	return stream, nil
}

type einoStream struct {
	reader       *schema.StreamReader[*schema.Message]
	cancel       context.CancelFunc
	modelName    string
	deploymentID string
	usage        Usage
	finished     bool
}

func (s *einoStream) Recv(ctx context.Context) (*StreamChunk, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		msg, err := s.reader.Recv()
		if cause := ctx.Err(); cause != nil {
			return nil, cause
		}
		if errors.Is(err, io.EOF) {
			// 有结束原因才接受 EOF，避免把提前断流当作成功。
			if !s.finished {
				return nil, &ProviderError{Err: io.ErrUnexpectedEOF, Retryable: true}
			}
			return &StreamChunk{
				Done: true, Model: s.modelName, DeploymentID: s.deploymentID, Usage: s.usage,
			}, nil
		}
		if err != nil {
			return nil, einoProviderError(err)
		}
		if meta := msg.ResponseMeta; meta != nil {
			s.finished = s.finished || meta.FinishReason != ""
			if meta.Usage != nil {
				s.usage = einoUsage(msg)
			}
		}

		// 原样转发 Eino 解析出的工具调用片段及 index。
		if msg.Content != "" || len(msg.ToolCalls) > 0 || msg.ReasoningContent != "" {
			return &StreamChunk{
				Content: msg.Content, ToolCalls: msg.ToolCalls, ReasoningContent: msg.ReasoningContent,
			}, nil
		}
	}
}

func (s *einoStream) Close() error {
	s.cancel()
	s.reader.Close()
	return nil
}

func einoModelOptions(tools []*schema.ToolInfo, modelName *string) []model.Option {
	// 供应商可能返回具体模型名，覆盖部署配置中的别名。
	readModel := func(_ context.Context, msg *schema.Message, raw []byte) (*schema.Message, error) {
		var data struct {
			Model string `json:"model"`
		}
		if json.Unmarshal(raw, &data) == nil && data.Model != "" {
			*modelName = data.Model
		}
		return msg, nil
	}
	return []model.Option{
		model.WithTools(tools),
		einoopenai.WithResponseMessageModifier(readModel),
		einoopenai.WithResponseChunkMessageModifier(func(ctx context.Context, msg *schema.Message, raw []byte, _ bool) (*schema.Message, error) {
			return readModel(ctx, msg, raw)
		}),
	}
}

func einoUsage(msg *schema.Message) Usage {
	if msg.ResponseMeta == nil || msg.ResponseMeta.Usage == nil {
		return Usage{}
	}
	u := msg.ResponseMeta.Usage
	return Usage{PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens, TotalTokens: u.TotalTokens}
}

func einoProviderError(err error) error {
	var apiErr *einoopenai.APIError
	var requestErr *sdkopenai.RequestError
	var networkErr net.Error

	status := 0
	switch {
	case errors.As(err, &apiErr):
		status = apiErr.HTTPStatusCode
	case errors.As(err, &requestErr):
		status = requestErr.HTTPStatusCode
	}

	// 限流、上游故障或网络错误可触发备用部署。
	retryable := status == http.StatusTooManyRequests || status >= 500
	if status == 0 {
		retryable = errors.As(err, &networkErr)
	}
	return &ProviderError{StatusCode: status, Err: err, Retryable: retryable}
}
