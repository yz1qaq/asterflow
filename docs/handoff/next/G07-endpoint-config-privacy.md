# G07 PR #19 端点配置回显修复

工具的 AuthConfig 已加密，EndpointConfig 会作为版本详情返回。P2 修复把写入、公开读取与固定版本执行的规则分开。注册与追加时要求配置地址没有 URL 用户信息、查询参数或片段。旧配置继续使用既有网络策略运行，公开响应将不符合规则的 URL 配置整份隐藏，附加 endpoint_config_redacted 字段。试跑的 HTTP 客户端错误去掉包含完整 URL 的外层包装，保留可识别的底层错误。

页面展开旧版本时显示配置已隐藏。追加表单不预填旧端点，要求明确输入新配置。普通 URL 以及同源跳转沿用现有行为。现有 PostgreSQL schema 和已保存快照无需迁移。本次使用普通查询参数的隔离数据验证，未使用真实凭据。

旧实现下回归失败。正式工作区通过前端测试及构建，临时 Go 覆盖通过 Tool、Tooling、HTTP、迁移 race。在隔离桌面与真实 PostgreSQL 验证旧端点字段隐藏、旧版本仍能试跑查询、带查询参数的新端点被拒绝、表单重新填写后成功保存 v2。已按负责人授权写入生产 Go 并合入两处回归。隔离 PostgreSQL 的 `make check` 与从正式源码启动的桌面复验通过。

## `internal/platform/tool/endpoint_policy.go`

```diff
--- a/internal/platform/tool/endpoint_policy.go
+++ b/internal/platform/tool/endpoint_policy.go
@@ -32,24 +32,39 @@
    return ok
 }

-func (p *EndpointPolicy) validateURL(ctx context.Context, rawURL string) error {
-   u, err := url.Parse(rawURL)
-   if err != nil || u.Hostname() == "" {
-       return fmt.Errorf("invalid tool endpoint URL")
+func parseToolEndpointURL(rawURL string) (*url.URL, error) {
+   endpoint, err := url.Parse(rawURL)
+   if err != nil || endpoint.Hostname() == "" {
+       return nil, fmt.Errorf("invalid tool endpoint URL")
    }
-   if u.Scheme != "http" && u.Scheme != "https" {
-       return fmt.Errorf("tool endpoint scheme must be http or https")
+   if endpoint.Scheme != "http" && endpoint.Scheme != "https" {
+       return nil, fmt.Errorf("tool endpoint scheme must be http or https")
    }
-   // URL 只描述请求目标，凭据由 auth_config 单独处理。
-   if u.User != nil {
-       return fmt.Errorf("tool endpoint URL must not contain credentials")
+   if endpoint.User != nil {
+       return nil, fmt.Errorf("tool endpoint URL must not contain credentials")
    }
-   return p.validateHost(ctx, u.Hostname())
+   return endpoint, nil
 }

-// ValidateURL 校验可配置的服务端 HTTP 出口地址。
+func parseConfiguredToolEndpointURL(rawURL string) (*url.URL, error) {
+   endpoint, err := parseToolEndpointURL(rawURL)
+   if err != nil {
+       return nil, err
+   }
+   // 版本接口会显示端点配置，查询参数及片段不得进入这一配置。
+   if endpoint.RawQuery != "" || endpoint.ForceQuery || strings.Contains(rawURL, "#") {
+       return nil, fmt.Errorf("configured tool endpoint URL must not contain a query or fragment")
+   }
+   return endpoint, nil
+}
+
+// ValidateURL 校验运行时的 HTTP 目标，包括同源跳转的地址。
 func (p *EndpointPolicy) ValidateURL(ctx context.Context, rawURL string) error {
-   return p.validateURL(ctx, rawURL)
+   endpoint, err := parseToolEndpointURL(rawURL)
+   if err != nil {
+       return err
+   }
+   return p.validateHost(ctx, endpoint.Hostname())
 }

 func (p *EndpointPolicy) validateHost(ctx context.Context, host string) error {
@@ -89,6 +104,13 @@
 }

 func (p *EndpointPolicy) validateConfig(ctx context.Context, sourceType string, endpoint map[string]any) error {
+   return p.validateToolEndpoints(ctx, sourceType, endpoint, parseConfiguredToolEndpointURL)
+}
+
+func (p *EndpointPolicy) validateToolEndpoints(
+   ctx context.Context, sourceType string, endpoint map[string]any,
+   parseURL func(string) (*url.URL, error),
+) error {
    if p == nil {
        return nil
    }
@@ -120,7 +142,11 @@
            continue
        }
        seen = true
-       if err := p.validateURL(ctx, raw); err != nil {
+       endpointURL, err := parseURL(raw)
+       if err != nil {
+           return fmt.Errorf("%s: %w", field, err)
+       }
+       if err := p.validateHost(ctx, endpointURL.Hostname()); err != nil {
            return fmt.Errorf("%s: %w", field, err)
        }
    }
```

