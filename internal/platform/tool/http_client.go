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
