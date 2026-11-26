# G07 发布指定工具版本

固定 Final 的独立实例验证了 v2 试跑前被拒绝、同一 v2 成功试跑后发布、v1 状态保留。AsterFlow 保持指定版本、草稿状态和最近成功试跑不超过七天的门禁，由 PostgreSQL 条件 UPDATE 保证并发请求只有一个能发布。HTTP 映射为错配或跨空间 404、缺少或过期的成功试跑 400、状态冲突 409，成功返回 200。

学员已保存发布代码，正式工作区通过服务端编译和隔离 PostgreSQL HTTP race，覆盖版本独立门禁、成功记录过期、失败记录不覆盖之前的成功、Viewer、跨空间、版本错配、并发发布和最终数据库状态。

## SQL 与 Store

在 internal/infrastructure/postgres/queries/tool.sql 末尾添加。

```sql
-- name: PublishToolVersion :execrows
UPDATE tool_versions
SET status = 'published'
WHERE tool_id = sqlc.arg(tool_id)
  AND id = sqlc.arg(version_id)
  AND status = 'draft';
```

在 internal/platform/tool/postgres_store.go 末尾添加。

```go
func (s *PostgresStore) PublishToolVersion(ctx context.Context, toolID, versionID string) error {
	tid, err := uuid.Parse(toolID)
	if err != nil {
		return ErrToolNotFound
	}
	vid, err := uuid.Parse(versionID)
	if err != nil {
		return ErrToolNotFound
	}

	// SQL 只允许 draft 变为 published。并发请求中只有一个能更新这一行。
	affected, err := s.q.PublishToolVersion(ctx, pgstore.PublishToolVersionParams{
		ToolID: tid, VersionID: vid,
	})
	if err != nil {
		return fmt.Errorf("publish tool version: %w", err)
	}
	if affected == 0 {
		return ErrToolVersionNotDraft
	}
	return nil
}
```

在 internal/platform/tool/service.go 的 Store 接口增加 `PublishToolVersion(ctx context.Context, toolID, versionID string) error`。

## Service

在错误变量块新增。

```go
ErrToolVersionNotDraft = errors.New("tool version is not in draft status")
ErrToolTestRunExpired = errors.New("successful tool test run is more than seven days old")
```

在 internal/platform/tool/service.go 末尾添加。

```go
func (s *Service) PublishToolVersion(ctx context.Context, workspaceID, toolID, versionID string) error {
	item, err := s.store.GetTool(ctx, toolID)
	if err != nil {
		return err
	}
	if item.WorkspaceID != workspaceID {
		return ErrToolNotFound
	}

	// URL 中的工具与版本必须匹配。
	version, err := s.store.GetToolVersion(ctx, versionID)
	if err != nil {
		return err
	}
	if version.ToolID != item.ID {
		return ErrToolNotFound
	}
	if version.Status != "draft" {
		return ErrToolVersionNotDraft
	}

	// 按目标版本找最近一次成功试跑，并检查七天有效期。
	lastTest, err := s.store.GetToolLastSuccessfulTestRunForVersion(ctx, version.ID)
	if err != nil {
		return err
	}
	if time.Since(lastTest.CreatedAt) > 7*24*time.Hour {
		return ErrToolTestRunExpired
	}
	return s.store.PublishToolVersion(ctx, item.ID, version.ID)
}
```

## Handler 与路由

在 internal/api/v1/tool.go 末尾添加。

```go
func (h *ToolHandler) PublishToolVersion(c *gin.Context) {
	ctx := c.Request.Context()
	workspaceID := middleware.GetWorkspaceIDFromContext(ctx)
	err := h.service.PublishToolVersion(ctx, workspaceID, c.Param("tool_id"), c.Param("version_id"))

	switch {
	case errors.Is(err, tool.ErrToolNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "tool version not found"})
	case errors.Is(err, tool.ErrNoSuccessfulTestRun), errors.Is(err, tool.ErrToolTestRunExpired):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, tool.ErrToolVersionNotDraft):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "tool version could not be published"})
	default:
		c.JSON(http.StatusOK, gin.H{"status": "published"})
	}
}
```

在 internal/api/router.go 的工具路由块增加。

```go
workspaceRoutes.POST("/tools/:tool_id/versions/:version_id/publish", toolHandler.PublishToolVersion)
```

执行 make sqlc-generate、gofmt 与 go build ./...。助手已扩展现有 HTTP 验收并接入版本列表的 React 发布按钮，桌面联合电商服务验证通过。发布入口按指定版本只改变该版本的状态，后续 Agent 选择和保存已发布版本的 UUID。
