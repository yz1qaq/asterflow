package engine

import (
	"context"
	"fmt"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/yz1qaq/asterflow/internal/domain"
)

type VersionedToolBuilder interface {
	BuildByVersion(ctx context.Context, workspaceID, versionID string) (einotool.InvokableTool, domain.ToolIdentity, error)
}

// ConfigureTools 在启动装配时设置按版本构建工具的依赖。
func (e *Executor) ConfigureTools(builder VersionedToolBuilder) {
	e.toolBuilder = builder
}

func (e *Executor) buildTools(
	ctx context.Context, in RunInput,
) ([]einotool.BaseTool, map[string]domain.ToolIdentity, error) {
	if len(in.ToolVersionIDs) == 0 {
		return nil, nil, nil
	}
	if e.toolBuilder == nil {
		return nil, nil, fmt.Errorf("tool builder is not configured")
	}

	tools := make([]einotool.BaseTool, 0, len(in.ToolVersionIDs))
	identities := make(map[string]domain.ToolIdentity, len(in.ToolVersionIDs))

	for _, versionID := range in.ToolVersionIDs {
		built, identity, err := e.toolBuilder.BuildByVersion(ctx, in.WorkspaceID, versionID)
		if err != nil {
			return nil, nil, fmt.Errorf("build tool version %s: %w", versionID, err)
		}
		info, err := built.Info(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("read tool info for version %s: %w", versionID, err)
		}
		if _, exists := identities[info.Name]; exists {
			return nil, nil, fmt.Errorf("duplicate tool name %q in agent snapshot", info.Name)
		}

		// 模型按名称请求工具，入库时按该名称查回两个 ID。
		identities[info.Name] = identity
		tools = append(tools, built)
	}
	return tools, identities, nil
}
