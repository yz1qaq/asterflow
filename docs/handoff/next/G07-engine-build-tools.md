# G07 Engine 组装快照工具列表

固定 Final 的 Engine.buildTools 遍历快照里的版本 UUID，经 Registry.BuildByVersion 组装工具，并以模型使用的工具名检查重复。AsterFlow 沿用这一过程，输出 Eino ToolsConfig 需要的 BaseTool 列表。Tool Service 保持同一工作空间和固定版本的配置读取。

本批为学员手写候选。隔离候选已通过 go race，真实 Eino ToolsNode 收到快照工具列表后，按 get_order 找到 v1 执行器；重复名、缺少构建器、未知版本均返回错误。候选生产启动编译通过。

目前正式聊天的 gatewayChatModel / OpenAI 传输层只支持正文，下一步补模型接收工具定义以及返回、回喂工具消息的能力，然后将此列表交给实际 ADK Runner。该阶段尚无聊天工具调用网页验收。

## `internal/runtime/engine/executor.go`

```diff
--- a/internal/runtime/engine/executor.go
+++ b/internal/runtime/engine/executor.go
@@ -20,6 +20,7 @@
 type Executor struct {
    promptRenderer PromptRenderer
    models         llm.Gateway
+	toolBuilder    VersionedToolBuilder
 }

 const maxHistoryMessages = 10
```

## 新建 `internal/runtime/engine/tools.go`

```go
package engine

import (
	"context"
	"fmt"

	einotool "github.com/cloudwego/eino/components/tool"
)

type VersionedToolBuilder interface {
	BuildByVersion(ctx context.Context, workspaceID, versionID string) (einotool.InvokableTool, error)
}

// ConfigureTools 在启动装配时设置按版本构建工具的依赖。
func (e *Executor) ConfigureTools(builder VersionedToolBuilder) {
	e.toolBuilder = builder
}

func (e *Executor) buildTools(ctx context.Context, in RunInput) ([]einotool.BaseTool, error) {
	if len(in.ToolVersionIDs) == 0 {
		return nil, nil
	}
	if e.toolBuilder == nil {
		return nil, fmt.Errorf("tool builder is not configured")
	}

	tools := make([]einotool.BaseTool, 0, len(in.ToolVersionIDs))
	seen := make(map[string]struct{}, len(in.ToolVersionIDs))
	for _, versionID := range in.ToolVersionIDs {
		// 沿用会话传入的固定版本和工作空间。
		built, err := e.toolBuilder.BuildByVersion(ctx, in.WorkspaceID, versionID)
		if err != nil {
			return nil, fmt.Errorf("build tool version %s: %w", versionID, err)
		}
		info, err := built.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("read tool info for version %s: %w", versionID, err)
		}

		// 模型按名称发起调用，重复的名字会让执行入口不唯一。
		if _, exists := seen[info.Name]; exists {
			return nil, fmt.Errorf("duplicate tool name %q in agent snapshot", info.Name)
		}
		seen[info.Name] = struct{}{}
		tools = append(tools, built)
	}
	return tools, nil
}
```

## `cmd/server/main.go`

```diff
--- a/cmd/server/main.go
+++ b/cmd/server/main.go
@@ -114,6 +114,7 @@
        return fmt.Errorf("initialize tool registry: %w", err)
    }
    toolRegistry.ConfigureVersionLoader(toolService)
+	executor.ConfigureTools(toolRegistry)
    handler := api.NewRouter(pool, iamService, modelService, promptService, api.Services{
        Agent:        agentService,
        Conversation: conversationService,
```

## 验收

学员已保存，助手已把回归写入现有 Engine 测试，正式 race 通过。下一批 [Eino 模型适配器](G07-eino-model-provider.md)。上一批 Eino Schema 已成为直接 import，运行 go mod tidy 整理 go.mod；Go 构建输出应写入 bin 或临时目录。
