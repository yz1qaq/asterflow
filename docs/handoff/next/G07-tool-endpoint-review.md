# G07 工具端点的重复字段和试跑错误

状态：实现和回归已写入正式 Go 工作区，本地完整验收通过。G07 PR #19 待新提交 CI 与独立复审。

## 与现有链路的关系

- 固定 Final 的工具平台管理版本配置，REST 执行器读取配置发出请求，试跑服务记录执行结果。AsterFlow 保留这条职责链，在配置写入、版本读取和 REST 请求失败三个边界收紧输出。
- `CreateTool` 和 `CreateToolVersion` 先解析完整配置，再逐个检查原始端点对象顶层的 `url` 和 `card_url`。同名地址字段产生 `ErrInvalidArgument`，Gin 返回 400。追加时先合并继承值再检查。
- `publicToolEndpointConfig` 复用检查。对含重复地址字段的历史版本，版本请求返回 `{}` 和 `endpoint_config_redacted: true`。执行配置继续按原解析规则加载固定版本。
- `http.Client.Do` 返回错误时，REST 执行器只公开已知策略错误和取消状态，其余返回 `call rest tool: upstream request failed`。试跑保存这条错误。

## 本批代码差异

```diff
--- a/internal/platform/tool/config.go
+++ b/internal/platform/tool/config.go
@@ -4,6 +4,7 @@
     "encoding/json"
     "fmt"
     "regexp"
+    "strings"
 )

 type CreateToolRequest struct {
@@ -58,6 +59,43 @@
     }, nil
 }

+// validateToolEndpointKeys 检查已解析为对象的端点配置中，公开地址字段是否唯一。
+func validateToolEndpointKeys(raw string) error {
+    if raw == "" {
+        return nil
+    }
+    decoder := json.NewDecoder(strings.NewReader(raw))
+    if _, err := decoder.Token(); err != nil {
+        return fmt.Errorf("invalid endpoint_config: %w", err)
+    }
+
+    // 逐个读顶层键，保留 map 解码时会被合并的同名字段。
+    seen := make(map[string]struct{}, 2)
+    for decoder.More() {
+        token, err := decoder.Token()
+        if err != nil {
+            return fmt.Errorf("invalid endpoint_config: %w", err)
+        }
+        key, ok := token.(string)
+        if !ok {
+            return fmt.Errorf("invalid endpoint_config: expected a field name")
+        }
+        if key == "url" || key == "card_url" {
+            if _, exists := seen[key]; exists {
+                return fmt.Errorf("endpoint_config has duplicate %s field", key)
+            }
+            seen[key] = struct{}{}
+        }
+
+        // 跳过当前字段的整个值，下一次读取的仍是顶层键。
+        var ignored json.RawMessage
+        if err := decoder.Decode(&ignored); err != nil {
+            return fmt.Errorf("invalid endpoint_config: %w", err)
+        }
+    }
+    return nil
+}
+
 func parseJSONObject(field, value string) (map[string]any, error) {
     if value == "" {
         return map[string]any{}, nil
--- a/internal/platform/tool/service.go
+++ b/internal/platform/tool/service.go
@@ -103,6 +103,9 @@
         req.SourceType, req.SchemaJSON, req.EndpointConfig, req.AuthConfig,
     )
     if err != nil {
+        return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
+    }
+    if err := validateToolEndpointKeys(req.EndpointConfig); err != nil {
         return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
     }
     if s.policy == nil {
@@ -190,6 +193,9 @@
     if _, err := parseJSONObject("retry_policy", req.RetryPolicy); err != nil {
         return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
     }
+    if err := validateToolEndpointKeys(req.EndpointConfig); err != nil {
+        return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
+    }
     if s.policy == nil {
         return nil, errors.New("tool endpoint policy is not configured")
     }
@@ -244,7 +250,7 @@

 func publicToolEndpointConfig(raw string) (string, bool) {
     config, err := parseJSONObject("endpoint_config", raw)
-    if err != nil {
+    if err != nil || validateToolEndpointKeys(raw) != nil {
         return "{}", true
     }
     // 隐藏整份不合规的旧配置，避免把删减后的地址当作原配置继续编辑。
--- a/internal/platform/tool/endpoint_policy.go
+++ b/internal/platform/tool/endpoint_policy.go
@@ -2,10 +2,18 @@

 import (
     "context"
+    "errors"
     "fmt"
     "net"
     "net/url"
     "strings"
+)
+
+// 可向工具调用方公开的固定策略错误。
+var (
+    ErrToolPrivateEndpoint     = errors.New("tool endpoint resolves to a private or local network address")
+    ErrToolCrossOriginRedirect = errors.New("tool endpoint cross-origin redirect is blocked")
+    ErrToolRedirectLimit       = errors.New("tool endpoint stopped after 5 redirects")
 )

 // EndpointPolicy 控制 Tool 可以访问的网络目标。显式 allowlist 可放行课堂 Compose 服务。
@@ -92,7 +100,7 @@
     }
     for _, addr := range addrs {
         if unsafeIP(addr.IP) {
-            return nil, fmt.Errorf("tool endpoint resolves to a private or local network address")
+            return nil, ErrToolPrivateEndpoint
         }
     }
     return addrs, nil
--- a/internal/platform/tool/http_client.go
+++ b/internal/platform/tool/http_client.go
@@ -46,11 +46,11 @@
         Transport: transport,
         CheckRedirect: func(req *http.Request, via []*http.Request) error {
             if len(via) >= 5 {
-                return fmt.Errorf("tool endpoint stopped after 5 redirects")
+                return ErrToolRedirectLimit
             }
             // 鉴权请求只跟随同源跳转。
             if len(via) > 0 && !sameOrigin(via[0].URL, req.URL) {
-                return fmt.Errorf("tool endpoint cross-origin redirect is blocked")
+                return ErrToolCrossOriginRedirect
             }
             return p.ValidateURL(req.Context(), req.URL.String())
         },
--- a/internal/runtime/tooling/rest.go
+++ b/internal/runtime/tooling/rest.go
@@ -8,7 +8,6 @@
     "fmt"
     "io"
     "net/http"
-    "net/url"

     "github.com/yz1qaq/asterflow/internal/platform/tool"
 )
@@ -70,12 +69,19 @@

     resp, err := e.client.Do(req)
     if err != nil {
-        // http.Client 的外层错误附带完整 URL，试跑只返回底层错误。
-        var requestErr *url.Error
-        if errors.As(err, &requestErr) {
-            err = requestErr.Err
+        // 只公开固定的策略错误和取消状态，避免记录上游诊断中的地址。
+        for _, publicErr := range []error{
+            context.Canceled,
+            context.DeadlineExceeded,
+            tool.ErrToolPrivateEndpoint,
+            tool.ErrToolCrossOriginRedirect,
+            tool.ErrToolRedirectLimit,
+        } {
+            if errors.Is(err, publicErr) {
+                return "", fmt.Errorf("call rest tool: %w", publicErr)
+            }
         }
-        return "", fmt.Errorf("call rest tool: %w", err)
+        return "", errors.New("call rest tool: upstream request failed")
     }
     defer resp.Body.Close()

```

## 验证

新增回归先在原实现上失败。正式工作区在隔离 PostgreSQL 和 Redis 上通过 `make check`，验证登记和追加返回 400、Viewer 的旧配置隐藏、旧版本仍按原地址试跑、网络错误文案、跳转策略和 Eino 零值。桌面 Chrome 使用正式后端和独立 Crossborder，确认隐藏旧端点、历史版本试跑成功、新版本必须显式填写端点和 v2 保存后 v1 仍隐藏。

回归已合入现有 `security_test.go`、`rest_test.go` 和 `tool_test.go`。参见[验收记录](../../delivery/G07-VERIFICATION.md)。
