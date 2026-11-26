package tooling

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
	"github.com/yz1qaq/asterflow/internal/domain"
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
	// 转交给 Registry 构造的执行器，沿用执行前的参数校验。
	return t.executor.Execute(ctx, json.RawMessage(arguments))
}

func (r *Registry) BuildInvokable(target *tool.ExecutionConfig) (einotool.InvokableTool, error) {
	// 复用 Build 的来源选择、敏感工具拦截和参数校验。
	built, err := r.Build(target)
	if err != nil {
		return nil, err
	}

	// 提供模型选择工具和生成参数时需要的描述。
	info := &schema.ToolInfo{Name: target.Tool.Name, Desc: target.Tool.Description}
	if len(target.Config.Schema) > 0 {
		// 保留 definitions 等原始关键字，使 Schema 内的引用仍可解析。
		params := &jsonschema.Schema{Extras: maps.Clone(target.Config.Schema)}
		info.ParamsOneOf = schema.NewParamsOneOfByJSONSchema(params)
	}

	return &executorInvokableTool{info: info, executor: built.Executor}, nil
}

// BuildByVersion 根据会话快照固定的工具版本构造 Eino 工具。
func (r *Registry) BuildByVersion(
	ctx context.Context, workspaceID, versionID string,
) (einotool.InvokableTool, domain.ToolIdentity, error) {
	if r.versions == nil {
		return nil, domain.ToolIdentity{}, fmt.Errorf("tool version loader is not configured")
	}

	// 按版本 UUID 读取配置，空间归属和凭据由 Tool Service 处理。
	target, err := r.versions.GetToolConfigByVersion(ctx, workspaceID, versionID)
	if err != nil {
		return nil, domain.ToolIdentity{}, fmt.Errorf("load tool version %s: %w", versionID, err)
	}

	built, err := r.BuildInvokable(target)
	if err != nil {
		return nil, domain.ToolIdentity{}, err
	}

	// 两个 ID 与执行器来自同一次版本配置读取。
	return built, domain.ToolIdentity{
		ToolID: target.Tool.ID, VersionID: target.VersionID,
	}, nil
}
