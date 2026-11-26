# G07 工具调用的主记录与版本 ID

固定 Final 区分工具主记录和不可变版本，也分别定义 tool_calls.tool_id 和 tool_calls.tool_version_id。AsterFlow 现有 Store 已支持这两列。现有运行时只传递版本 UUID，数据库回归确认 tool_id 为 NULL。

Registry 的按版本加载在同一次请求中得到工具主记录和指定版本。让 BuildByVersion 额外返回 ToolIdentity。Engine 继续按模型看到的工具名建表，结果传给会话，保存时按名称查回两个 UUID。模型调用 ID 继续用于 assistant 与 tool 结果配对。未加载的工具没有匹配项，两个 UUID 为空。已有记录可按原格式读取，本批无需迁移。

已按负责人授权保存 Go 并更新三处既有测试。正式工作区的本地门禁与隔离桌面验证覆盖两个固定版本使用同一主记录、两个 UUID 落库、未知工具记录为空、网页刷新回读。桌面模型使用本机替身，Crossborder 服务与 PostgreSQL 为真实隔离实例。

## `internal/domain/tool.go`

```diff
--- a/internal/domain/tool.go
+++ b/internal/domain/tool.go
@@ -12,6 +12,12 @@
    CreatedBy   string    `json:"created_by"`
    CreatedAt   time.Time `json:"created_at"`
    UpdatedAt   time.Time `json:"updated_at"`
+}
+
+// ToolIdentity 是本轮实际加载的工具主记录和版本标识。
+type ToolIdentity struct {
+   ToolID    string
+   VersionID string
 }

 type ToolVersion struct {
```

## `internal/runtime/tooling/eino.go`

```diff
--- a/internal/runtime/tooling/eino.go
+++ b/internal/runtime/tooling/eino.go
@@ -9,6 +9,7 @@
    einotool "github.com/cloudwego/eino/components/tool"
    "github.com/cloudwego/eino/schema"
    "github.com/eino-contrib/jsonschema"
+   "github.com/yz1qaq/asterflow/internal/domain"
    "github.com/yz1qaq/asterflow/internal/platform/tool"
 )

@@ -51,15 +52,24 @@
 // BuildByVersion 根据会话快照固定的工具版本构造 Eino 工具。
 func (r *Registry) BuildByVersion(
    ctx context.Context, workspaceID, versionID string,
-) (einotool.InvokableTool, error) {
+) (einotool.InvokableTool, domain.ToolIdentity, error) {
    if r.versions == nil {
-       return nil, fmt.Errorf("tool version loader is not configured")
+       return nil, domain.ToolIdentity{}, fmt.Errorf("tool version loader is not configured")
    }

    // 按版本 UUID 读取配置，空间归属和凭据由 Tool Service 处理。
    target, err := r.versions.GetToolConfigByVersion(ctx, workspaceID, versionID)
    if err != nil {
-       return nil, fmt.Errorf("load tool version %s: %w", versionID, err)
+       return nil, domain.ToolIdentity{}, fmt.Errorf("load tool version %s: %w", versionID, err)
    }
-   return r.BuildInvokable(target)
+
+   built, err := r.BuildInvokable(target)
+   if err != nil {
+       return nil, domain.ToolIdentity{}, err
+   }
+
+   // 两个 ID 与执行器来自同一次版本配置读取。
+   return built, domain.ToolIdentity{
+       ToolID: target.Tool.ID, VersionID: target.VersionID,
+   }, nil
 }
```

## `internal/runtime/engine/tools.go`

