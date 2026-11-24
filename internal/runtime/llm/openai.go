package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// OpenAIClient calls OpenAI-compatible chat completions endpoints.
type OpenAIClient struct {
	http *http.Client
}

func NewOpenAIClient() *OpenAIClient {
	return &OpenAIClient{http: &http.Client{}}
}

// NewOpenAIProbeClient sends one request to the configured endpoint.
func NewOpenAIProbeClient() *OpenAIClient {
	return &OpenAIClient{http: &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIRequest struct {
	Model         string          `json:"model"`
	Messages      []openAIMessage `json:"messages"`
	Temperature   *float32        `json:"temperature,omitempty"`
	TopP          *float32        `json:"top_p,omitempty"`
	MaxTokens     *int            `json:"max_tokens,omitempty"`
	Stop          []string        `json:"stop,omitempty"`
	Seed          *int            `json:"seed,omitempty"`
	Stream        bool            `json:"stream"`
	StreamOptions *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options,omitempty"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type openAIResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage *openAIUsage `json:"usage"`
}

type openAIStreamChunk struct {
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *openAIUsage `json:"usage"`
}

// 拼接url
func (c *OpenAIClient) endpoint(dep ResolvedDeployment) string {
	return strings.TrimRight(dep.BaseURL, "/") + "/chat/completions"
}

// 创建一个http请求
func (c *OpenAIClient) newRequest(ctx context.Context, dep ResolvedDeployment, req GenerateRequest, stream bool) (*http.Request, error) {
	messages := make([]openAIMessage, 0, len(req.Messages)+1)
	if strings.TrimSpace(req.SystemPrompt) != "" {
		messages = append(messages, openAIMessage{Role: "system", Content: req.SystemPrompt})
	}
	for _, message := range req.Messages {
		messages = append(messages, openAIMessage{Role: message.Role, Content: message.Content})
	}
	body := openAIRequest{
		Model: dep.Model, Messages: messages,
		Temperature: req.Temperature, TopP: req.TopP, MaxTokens: req.MaxOutputTokens,
		Stop: req.Stop, Seed: req.Seed, Stream: stream,
	}
	if stream {
		body.StreamOptions = &struct {
			IncludeUsage bool `json:"include_usage"`
		}{IncludeUsage: true}
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(dep), bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if dep.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+dep.APIKey)
	}
	return httpReq, nil
}

func providerStatusError(status int, body []byte) error {
	err := fmt.Errorf("provider returned %d: %s", status, strings.TrimSpace(string(body)))
	retryable := status == http.StatusTooManyRequests || status >= 500
	return &ProviderError{StatusCode: status, Err: err, Retryable: retryable}
}

func (c *OpenAIClient) Generate(ctx context.Context, dep ResolvedDeployment, req GenerateRequest) (*GenerateResponse, error) {
	httpReq, err := c.newRequest(ctx, dep, req, false)
	if err != nil {
		return nil, &ProviderError{Err: err, Retryable: false}
	}
	//发起http请求
	res, err := c.http.Do(httpReq)
	if err != nil {
		return nil, &ProviderError{Err: err, Retryable: true}
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return nil, &ProviderError{Err: err, Retryable: true}
	}
	if res.StatusCode/100 != 2 {
		return nil, providerStatusError(res.StatusCode, raw)
	}
	var parsed openAIResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, &ProviderError{Err: fmt.Errorf("decode provider response: %w", err), Retryable: false}
	}
	if len(parsed.Choices) == 0 {
		return nil, &ProviderError{Err: fmt.Errorf("provider returned no choices"), Retryable: false}
	}
	response := &GenerateResponse{
		Content: parsed.Choices[0].Message.Content, Model: parsed.Model, DeploymentID: dep.ID,
	}
	if parsed.Usage != nil {
		response.Usage = Usage{
			PromptTokens: parsed.Usage.PromptTokens, CompletionTokens: parsed.Usage.CompletionTokens,
			TotalTokens: parsed.Usage.TotalTokens,
		}
	}
	return response, nil
}

func (c *OpenAIClient) Stream(ctx context.Context, dep ResolvedDeployment, req GenerateRequest) (Stream, error) {
	httpReq, err := c.newRequest(ctx, dep, req, true)
	if err != nil {
		return nil, &ProviderError{Err: err, Retryable: false}
	}
	res, err := c.http.Do(httpReq)
	if err != nil {
		return nil, &ProviderError{Err: err, Retryable: true}
	}
	if res.StatusCode/100 != 2 {
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		res.Body.Close()
		return nil, providerStatusError(res.StatusCode, raw)
	}
	return &openAIStream{body: res.Body, scanner: bufio.NewScanner(res.Body), deploymentID: dep.ID}, nil
}

type openAIStream struct {
	body         io.ReadCloser
	scanner      *bufio.Scanner
	deploymentID string
	model        string
	usage        Usage
	finished     bool
}

func (s *openAIStream) Recv(ctx context.Context) (*StreamChunk, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for s.scanner.Scan() {
		line := strings.TrimSpace(s.scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			return &StreamChunk{Done: true, Model: s.model, DeploymentID: s.deploymentID, Usage: s.usage}, nil
		}
		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return nil, &ProviderError{Err: fmt.Errorf("decode stream chunk: %w", err), Retryable: false}
		}
		if chunk.Model != "" {
			s.model = chunk.Model
		}
		if chunk.Usage != nil {
			s.usage = Usage{PromptTokens: chunk.Usage.PromptTokens, CompletionTokens: chunk.Usage.CompletionTokens, TotalTokens: chunk.Usage.TotalTokens}
		}
		if len(chunk.Choices) > 0 {
			choice := chunk.Choices[0]
			if choice.FinishReason != nil && *choice.FinishReason != "" {
				s.finished = true
			}
			if choice.Delta.Content != "" {
				return &StreamChunk{Content: choice.Delta.Content}, nil
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.scanner.Err(); err != nil {
		return nil, &ProviderError{Err: err, Retryable: true}
	}
	if !s.finished {
		return nil, &ProviderError{Err: io.ErrUnexpectedEOF, Retryable: true}
	}
	return &StreamChunk{Done: true, Model: s.model, DeploymentID: s.deploymentID, Usage: s.usage}, nil
}

func (s *openAIStream) Close() error { return s.body.Close() }
