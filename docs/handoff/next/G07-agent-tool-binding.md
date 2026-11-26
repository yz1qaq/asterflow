# G07 Agent 快照绑定已发布工具版本

固定 Final 的独立实例通过创建 Agent 和直接查询其 agent_versions.snapshot_json 验证，提交 ToolID 后同时保存 ToolID 与具体的 VersionID。AsterFlow 保持这两层 ID、按当前工作空间校验，通过只查询 published 明确限制首次绑定，无已发布版本时拒绝。已保存快照保持原来的 VersionID，新建 Agent 版本时再解析当前最新已发布版本。

学员已保存。本批回归并入现有 Agent HTTP 测试。学员已补 writeAgentError 的 404 映射，正式隔离 PostgreSQL HTTP race 通过，覆盖草稿拒绝、跨空间、非法与重复 UUID、追加和旧快照回读。React 新建和追加版本已提供工具选择；桌面配合隔离电商与平台 PostgreSQL 验证只显示已发布工具、预填 ToolID，以及工具发布 v2 后两个 Agent 版本各自保留所绑定的版本。

## 完整改动

### `internal/infrastructure/postgres/queries/tool.sql`

```diff
--- internal/infrastructure/postgres/queries/tool.sql
+++ internal/infrastructure/postgres/queries/tool.sql
@@ -89,3 +89,11 @@
 WHERE tool_id = sqlc.arg(tool_id)
   AND id = sqlc.arg(version_id)
   AND status = 'draft';
+
+-- name: GetToolLatestPublishedVersionID :one
+SELECT id
+FROM tool_versions
+WHERE tool_id = $1
+  AND status = 'published'
+ORDER BY version DESC
+LIMIT 1;
```

### `internal/platform/tool/postgres_store.go`

```diff
--- internal/platform/tool/postgres_store.go
+++ internal/platform/tool/postgres_store.go
@@ -258,3 +258,19 @@
    }
    return nil
 }
+
+func (s *PostgresStore) GetToolLatestPublishedVersionID(ctx context.Context, toolID string) (string, error) {
+	id, err := uuid.Parse(toolID)
+	if err != nil {
+		return "", ErrToolNotFound
+	}
+
+	versionID, err := s.q.GetToolLatestPublishedVersionID(ctx, id)
+	if errors.Is(err, pgx.ErrNoRows) {
+		return "", ErrNoPublishedToolVersion
+	}
+	if err != nil {
+		return "", fmt.Errorf("get latest published tool version: %w", err)
+	}
+	return versionID.String(), nil
+}
```

### `internal/platform/tool/service.go`

```diff
--- internal/platform/tool/service.go
+++ internal/platform/tool/service.go
@@ -13,12 +13,13 @@
 )

 var (
-	ErrToolNotFound        = errors.New("tool not found")
-	ErrInvalidArgument     = errors.New("invalid tool argument")
-	ErrToolAlreadyExists   = errors.New("tool name already exists in workspace")
-	ErrNoSuccessfulTestRun = errors.New("no successful test run for tool version")
-	ErrToolVersionNotDraft = errors.New("tool version is not in draft status")
-	ErrToolTestRunExpired  = errors.New("successful tool test run is more than seven days old")
+	ErrToolNotFound           = errors.New("tool not found")
+	ErrInvalidArgument        = errors.New("invalid tool argument")
+	ErrToolAlreadyExists      = errors.New("tool name already exists in workspace")
+	ErrNoSuccessfulTestRun    = errors.New("no successful test run for tool version")
+	ErrToolVersionNotDraft    = errors.New("tool version is not in draft status")
+	ErrToolTestRunExpired     = errors.New("successful tool test run is more than seven days old")
+	ErrNoPublishedToolVersion = errors.New("tool has no published version")
 )

 type Store interface {
@@ -28,6 +29,7 @@
    ListToolVersions(ctx context.Context, toolID string) ([]*domain.ToolVersion, error)

    GetToolCurrentVersion(ctx context.Context, toolID string) (*domain.ToolVersion, error)
+	GetToolLatestPublishedVersionID(ctx context.Context, toolID string) (string, error)
    AppendToolVersion(ctx context.Context, workspaceID string, v *domain.ToolVersion) error
    GetToolVersion(ctx context.Context, versionID string) (*domain.ToolVersion, error)

@@ -304,3 +306,11 @@
    }
    return s.store.PublishToolVersion(ctx, item.ID, version.ID)
 }
+
+func (s *Service) ResolvePublishedToolVersionID(ctx context.Context, workspaceID, toolID string) (string, error) {
+	// 先校验工具归属，再解析它在当前时刻可绑定的版本。
+	if err := s.EnsureToolWorkspace(ctx, toolID, workspaceID); err != nil {
+		return "", err
+	}
+	return s.store.GetToolLatestPublishedVersionID(ctx, toolID)
+}
```

### `internal/domain/agent_snapshot.go`

```diff
--- internal/domain/agent_snapshot.go
+++ internal/domain/agent_snapshot.go
@@ -8,6 +8,8 @@
    UserPromptID          string           `json:"user_prompt_id,omitempty"`
    UserPromptVersionID   string           `json:"user_prompt_version_id,omitempty"`
    PromptEnv             string           `json:"prompt_env,omitempty"`
+	ToolIDs               []string         `json:"tool_ids,omitempty"`
+	ToolVersionIDs        []string         `json:"tool_version_ids,omitempty"`
    ModelProfileVersionID string           `json:"model_profile_version_id,omitempty"`
    GenerationConfig      GenerationConfig `json:"generation_config"`
 }
```

### `internal/platform/agent/service.go`