```diff
--- a/internal/runtime/engine/tools.go
+++ b/internal/runtime/engine/tools.go
@@ -5,10 +5,11 @@
    "fmt"

    einotool "github.com/cloudwego/eino/components/tool"
+   "github.com/yz1qaq/asterflow/internal/domain"
 )

 type VersionedToolBuilder interface {
-   BuildByVersion(ctx context.Context, workspaceID, versionID string) (einotool.InvokableTool, error)
+   BuildByVersion(ctx context.Context, workspaceID, versionID string) (einotool.InvokableTool, domain.ToolIdentity, error)
 }

 // ConfigureTools 在启动装配时设置按版本构建工具的依赖。
@@ -18,7 +19,7 @@

 func (e *Executor) buildTools(
    ctx context.Context, in RunInput,
-) ([]einotool.BaseTool, map[string]string, error) {
+) ([]einotool.BaseTool, map[string]domain.ToolIdentity, error) {
    if len(in.ToolVersionIDs) == 0 {
        return nil, nil, nil
    }
@@ -27,10 +28,10 @@
    }

    tools := make([]einotool.BaseTool, 0, len(in.ToolVersionIDs))
-   versions := make(map[string]string, len(in.ToolVersionIDs))
+   identities := make(map[string]domain.ToolIdentity, len(in.ToolVersionIDs))

    for _, versionID := range in.ToolVersionIDs {
-       built, err := e.toolBuilder.BuildByVersion(ctx, in.WorkspaceID, versionID)
+       built, identity, err := e.toolBuilder.BuildByVersion(ctx, in.WorkspaceID, versionID)
        if err != nil {
            return nil, nil, fmt.Errorf("build tool version %s: %w", versionID, err)
        }
@@ -38,13 +39,13 @@
        if err != nil {
            return nil, nil, fmt.Errorf("read tool info for version %s: %w", versionID, err)
        }
-       if _, exists := versions[info.Name]; exists {
+       if _, exists := identities[info.Name]; exists {
            return nil, nil, fmt.Errorf("duplicate tool name %q in agent snapshot", info.Name)
        }

-       // 记住实际构建的版本，保存工具记录时按名称查回 UUID。
-       versions[info.Name] = versionID
+       // 模型按名称请求工具，入库时按该名称查回两个 ID。
+       identities[info.Name] = identity
        tools = append(tools, built)
    }
-   return tools, versions, nil
+   return tools, identities, nil
 }
```

## `internal/runtime/engine/executor.go`

```diff
--- a/internal/runtime/engine/executor.go
+++ b/internal/runtime/engine/executor.go
@@ -57,7 +57,7 @@
    NextSeq              int64
    ToolMessages         []*schema.Message
    ToolResults          []ToolResultEvent
-   ToolVersions         map[string]string
+   ToolIdentities       map[string]domain.ToolIdentity
 }

 func (e *Executor) Run(ctx context.Context, in RunInput, emit Emitter) (*RunResult, error) {
@@ -172,7 +172,7 @@
    }

    // 会话快照决定本轮可供 ADK 调用的工具。
-   tools, versions, err := e.buildTools(ctx, in)
+   tools, toolIdentities, err := e.buildTools(ctx, in)
    if err != nil {
        if ctx.Err() != nil {
            return nil, ctx.Err()
@@ -217,7 +217,7 @@
    }
    result.NextSeq = seq
    result.ToolResults = toolResults
-   result.ToolVersions = versions
+   result.ToolIdentities = toolIdentities

    if userVersion != "" {
        result.RenderedUserPrompt = user
```

## `internal/platform/conversation/turn_messages.go`

```diff
--- a/internal/platform/conversation/turn_messages.go
+++ b/internal/platform/conversation/turn_messages.go
@@ -37,12 +37,15 @@
                    return nil, fmt.Errorf("invalid assistant tool call %q", call.ID)
                }

+               // 未知工具不在本轮装配结果里，两个字段保持空值。
+               identity := result.ToolIdentities[call.Function.Name]
+
                // SQL 主键与模型生成的调用 ID 分别保存。
                message.ToolCalls[i] = domain.ToolCall{
                    ID: uuid.NewString(), MessageID: message.ID,
                    ProviderCallID: call.ID, Name: call.Function.Name,
-                   ToolVersionID: result.ToolVersions[call.Function.Name],
-                   Args:          call.Function.Arguments,
+                   ToolID: identity.ToolID, ToolVersionID: identity.VersionID,
+                   Args: call.Function.Arguments,
                }
                pending[call.ID] = &message.ToolCalls[i]
            }
```
