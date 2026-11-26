# G07 试跑 HTTP API

固定 Final 的试跑执行并保存结果，返回 200 和 ToolTestRun。AsterFlow 沿用相同 API 路径和输入形状，使用 Gin。读取工具配置时确认空间，Registry 拦截敏感工具，参数校验失败记为 error。成功与失败记录均使用 Build 得到的 VersionID，确保执行期间有新草稿时仍对应被试跑的版本。REST 换源时要求显式配置或清空鉴权，防止试跑发送从旧源继承的凭据。

前一批 Registry 和 Schema 已在正式工作区通过隔离库及真实 Crossborder 回归。学员已保存本批 Go 代码，正式工作区通过编译和隔离 PostgreSQL/Crossborder HTTP race。覆盖身份与空间、敏感拦截、参数无效并未实际执行、试跑途中追加 v2 仍记到 v1、REST 换源限制、真实订单返回与写入试跑记录。

## REST 凭据继承

```diff
--- internal/platform/tool/service.go
+++ internal/platform/tool/service.go
@@ -4,6 +4,7 @@
    "context"
    "errors"
    "fmt"
+	"net/url"
    "strings"
    "time"

@@ -166,7 +167,8 @@
    if req.EndpointConfig == "" {
        req.EndpointConfig = current.EndpointConfig
    }
-	if req.AuthConfig == "" {
+	inheritAuth := req.AuthConfig == ""
+	if inheritAuth {
        req.AuthConfig, err = s.authPlain(current)
        if err != nil {
            return nil, err
@@ -189,6 +191,18 @@
    }
    if err := s.policy.validateConfig(ctx, cfg.SourceType, cfg.EndpointConfig); err != nil {
        return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
+	}
+	// 沿用旧凭据时，REST 目标必须保持同源；换目标时需显式配置。
+	if inheritAuth && item.SourceType == "rest_api" && len(cfg.AuthConfig) > 0 {
+		oldEndpoint, err := parseJSONObject("endpoint_config", current.EndpointConfig)
+		if err != nil {
+			return nil, fmt.Errorf("read previous tool endpoint: %w", err)
+		}
+		oldURL, oldErr := url.Parse(stringValue(oldEndpoint["url"]))
+		newURL, newErr := url.Parse(stringValue(cfg.EndpointConfig["url"]))
+		if oldErr != nil || newErr != nil || !sameOrigin(oldURL, newURL) {
+			return nil, fmt.Errorf("%w: endpoint origin changed; provide auth_config or clear it with {}", ErrInvalidArgument)
+		}
    }
    if len(cfg.AuthConfig) == 0 {
        req.AuthConfig = "{}"
```

## Handler

```diff
--- internal/api/v1/tool.go
+++ internal/api/v1/tool.go
@@ -1,20 +1,24 @@
 package v1

 import (
+	"encoding/json"
    "errors"
    "net/http"
+	"time"

    "github.com/gin-gonic/gin"
    "github.com/yz1qaq/asterflow/internal/api/middleware"
    "github.com/yz1qaq/asterflow/internal/platform/tool"
+	"github.com/yz1qaq/asterflow/internal/runtime/tooling"
 )

 type ToolHandler struct {
-	service *tool.Service
+	service  *tool.Service
+	registry *tooling.Registry
 }

-func NewToolHandler(service *tool.Service) *ToolHandler {
-	return &ToolHandler{service: service}
+func NewToolHandler(service *tool.Service, registry *tooling.Registry) *ToolHandler {
+	return &ToolHandler{service: service, registry: registry}
 }

 func (h *ToolHandler) ListTools(c *gin.Context) {
@@ -100,3 +104,49 @@
        c.JSON(http.StatusCreated, version)
    }
 }
+
+func (h *ToolHandler) TestTool(c *gin.Context) {
+	var body struct {
+		Input json.RawMessage `json:"input"`
+	}
+	if err := decodeJSON(c.Writer, c.Request, &body); err != nil || len(body.Input) == 0 {
+		c.JSON(http.StatusBadRequest, gin.H{"error": "valid input is required"})
+		return
+	}
+
+	ctx := c.Request.Context()
+	workspaceID := middleware.GetWorkspaceIDFromContext(ctx)
+	// 只在执行前选一次版本，结果用这次选中的版本 ID 保存。
+	target, err := h.service.GetToolConfigForTest(ctx, workspaceID, c.Param("tool_id"))
+	if errors.Is(err, tool.ErrToolNotFound) {
+		c.JSON(http.StatusNotFound, gin.H{"error": "tool not found"})
+		return
+	}
+	if err != nil {
+		c.JSON(http.StatusInternalServerError, gin.H{"error": "tool configuration could not be loaded"})
+		return
+	}
+	built, err := h.registry.Build(target)
+	if errors.Is(err, tooling.ErrSensitiveToolRequiresApproval) {
+		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
+		return
+	}
+	if err != nil {
+		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
+		return
+	}
+
+	start := time.Now()
+	output, runErr := built.Executor.Execute(ctx, body.Input)
+	latencyMs := int(time.Since(start).Milliseconds())
+
+	// 即使执行失败，也保存一次试跑；数据库写失败才返回 HTTP 500。
+	run, err := h.service.RecordTestRun(
+		ctx, workspaceID, built.ToolID, built.VersionID, string(body.Input), output, latencyMs, runErr,
+	)
+	if err != nil {
+		c.JSON(http.StatusInternalServerError, gin.H{"error": "tool test run could not be recorded"})
+		return
+	}
+	c.JSON(http.StatusOK, run)
+}
```

