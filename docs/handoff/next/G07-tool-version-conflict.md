# G07 预审修复：追加版本冲突

固定 Final 在 Service 合并最新工具配置后创建草稿。AsterFlow 沿用配置继承、端点校验及既有工具行锁。在取号后比较 Service 的基础版本和数据库当前版本，阻止旧配置在并发清除凭据后写入新的草稿，映射 HTTP 409。继承发生在事务外，锁内不重新解密、解析 DNS 或加密。

预审回归在临时文件中暂停 A 读到 v1 后的执行，B 清空凭据并提交 v2，再放行 A。正式旧版复现 201 且 v3 的 has_auth=true。正式回归验证 A 返回 tool_version_conflict，v2 保持无凭据，下一次请求从 v2 继承后成功提交 v3。既有无继承的批量 Store 验收传 baseVersion=0；接口调用始终传请求读取到的 current.Version。

前端已增加专门的冲突提示和版本列表刷新。桌面 Chrome 注入受控 409 后验证了提示、关闭表单和基于 v2 重新打开。按负责人授权，Go 修复与测试已写入正式工作区。隔离 PostgreSQL 的定向 HTTP 回归和本地门禁通过。远程 CI 待 PR。

## `internal/platform/tool/service.go`

```diff
--- a/internal/platform/tool/service.go
+++ b/internal/platform/tool/service.go
@@ -16,6 +16,7 @@
    ErrToolNotFound           = errors.New("tool not found")
    ErrInvalidArgument        = errors.New("invalid tool argument")
    ErrToolAlreadyExists      = errors.New("tool name already exists in workspace")
+	ErrToolVersionConflict    = errors.New("tool version changed; refresh and try again")
    ErrNoSuccessfulTestRun    = errors.New("no successful test run for tool version")
    ErrToolVersionNotDraft    = errors.New("tool version is not in draft status")
    ErrToolTestRunExpired     = errors.New("successful tool test run is more than seven days old")
@@ -29,7 +30,7 @@
    ListToolVersions(ctx context.Context, toolID string) ([]*domain.ToolVersion, error)

    GetToolCurrentVersion(ctx context.Context, toolID string) (*domain.ToolVersion, error)
-	AppendToolVersion(ctx context.Context, workspaceID string, v *domain.ToolVersion) error
+	AppendToolVersion(ctx context.Context, workspaceID string, baseVersion int, v *domain.ToolVersion) error
    GetToolVersion(ctx context.Context, versionID string) (*domain.ToolVersion, error)

    CreateTestRun(ctx context.Context, run *domain.ToolTestRun) error
@@ -228,7 +229,8 @@
    if err := s.prepareVersionAuth(version); err != nil {
        return nil, err
    }
-	if err := s.store.AppendToolVersion(ctx, workspaceID, version); err != nil {
+	// 把继承配置时读到的版本号交给 Store，在锁内验证它仍是最新版本。
+	if err := s.store.AppendToolVersion(ctx, workspaceID, current.Version, version); err != nil {
        return nil, err
    }

```

## `internal/platform/tool/postgres_store.go`

```diff
--- a/internal/platform/tool/postgres_store.go
+++ b/internal/platform/tool/postgres_store.go
@@ -164,7 +164,9 @@
    return toolVersionFromRow(row), nil
 }

-func (s *PostgresStore) AppendToolVersion(ctx context.Context, workspaceID string, v *domain.ToolVersion) error {
+func (s *PostgresStore) AppendToolVersion(
+	ctx context.Context, workspaceID string, baseVersion int, v *domain.ToolVersion,
+) error {
    if v == nil {
        return errors.New("tool version is required")
    }
@@ -197,6 +199,10 @@
    next, err := q.NextToolVersion(ctx, toolID)
    if err != nil {
        return fmt.Errorf("get next tool version: %w", err)
+	}
+	// baseVersion 为 0 时，调用方提供的是独立配置，无需校验继承来源。
+	if baseVersion > 0 && int(next)-1 != baseVersion {
+		return ErrToolVersionConflict
    }
    row, err := q.InsertToolVersion(ctx, pgstore.InsertToolVersionParams{
        ID:                  versionID,
```

## `internal/api/v1/tool.go`

```diff
--- a/internal/api/v1/tool.go
+++ b/internal/api/v1/tool.go
@@ -96,6 +96,10 @@
    switch {
    case errors.Is(err, tool.ErrToolNotFound):
        c.JSON(http.StatusNotFound, gin.H{"error": "tool not found"})
+	case errors.Is(err, tool.ErrToolVersionConflict):
+		c.JSON(http.StatusConflict, gin.H{
+			"code": "tool_version_conflict", "error": err.Error(),
+		})
    case errors.Is(err, tool.ErrInvalidArgument):
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
    case err != nil:
```
