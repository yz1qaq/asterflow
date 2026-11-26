package tooling

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/yz1qaq/asterflow/internal/platform/tool"
)

type Executor interface {
	Execute(ctx context.Context, args json.RawMessage) (string, error)
}

const maxToolResponseBytes = 1 << 20

type restExecutor struct {
	client   *http.Client
	method   string
	endpoint string
	headers  http.Header
}

func NewRESTExecutor(client *http.Client, cfg *tool.ToolConfig) Executor {
	method, _ := cfg.EndpointConfig["method"].(string)
	if method == "" {
		method = http.MethodPost
	}
	endpoint, _ := cfg.EndpointConfig["url"].(string)

	headers := http.Header{"Content-Type": {"application/json"}}
	if header, ok := cfg.AuthConfig["header"].(string); ok && header != "" {
		if value, ok := cfg.AuthConfig["value"].(string); ok {
			headers.Set(header, value)
		}
	}
	if configured, ok := cfg.AuthConfig["headers"].(map[string]any); ok {
		for header, raw := range configured {
			if value, ok := raw.(string); ok && header != "" {
				headers.Set(header, value)
			}
		}
	}
	return &restExecutor{client: client, method: method, endpoint: endpoint, headers: headers}
}

func (e *restExecutor) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if e.endpoint == "" {
		return "", fmt.Errorf("rest tool: empty endpoint url")
	}
	if len(args) > 0 && !json.Valid(args) {
		return "", fmt.Errorf("rest tool input must be valid JSON")
	}

	// 让请求继承调用方的取消信号。GET 与 DELETE 沿用 Final 的无请求体规则。
	var body io.Reader
	if e.method != http.MethodGet && e.method != http.MethodDelete {
		body = bytes.NewReader(args)
	}
	req, err := http.NewRequestWithContext(ctx, e.method, e.endpoint, body)
	if err != nil {
		return "", fmt.Errorf("build rest tool request: %w", err)
	}
	req.Header = e.headers.Clone()

	resp, err := e.client.Do(req)
	if err != nil {
		// 只公开固定的策略错误和取消状态，避免记录上游诊断中的地址。
		for _, publicErr := range []error{
			context.Canceled,
			context.DeadlineExceeded,
			tool.ErrToolPrivateEndpoint,
			tool.ErrToolCrossOriginRedirect,
			tool.ErrToolRedirectLimit,
		} {
			if errors.Is(err, publicErr) {
				return "", fmt.Errorf("call rest tool: %w", publicErr)
			}
		}
		return "", errors.New("call rest tool: upstream request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("rest tool returned HTTP %d", resp.StatusCode)
	}
	data, err := readToolResponse(resp.Body)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func readToolResponse(r io.Reader) ([]byte, error) {
	// 多读一个字节，可以区分刚好达到上限和超出上限。
	data, err := io.ReadAll(io.LimitReader(r, maxToolResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read tool response: %w", err)
	}
	if len(data) > maxToolResponseBytes {
		return nil, fmt.Errorf("tool response exceeds %d bytes", maxToolResponseBytes)
	}
	return data, nil
}
