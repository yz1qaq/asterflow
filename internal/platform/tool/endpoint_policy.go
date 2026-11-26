package tool

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// 可向工具调用方公开的固定策略错误。
var (
	ErrToolPrivateEndpoint     = errors.New("tool endpoint resolves to a private or local network address")
	ErrToolCrossOriginRedirect = errors.New("tool endpoint cross-origin redirect is blocked")
	ErrToolRedirectLimit       = errors.New("tool endpoint stopped after 5 redirects")
)

// EndpointPolicy 控制 Tool 可以访问的网络目标。显式 allowlist 可放行课堂 Compose 服务。
type EndpointPolicy struct {
	allowedHosts map[string]struct{}
	allowPrivate bool
	resolver     *net.Resolver
}

func NewEndpointPolicy(allowedHosts []string, allowPrivate bool) *EndpointPolicy {
	p := &EndpointPolicy{allowedHosts: make(map[string]struct{}), allowPrivate: allowPrivate, resolver: net.DefaultResolver}
	// 配置中的主机名去掉首尾空白并统一小写，便于后续精确匹配。
	for _, host := range allowedHosts {
		if host = strings.ToLower(strings.TrimSpace(host)); host != "" {
			p.allowedHosts[host] = struct{}{}
		}
	}
	return p
}

// 判断是否允许这个host
func (p *EndpointPolicy) hostAllowed(host string) bool {
	_, ok := p.allowedHosts[strings.ToLower(strings.TrimSuffix(host, "."))]
	return ok
}

func parseToolEndpointURL(rawURL string) (*url.URL, error) {
	endpoint, err := url.Parse(rawURL)
	if err != nil || endpoint.Hostname() == "" {
		return nil, fmt.Errorf("invalid tool endpoint URL")
	}
	if endpoint.Scheme != "http" && endpoint.Scheme != "https" {
		return nil, fmt.Errorf("tool endpoint scheme must be http or https")
	}
	if endpoint.User != nil {
		return nil, fmt.Errorf("tool endpoint URL must not contain credentials")
	}
	return endpoint, nil
}

func parseConfiguredToolEndpointURL(rawURL string) (*url.URL, error) {
	endpoint, err := parseToolEndpointURL(rawURL)
	if err != nil {
		return nil, err
	}
	// 版本接口会显示端点配置，查询参数及片段不得进入这一配置。
	if endpoint.RawQuery != "" || endpoint.ForceQuery || strings.Contains(rawURL, "#") {
		return nil, fmt.Errorf("configured tool endpoint URL must not contain a query or fragment")
	}
	return endpoint, nil
}

// ValidateURL 校验运行时的 HTTP 目标，包括同源跳转的地址。
func (p *EndpointPolicy) ValidateURL(ctx context.Context, rawURL string) error {
	endpoint, err := parseToolEndpointURL(rawURL)
	if err != nil {
		return err
	}
	return p.validateHost(ctx, endpoint.Hostname())
}

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
			return nil, ErrToolPrivateEndpoint
		}
	}
	return addrs, nil
}

func unsafeIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast()
}

func (p *EndpointPolicy) validateConfig(ctx context.Context, sourceType string, endpoint map[string]any) error {
	return p.validateToolEndpoints(ctx, sourceType, endpoint, parseConfiguredToolEndpointURL)
}

func (p *EndpointPolicy) validateToolEndpoints(
	ctx context.Context, sourceType string, endpoint map[string]any,
	parseURL func(string) (*url.URL, error),
) error {
	if p == nil {
		return nil
	}
	// 按来源类型选择需要检查的端点字段。
	var fields []string
	switch sourceType {
	case "rest_api":
		fields = []string{"url"}
	case "mcp_server":
		transport, _ := endpoint["transport"].(string)
		// 未指定传输方式和 URL 时，按 MCP 默认的 stdio 处理。
		if transport == "" && strings.TrimSpace(stringValue(endpoint["url"])) == "" {
			transport = "stdio"
		}
		if transport == "stdio" {
			return fmt.Errorf("MCP stdio is disabled in app/worker; deploy an isolated connector runner and use streamable_http")
		}
		fields = []string{"url"}
	case "a2a":
		fields = []string{"card_url", "url"}
	default:
		return nil
	}
	// 逐个检查已填的地址。需要网络端点的来源至少要提供一个地址。
	seen := false
	for _, field := range fields {
		raw, _ := endpoint[field].(string)
		if raw == "" {
			continue
		}
		seen = true
		endpointURL, err := parseURL(raw)
		if err != nil {
			return fmt.Errorf("%s: %w", field, err)
		}
		if err := p.validateHost(ctx, endpointURL.Hostname()); err != nil {
			return fmt.Errorf("%s: %w", field, err)
		}
	}
	if !seen {
		return fmt.Errorf("tool endpoint URL is required")
	}
	return nil
}

func stringValue(value any) string {
	result, _ := value.(string)
	return result
}
