# G07 追加工具版本的 Service 和 Gin

固定 Final 的工具详情页在「新建版本」时预填最新版本的配置，向 `POST /api/v1/tools/:tool_id/versions` 发送新配置。工作空间和创建者由鉴权上下文提供，Store 提交后写回版本号。空字符串沿用当前版本对应字段，鉴权明确给空对象则清空。成功返回 201 和脱敏的新草稿。

上一批 Store 已在正式工作区通过注册、迁移、HTTP 和隔离 PostgreSQL 并发 race 验收。学员已在正式工作区保存 Service 和 Gin。扩展后的 `internal/api/tool_test.go` 在隔离 PostgreSQL 通过 HTTP race，覆盖沿用凭据、清空凭据、旧版本保留、禁止跨空间和 Viewer 写入、拒绝伪造身份与非法 JSON。React 已接入，详见当前状态。以下保留本轮手写代码作为对照。

## 请求类型与 Store 接口

在 `internal/platform/tool/config.go` 的 `CreateToolRequest` 下加入。

```go
type CreateToolVersionRequest struct {
	SchemaJSON     string `json:"schema_json"`
	EndpointConfig string `json:"endpoint_config"`
	AuthConfig     string `json:"auth_config"`
	RetryPolicy    string `json:"retry_policy"`
	CreatedBy      string `json:"-"`
}
```

在 `internal/platform/tool/service.go` 的 `Store` 末尾追加。

```diff
    ListToolVersions(ctx context.Context, toolID string) ([]*domain.ToolVersion, error)
+	GetToolCurrentVersion(ctx context.Context, toolID string) (*domain.ToolVersion, error)
+	AppendToolVersion(ctx context.Context, workspaceID string, v *domain.ToolVersion) error
 }
```

## Service 实现与响应脱敏

在 `internal/platform/tool/security.go` 的 `prepareVersionAuth` 后追加。以后构建运行时执行器也能复用该解密函数。

```go
func (s *Service) authPlain(version *domain.ToolVersion) (string, error) {
	// 兼容没有密文的旧版本；新版本保存时仍会加密。
	if len(version.AuthConfigEncrypted) == 0 {
		return version.AuthConfig, nil
	}
	if s.cipher == nil {
		return "", fmt.Errorf("tool credential cipher is not configured")
	}
	plain, err := s.cipher.Decrypt(version.AuthConfigEncrypted)
	if err != nil {
		return "", fmt.Errorf("decrypt tool auth config: %w", err)
	}
	return plain, nil
}
```

在 `internal/platform/tool/service.go` 的 `CreateTool` 后追加新方法和脱敏函数。

```go
func (s *Service) CreateToolVersion(
	ctx context.Context, workspaceID, toolID string, req CreateToolVersionRequest,
) (*domain.ToolVersion, error) {
	// 先确认工具归属，再读取它的最新版本和凭据。
	item, err := s.store.GetTool(ctx, toolID)
	if err != nil {
		return nil, err
	}
	if item.WorkspaceID != workspaceID {
		return nil, ErrToolNotFound
	}
	current, err := s.store.GetToolCurrentVersion(ctx, item.ID)
	if err != nil {
		return nil, err
	}

	// 空字符串表示沿用。显式传入 "{}" 可以清空鉴权配置。
	if req.SchemaJSON == "" {
		req.SchemaJSON = current.SchemaJSON
	}
	if req.EndpointConfig == "" {
		req.EndpointConfig = current.EndpointConfig
	}
	if req.AuthConfig == "" {
		req.AuthConfig, err = s.authPlain(current)
		if err != nil {
			return nil, err
		}
	}
	if req.RetryPolicy == "" {
		req.RetryPolicy = current.RetryPolicy
	}

	// 校验合并后的完整配置，继承的端点也要符合当前策略。
	cfg, err := toolConfigFromStrings(item.SourceType, req.SchemaJSON, req.EndpointConfig, req.AuthConfig)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	if _, err := parseJSONObject("retry_policy", req.RetryPolicy); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	if s.policy == nil {
		return nil, errors.New("tool endpoint policy is not configured")
	}
	if err := s.policy.validateConfig(ctx, cfg.SourceType, cfg.EndpointConfig); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	if len(cfg.AuthConfig) == 0 {
		req.AuthConfig = "{}"
	}

	version := &domain.ToolVersion{
		ID:             uuid.NewString(),
		ToolID:         item.ID,
		SchemaJSON:     req.SchemaJSON,
		EndpointConfig: req.EndpointConfig,
		AuthConfig:     req.AuthConfig,
		RetryPolicy:    req.RetryPolicy,
		CreatedBy:      req.CreatedBy,
	}

	// 凭据加密后再落库，Store 负责版本号、草稿状态与创建时间。
	if err := s.prepareVersionAuth(version); err != nil {
		return nil, err
	}
	if err := s.store.AppendToolVersion(ctx, workspaceID, version); err != nil {
		return nil, err
	}

	return publicToolVersion(version), nil
}

func publicToolVersion(version *domain.ToolVersion) *domain.ToolVersion {
	safeVersion := *version
	safeVersion.AuthConfig = "{}"
	safeVersion.AuthConfigEncrypted = nil
	return &safeVersion
}
```

将同一文件的 `ListToolVersions` 的循环改为复用脱敏函数。

```diff
    result := make([]*domain.ToolVersion, 0, len(versions))
    for _, version := range versions {
-		safeVersion := *version
-		safeVersion.AuthConfig = "{}"
-		safeVersion.AuthConfigEncrypted = nil
-		result = append(result, &safeVersion)
+		result = append(result, publicToolVersion(version))
    }
    return result, nil
```

## Gin Handler 与路由

在 `internal/api/v1/tool.go` 的 `CreateTool` 下追加。

```go
func (h *ToolHandler) CreateToolVersion(c *gin.Context) {
	var req tool.CreateToolVersionRequest
	if err := decodeJSON(c.Writer, c.Request, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	// 工具 ID 来自路由，空间和创建者来自鉴权上下文。
	ctx := c.Request.Context()
	req.CreatedBy = middleware.GetUserIDFromContext(ctx)
	workspaceID := middleware.GetWorkspaceIDFromContext(ctx)

	version, err := h.service.CreateToolVersion(ctx, workspaceID, c.Param("tool_id"), req)
	switch {
	case errors.Is(err, tool.ErrToolNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "tool not found"})
	case errors.Is(err, tool.ErrInvalidArgument):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "tool operation failed"})
	default:
		c.JSON(http.StatusCreated, version)
	}
}
```

在 `internal/api/router.go` 的工具路由块中追加 POST。

```diff
        workspaceRoutes.GET("/tools/:tool_id/versions", toolHandler.ListToolVersions)
+		workspaceRoutes.POST("/tools/:tool_id/versions", toolHandler.CreateToolVersion)
        workspaceRoutes.POST("/tools", toolHandler.CreateTool)
```

这里沿用 Final 的取最新配置、补齐、校验、解密并重新加密流程。追加时沿用 AsterFlow 的空间归属、事务取号和凭据校验。另将 `retry_policy` 统一校验为 JSON 对象。编译验收用 `go build ./cmd/server`。