## `internal/domain/tool.go`

```diff
--- a/internal/domain/tool.go
+++ b/internal/domain/tool.go
@@ -21,18 +21,19 @@
 }

 type ToolVersion struct {
-   ID                  string    `json:"id"`
-   ToolID              string    `json:"tool_id"`
-   Version             int       `json:"version"`
-   SchemaJSON          string    `json:"schema_json"`
-   EndpointConfig      string    `json:"endpoint_config"`
-   AuthConfig          string    `json:"auth_config"`
-   AuthConfigEncrypted []byte    `json:"-"`
-   HasAuth             bool      `json:"has_auth"`
-   RetryPolicy         string    `json:"retry_policy"`
-   Status              string    `json:"status"`
-   CreatedBy           string    `json:"created_by"`
-   CreatedAt           time.Time `json:"created_at"`
+   ID                     string    `json:"id"`
+   ToolID                 string    `json:"tool_id"`
+   Version                int       `json:"version"`
+   SchemaJSON             string    `json:"schema_json"`
+   EndpointConfig         string    `json:"endpoint_config"`
+   EndpointConfigRedacted bool      `json:"endpoint_config_redacted,omitempty"`
+   AuthConfig             string    `json:"auth_config"`
+   AuthConfigEncrypted    []byte    `json:"-"`
+   HasAuth                bool      `json:"has_auth"`
+   RetryPolicy            string    `json:"retry_policy"`
+   Status                 string    `json:"status"`
+   CreatedBy              string    `json:"created_by"`
+   CreatedAt              time.Time `json:"created_at"`
 }

 type ToolTestRun struct {
```

## `internal/platform/tool/service.go`

```diff
--- a/internal/platform/tool/service.go
+++ b/internal/platform/tool/service.go
@@ -79,10 +79,7 @@

    result := make([]*domain.ToolVersion, 0, len(versions))
    for _, version := range versions {
-       safeVersion := *version
-       safeVersion.AuthConfig = "{}"
-       safeVersion.AuthConfigEncrypted = nil
-       result = append(result, &safeVersion)
+       result = append(result, publicToolVersion(version))
    }
    return result, nil
 }
@@ -241,7 +238,30 @@
    safeVersion := *version
    safeVersion.AuthConfig = "{}"
    safeVersion.AuthConfigEncrypted = nil
+   safeVersion.EndpointConfig, safeVersion.EndpointConfigRedacted = publicToolEndpointConfig(version.EndpointConfig)
    return &safeVersion
+}
+
+func publicToolEndpointConfig(raw string) (string, bool) {
+   config, err := parseJSONObject("endpoint_config", raw)
+   if err != nil {
+       return "{}", true
+   }
+   // 隐藏整份不合规的旧配置，避免把删减后的地址当作原配置继续编辑。
+   for _, field := range []string{"url", "card_url"} {
+       value, exists := config[field]
+       if !exists || value == "" {
+           continue
+       }
+       endpoint, ok := value.(string)
+       if !ok {
+           return "{}", true
+       }
+       if _, err := parseConfiguredToolEndpointURL(endpoint); err != nil {
+           return "{}", true
+       }
+   }
+   return raw, false
 }

 func (s *Service) RecordTestRun(
```

## `internal/platform/tool/execution_config.go`

```diff
--- a/internal/platform/tool/execution_config.go
+++ b/internal/platform/tool/execution_config.go
@@ -64,7 +64,8 @@
    if s.policy == nil {
        return nil, errors.New("tool endpoint policy is not configured")
    }
-   if err := s.policy.validateConfig(ctx, cfg.SourceType, cfg.EndpointConfig); err != nil {
+   // 已固定的旧版本继续按原网络规则执行，写入新配置时才要求使用公开地址。
+   if err := s.policy.validateToolEndpoints(ctx, cfg.SourceType, cfg.EndpointConfig, parseToolEndpointURL); err != nil {
        return nil, err
    }

```

## `internal/runtime/tooling/rest.go`

```diff
--- a/internal/runtime/tooling/rest.go
+++ b/internal/runtime/tooling/rest.go
@@ -4,9 +4,11 @@
    "bytes"
    "context"
    "encoding/json"
+   "errors"
    "fmt"
    "io"
    "net/http"
+   "net/url"

    "github.com/yz1qaq/asterflow/internal/platform/tool"
 )
@@ -68,6 +70,11 @@

    resp, err := e.client.Do(req)
    if err != nil {
+       // http.Client 的外层错误附带完整 URL，试跑只返回底层错误。
+       var requestErr *url.Error
+       if errors.As(err, &requestErr) {
+           err = requestErr.Err
+       }
        return "", fmt.Errorf("call rest tool: %w", err)
    }
    defer resp.Body.Close()
```