```diff
--- internal/platform/agent/service.go
+++ internal/platform/agent/service.go
@@ -22,6 +22,10 @@
 // ModelProfileVersionChecker verifies a pinned model profile version is visible in the workspace.
 type ModelProfileVersionChecker interface {
    ValidateProfileVersion(ctx context.Context, workspaceID, versionID string) error
+}
+
+type ToolVersionResolver interface {
+	ResolvePublishedToolVersionID(ctx context.Context, workspaceID, toolID string) (string, error)
 }

 type Store interface {
@@ -41,6 +45,7 @@
    store   Store
    prompts PromptVersionChecker
    models  ModelProfileVersionChecker
+	tools   ToolVersionResolver
 }

 func NewService(store Store, prompts PromptVersionChecker, models ModelProfileVersionChecker) *Service {
@@ -54,6 +59,7 @@
    UserPromptID          string                  `json:"user_prompt_id"`
    UserPromptVersionID   string                  `json:"user_prompt_version_id"`
    PromptEnv             string                  `json:"prompt_env"`
+	ToolIDs               []string                `json:"tool_ids"`
    ModelProfileVersionID string                  `json:"model_profile_version_id"`
    GenerationConfig      domain.GenerationConfig `json:"generation_config"`
 }
@@ -123,6 +129,11 @@
        }
    }

+	toolIDs, toolVersionIDs, err := s.resolveSnapshotTools(ctx, workspaceID, in.ToolIDs)
+	if err != nil {
+		return "", err
+	}
+
    encoded, err := json.Marshal(domain.AgentSnapshot{
        ID:                    agentID,
        SystemPrompt:          system,
@@ -131,6 +142,8 @@
        UserPromptID:          userID,
        UserPromptVersionID:   userVersion,
        PromptEnv:             env,
+		ToolIDs:               toolIDs,
+		ToolVersionIDs:        toolVersionIDs,
        ModelProfileVersionID: modelVersion,
        GenerationConfig:      in.GenerationConfig,
    })
@@ -299,3 +312,42 @@
    }
    return version, nil
 }
+
+func (s *Service) ConfigureToolResolver(tools ToolVersionResolver) {
+	s.tools = tools
+}
+
+func (s *Service) resolveSnapshotTools(
+	ctx context.Context, workspaceID string, requested []string,
+) ([]string, []string, error) {
+	if len(requested) == 0 {
+		return nil, nil, nil
+	}
+	if s.tools == nil {
+		return nil, nil, errors.New("tool version resolver is not configured")
+	}
+
+	toolIDs := make([]string, 0, len(requested))
+	versionIDs := make([]string, 0, len(requested))
+	seen := make(map[string]struct{}, len(requested))
+	for _, raw := range requested {
+		id, err := uuid.Parse(strings.TrimSpace(raw))
+		if err != nil {
+			return nil, nil, fmt.Errorf("%w: tool_ids must contain valid UUIDs", ErrInvalidArgument)
+		}
+		toolID := id.String()
+		if _, exists := seen[toolID]; exists {
+			continue
+		}
+		versionID, err := s.tools.ResolvePublishedToolVersionID(ctx, workspaceID, toolID)
+		if err != nil {
+			return nil, nil, fmt.Errorf("pin tool %s: %w", toolID, err)
+		}
+
+		// 两个切片同序保存。ToolID 用于编辑，VersionID 用于复现执行。
+		seen[toolID] = struct{}{}
+		toolIDs = append(toolIDs, toolID)
+		versionIDs = append(versionIDs, versionID)
+	}
+	return toolIDs, versionIDs, nil
+}
```

### `internal/api/v1/agent.go`

```diff
--- internal/api/v1/agent.go
+++ internal/api/v1/agent.go
@@ -9,6 +9,7 @@
    "github.com/yz1qaq/asterflow/internal/platform/agent"
    "github.com/yz1qaq/asterflow/internal/platform/modelconfig"
    "github.com/yz1qaq/asterflow/internal/platform/prompt"
+	"github.com/yz1qaq/asterflow/internal/platform/tool"
 )

 type AgentHandler struct {
@@ -21,10 +22,11 @@

 func writeAgentError(c *gin.Context, err error) {
    switch {
-	case errors.Is(err, agent.ErrInvalidArgument):
+	case errors.Is(err, agent.ErrInvalidArgument), errors.Is(err, tool.ErrNoPublishedToolVersion):
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
    case errors.Is(err, prompt.ErrPromptVersionNotFound),
        errors.Is(err, modelconfig.ErrProfileVersionNotFound),
+		errors.Is(err, tool.ErrToolNotFound),
        errors.Is(err, agent.ErrAgentNotFound),
        errors.Is(err, agent.ErrAgentVersionNotFound):
        c.JSON(http.StatusNotFound, gin.H{"error": "agent or referenced version not found"})
```

### `cmd/server/main.go`

```diff
--- cmd/server/main.go
+++ cmd/server/main.go
@@ -108,6 +108,7 @@
    // 只为显式指定的主机提供私网例外。
    endpointPolicy := tool.NewEndpointPolicy(strings.Split(cfg.ToolAllowedHosts, ","), false)
    toolService.ConfigureSecurity(credentialCipher, endpointPolicy)
+	agentService.ConfigureToolResolver(toolService)
    toolRegistry, err := tooling.NewRegistry(endpointPolicy)
    if err != nil {
        return fmt.Errorf("initialize tool registry: %w", err)
```

运行 make sqlc-generate、gofmt 和 go build ./...。后续的聊天运行时读取已固定的 ToolVersionIDs 构造 ADK 工具，聊天入口与版本绑定分开验收。
