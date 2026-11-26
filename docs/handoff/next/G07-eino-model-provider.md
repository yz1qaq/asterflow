# G07 Eino OpenAI 模型适配器

固定 Final 使用 github.com/cloudwego/eino-ext/components/model/openai v0.1.13。AsterFlow 让正式聊天的现有 FailoverGateway 使用相同 Eino OpenAI 组件，继续保有工作空间、模型方案、超时和主备策略。供应商试跑入口另有禁止重定向的探测客户端。AsterFlow 延续现有供应商兼容字段 max_tokens。

本批为学员手写候选。由 Eino 组件负责工具描述和 OpenAI 协议，保留 assistant.tool_calls 和工具结果的 tool_call_id，以及多片参数的 index。适配器继续返回原有 Gateway 的 StreamChunk 和 Usage。模型调用结束时产生 Done；断流前没有结束原因时报意外 EOF。

候选编译、模型与 Engine race 通过。本地模型 HTTP 替身验证了 tools、ToolCallID、分段 arguments、返回模型名称、usage、429/502 与 401、取消和提前 EOF。隔离桌面 Chrome 使用本地模型替身通过首轮模板、多轮、主备、提前 EOF、409、取消和刷新回读。

## `internal/runtime/llm/types.go`

```diff
--- a/internal/runtime/llm/types.go
+++ b/internal/runtime/llm/types.go
@@ -3,6 +3,8 @@
 import (
    "context"
    "errors"
+
+	"github.com/cloudwego/eino/schema"
 )

 var (
@@ -11,31 +13,29 @@
    ErrQuotaExceeded        = errors.New("model usage quota exceeded")
 )

-type ToolCall struct {
-	ID        string `json:"id"`
-	Name      string `json:"name"`      //函数名
-	Arguments string `json:"arguments"` //Json格式的参数
-
-}
+// ToolCall 使用 Eino 的结构，保留流式工具调用的 index。
+type ToolCall = schema.ToolCall

 type Message struct {
-	Role       string     `json:"role"`
-	Content    string     `json:"content"`
-	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
-	ToolCallID string     `json:"tool_call_id,omitempty"`
+	ReasoningContent string     `json:"reasoning_content,omitempty"`
+	Role             string     `json:"role"`
+	Content          string     `json:"content"`
+	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
+	ToolCallID       string     `json:"tool_call_id,omitempty"`
 }

 // GenerateRequest is the business-level model call. It never carries API keys;
 // the gateway resolves deployments from the model profile version.
 type GenerateRequest struct {
-	ModelProfileVersionID string    `json:"model_profile_version_id"`
-	SystemPrompt          string    `json:"system_prompt"`
-	Messages              []Message `json:"messages"`
-	Temperature           *float32  `json:"temperature,omitempty"`
-	TopP                  *float32  `json:"top_p,omitempty"`
-	MaxOutputTokens       *int      `json:"max_output_tokens,omitempty"`
-	Stop                  []string  `json:"stop,omitempty"`
-	Seed                  *int      `json:"seed,omitempty"`
+	Tools                 []*schema.ToolInfo `json:"-"`
+	ModelProfileVersionID string             `json:"model_profile_version_id"`
+	SystemPrompt          string             `json:"system_prompt"`
+	Messages              []Message          `json:"messages"`
+	Temperature           *float32           `json:"temperature,omitempty"`
+	TopP                  *float32           `json:"top_p,omitempty"`
+	MaxOutputTokens       *int               `json:"max_output_tokens,omitempty"`
+	Stop                  []string           `json:"stop,omitempty"`
+	Seed                  *int               `json:"seed,omitempty"`
 }

 type Usage struct {
@@ -45,17 +45,19 @@
 }

 type GenerateResponse struct {
-	Content      string     `json:"content"`
-	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
-	Usage        Usage      `json:"usage"`
-	Model        string     `json:"model"`
-	DeploymentID string     `json:"deployment_id"`
+	ReasoningContent string     `json:"reasoning_content,omitempty"`
+	Content          string     `json:"content"`
+	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
+	Usage            Usage      `json:"usage"`
+	Model            string     `json:"model"`
+	DeploymentID     string     `json:"deployment_id"`
 }

 type StreamChunk struct {
-	Content   string     `json:"content,omitempty"`
-	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
-	Done      bool       `json:"done"`
+	ReasoningContent string     `json:"reasoning_content,omitempty"`
+	Content          string     `json:"content,omitempty"`
+	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
+	Done             bool       `json:"done"`
    // Filled by adapters on the final chunk (Done=true); optional on deltas.
    Model        string `json:"model,omitempty"`
    DeploymentID string `json:"deployment_id,omitempty"`
```

## 新建 `internal/runtime/llm/eino_openai.go`

```go
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
	// 每次模型调用由网关先选部署，再把该部署的模型参数交给 Eino。
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
		// 工具调用和结果保留同一个 tool_call_id，供模型衔接上一轮输出。
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
	// 供应商响应可能返回具体模型名，覆盖部署配置中的别名。
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
	// 沿用原网关的切换条件。限流、上游故障或网络错误可触发备用部署。
	retryable := status == http.StatusTooManyRequests || status >= 500
	if status == 0 {
		retryable = errors.As(err, &networkErr)
	}
	return &ProviderError{StatusCode: status, Err: err, Retryable: retryable}
}
```

## `cmd/server/main.go`

```diff
--- a/cmd/server/main.go
+++ b/cmd/server/main.go
@@ -96,7 +96,7 @@
    agentStore := agent.NewPostgresStore(pool)
    agentService := agent.NewService(agentStore, promptService, modelService)

-	gateway := llm.NewFailoverGateway(modelService, llm.NewOpenAIClient())
+	gateway := llm.NewFailoverGateway(modelService, llm.NewEinoOpenAIClient())
    executor := engine.NewExecutor(promptService, gateway)

    conversationStore := conversation.NewPostgresStore(pool)
```

## 依赖与下一步

在 AsterFlow 根目录运行：

```bash
env -u GOROOT GOTOOLCHAIN=go1.26.6 go get github.com/cloudwego/eino-ext/components/model/openai@v0.1.13 github.com/meguminnnnnnnnn/go-openai@v0.1.2
env -u GOROOT GOTOOLCHAIN=go1.26.6 go mod tidy
```

学员已保存，助手将模型 HTTP 回归写入现有测试，正式 race 通过。下一步 [ADK 执行固定版本工具](G07-adk-tool-loop.md)。工具事件与历史持久化保留到同一组后续。
