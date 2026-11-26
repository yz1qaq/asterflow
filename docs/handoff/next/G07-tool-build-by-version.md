# G07 按固定版本构造 Eino 工具

固定 Final 的 Engine.buildTools 调用 Registry.BuildByVersion，由注册表按工具版本 UUID 取配置，再构造 Eino 工具。隔离参考实例确认 get_order v1 的 UUID 已写入 Agent 快照。

AsterFlow 候选加入 ToolVersionLoader、BuildByVersion 和启动装配。GetToolConfigByVersion 仍校验工作空间，再提供该版本的参数规则、端点与凭据。BuildInvokable 复用试跑执行器与参数校验，并把模型元数据和执行入口包装为 Eino InvokableTool。本批合入上一批尚未保存的完整适配器代码。下批由 Engine 批量构造并安装到 ADK。

本批为学员手写候选。使用 Go overlay 验证编译，并在隔离 PostgreSQL 与本地 HTTP 替身验证 v2 已发布时，传 v1 UUID 仍只调用 v1 地址，传 v2 UUID 调用 v2 地址；参数不合法、未知版本和跨空间均返回错误。正式工作区已修复会话快照传递并通过隔离会话及 HTTP race。

## `internal/runtime/tooling/registry.go`

```diff
--- a/internal/runtime/tooling/registry.go
+++ b/internal/runtime/tooling/registry.go
@@ -25,9 +25,14 @@
    Executor  Executor
 }

+type ToolVersionLoader interface {
+	GetToolConfigByVersion(ctx context.Context, workspaceID, versionID string) (*tool.ExecutionConfig, error)
+}
+
 type Registry struct {
-	client *http.Client
-	sdk    map[string]Executor
+	client   *http.Client
+	sdk      map[string]Executor
+	versions ToolVersionLoader
 }

 func NewRegistry(policy *tool.EndpointPolicy) (*Registry, error) {
@@ -36,6 +41,11 @@
        return nil, err
    }
    return &Registry{client: client, sdk: make(map[string]Executor)}, nil
+}
+
+// ConfigureVersionLoader 在启动时设置按版本读取配置的服务。
+func (r *Registry) ConfigureVersionLoader(loader ToolVersionLoader) {
+	r.versions = loader
 }

 // RegisterSDK 在启动阶段注册内置 Go 执行器。
```

## 新建 `internal/runtime/tooling/eino.go`

```go
package tooling

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
	"github.com/yz1qaq/asterflow/internal/platform/tool"
)

type executorInvokableTool struct {
	info     *schema.ToolInfo
	executor Executor
}

var _ einotool.InvokableTool = (*executorInvokableTool)(nil)

func (t *executorInvokableTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t *executorInvokableTool) InvokableRun(
	ctx context.Context, arguments string, _ ...einotool.Option,
) (string, error) {
	// 复用 Registry 构造的执行器，进入业务调用前仍会校验参数。
	return t.executor.Execute(ctx, json.RawMessage(arguments))
}

func (r *Registry) BuildInvokable(target *tool.ExecutionConfig) (einotool.InvokableTool, error) {
	// 先走现有构建入口，沿用敏感工具拦截、来源选择和参数校验。
	built, err := r.Build(target)
	if err != nil {
		return nil, err
	}

	// 模型只看到名称、描述和入参定义。
	info := &schema.ToolInfo{Name: target.Tool.Name, Desc: target.Tool.Description}
	if len(target.Config.Schema) > 0 {
		// 保留原始关键字，例如 Draft 7 的 definitions 及指向它的 $ref。
		params := &jsonschema.Schema{Extras: maps.Clone(target.Config.Schema)}
		info.ParamsOneOf = schema.NewParamsOneOfByJSONSchema(params)
	}

	return &executorInvokableTool{info: info, executor: built.Executor}, nil
}

// BuildByVersion 根据会话快照固定的工具版本构造 Eino 工具。
func (r *Registry) BuildByVersion(
	ctx context.Context, workspaceID, versionID string,
) (einotool.InvokableTool, error) {
	if r.versions == nil {
		return nil, fmt.Errorf("tool version loader is not configured")
	}

	// 按版本 UUID 读取配置，空间归属和凭据由 Tool Service 处理。
	target, err := r.versions.GetToolConfigByVersion(ctx, workspaceID, versionID)
	if err != nil {
		return nil, fmt.Errorf("load tool version %s: %w", versionID, err)
	}
	return r.BuildInvokable(target)
}
```

## `cmd/server/main.go`

```diff
--- a/cmd/server/main.go
+++ b/cmd/server/main.go
@@ -113,6 +113,7 @@
    if err != nil {
        return fmt.Errorf("initialize tool registry: %w", err)
    }
+	toolRegistry.ConfigureVersionLoader(toolService)

    handler := api.NewRouter(pool, iamService, modelService, promptService, api.Services{
        Agent:        agentService,
```

## 验收

gofmt 三个文件，执行 go mod tidy。候选验证见当前状态。学员已保存，助手把按版本构建与 ToolsNode 测试并入现有测试，正式隔离 PostgreSQL race 通过。下一批 [Engine 组装列表](G07-engine-build-tools.md)。
