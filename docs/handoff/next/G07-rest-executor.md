# G07 REST 执行器与出站 HTTP 客户端

固定 Final 的 REST 执行器读取已保存的 URL、method 和鉴权头，将工具参数作为 JSON 请求体发送。AsterFlow 沿用相同结构与支持的鉴权头格式、GET/DELETE 无请求体、同源重定向及 1 MiB 响应上限。出网策略对非例外主机直接连接已校验的 IP。REST 执行器将非 2xx 记为失败，错误中保留状态码。

学员已保存。本批正式工作区通过 race，使用 AsterFlow 仓库中的独立 Crossborder 进程和一次性 PostgreSQL 验证 get_order 命中种子记录及不存在订单返回 404。本地 HTTP 夹具验证鉴权头、同源重定向、跨源拦截、私网限制、取消与响应体大小。本批完整代码见下文。正式测试已保存在 internal/runtime/tooling/rest_test.go。

## 拨号所需的 IP

在 `internal/platform/tool/endpoint_policy.go` 替换 `validateHost` 并在 `unsafeIP` 前新增 `lookupPublicIPs`。

```go
func (p *EndpointPolicy) validateHost(ctx context.Context, host string) error {
	if p == nil || p.allowPrivate || p.hostAllowed(host) {
		return nil
	}
	_, err := p.lookupPublicIPs(ctx, host)
	return err
}

func (p *EndpointPolicy) lookupPublicIPs(ctx context.Context, host string) ([]net.IPAddr, error) {
	var addrs []net.IPAddr
	if ip := net.ParseIP(host); ip != nil {
		addrs = []net.IPAddr{{IP: ip}}
	} else {
		var err error
		addrs, err = p.resolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("resolve tool endpoint host: %w", err)
		}
	}

	if len(addrs) == 0 {
		return nil, fmt.Errorf("tool endpoint host has no IP addresses")
	}
	for _, addr := range addrs {
		if unsafeIP(addr.IP) {
			return nil, fmt.Errorf("tool endpoint resolves to a private or local network address")
		}
	}
	return addrs, nil
}
```

## 受控 HTTP Client

新建 `internal/platform/tool/http_client.go`。

```go
package tool

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (p *EndpointPolicy) HTTPClient(timeout time.Duration) (*http.Client, error) {
	if p == nil {
		return nil, fmt.Errorf("tool endpoint policy is not configured")
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		if p.allowPrivate || p.hostAllowed(host) {
			return dialer.DialContext(ctx, network, address)
		}

		// 连接刚完成校验的 IP，避免校验和实际连接分别查 DNS。
		addrs, err := p.lookupPublicIPs(ctx, host)
		if err != nil {
			return nil, err
		}
		var lastErr error
		for _, addr := range addrs {
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(addr.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		return nil, lastErr
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("tool endpoint stopped after 5 redirects")
			}
			// 鉴权请求只跟随同源跳转。
			if len(via) > 0 && !sameOrigin(via[0].URL, req.URL) {
				return fmt.Errorf("tool endpoint cross-origin redirect is blocked")
			}
			return p.ValidateURL(req.Context(), req.URL.String())
		},
	}, nil
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) &&
		strings.EqualFold(strings.TrimSuffix(a.Hostname(), "."), strings.TrimSuffix(b.Hostname(), ".")) &&
		effectivePort(a) == effectivePort(b)
}

func effectivePort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	if strings.EqualFold(u.Scheme, "https") {
		return "443"
	}
	return "80"
}
```

## REST 执行器

新建 `internal/runtime/tooling/rest.go`。

```go
package tooling

import (
	"bytes"
	"context"
	"encoding/json"
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
		return "", fmt.Errorf("call rest tool: %w", err)
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
```

学员运行 go build ./...。下一步将执行前加载的配置与 Executor 组装到 Registry，接参数 Schema 校验和试跑入口。试跑使用同一 VersionID 执行和记账，敏感工具依交付路线限制，发布由近期成功记录门禁控制。