## 路由

```diff
--- internal/api/router.go
+++ internal/api/router.go
@@ -14,6 +14,7 @@
    "github.com/yz1qaq/asterflow/internal/platform/modelconfig"
    "github.com/yz1qaq/asterflow/internal/platform/prompt"
    "github.com/yz1qaq/asterflow/internal/platform/tool"
+	"github.com/yz1qaq/asterflow/internal/runtime/tooling"
 )

 type ReadinessChecker interface {
@@ -24,6 +25,7 @@
    Agent        *agent.Service
    Conversation *conversation.Service
    Tool         *tool.Service
+	ToolRegistry *tooling.Registry
 }

 func NewRouter(
@@ -184,11 +186,14 @@
        services = extra[0]
    }
    if services.Tool != nil {
-		toolHandler := v1.NewToolHandler(services.Tool)
+		toolHandler := v1.NewToolHandler(services.Tool, services.ToolRegistry)
        workspaceRoutes.GET("/tools", toolHandler.ListTools)
        workspaceRoutes.GET("/tools/:tool_id/versions", toolHandler.ListToolVersions)
        workspaceRoutes.POST("/tools", toolHandler.CreateTool)
        workspaceRoutes.POST("/tools/:tool_id/versions", toolHandler.CreateToolVersion)
+		if services.ToolRegistry != nil {
+			workspaceRoutes.POST("/tools/:tool_id/test", toolHandler.TestTool)
+		}
    }
    if services.Agent != nil {
        agentHandler := v1.NewAgentHandler(services.Agent)
```

## 启动装配

```diff
--- cmd/server/main.go
+++ cmd/server/main.go
@@ -23,6 +23,7 @@
    "github.com/yz1qaq/asterflow/internal/runtime/engine"
    "github.com/yz1qaq/asterflow/internal/runtime/llm"
    "github.com/yz1qaq/asterflow/internal/runtime/promptcache"
+	"github.com/yz1qaq/asterflow/internal/runtime/tooling"
 )

 //程序的总入口,初始化组件、连接数据库、启动http服务
@@ -108,10 +109,16 @@
    endpointPolicy := tool.NewEndpointPolicy(strings.Split(cfg.ToolAllowedHosts, ","), false)
    toolService.ConfigureSecurity(credentialCipher, endpointPolicy)

+	toolRegistry, err := tooling.NewRegistry(endpointPolicy)
+	if err != nil {
+		return fmt.Errorf("initialize tool registry: %w", err)
+	}
+
    handler := api.NewRouter(pool, iamService, modelService, promptService, api.Services{
        Agent:        agentService,
        Conversation: conversationService,
        Tool:         toolService,
+		ToolRegistry: toolRegistry,
    })

    if cfg.AutoseedAdmin {
```

编译用 go build ./...。助手已扩展现有 internal/api/tool_test.go 并复跑，React 试跑在桌面 Chrome 联调通过。后续发布仍按具体版本的近期成功记录判断。
