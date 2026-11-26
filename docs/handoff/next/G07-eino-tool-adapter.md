# G07 Eino 工具适配器

固定 Final 的 `Registry.BuildByVersion` 从快照的工具版本构造执行器和 `executorInvokableTool`，再通过 `ToolsConfig` 交给 ADK。隔离参考实例已启动，工具试跑查询虚构订单 `TTS-20260801-1001` 返回 `awaiting_shipment`，缺少 `order_id` 时在 Schema 校验处被拒。

本批为学员手写候选。AsterFlow 保持已有 Registry 输入 `ExecutionConfig`，用 `BuildInvokable` 在聊天侧新增 Eino 入口，试跑仍使用 `Build`。构建后的 `Info` 只含名称、描述和参数定义；`InvokableRun` 继续经过已构建的校验执行器。此前已支持的 Draft 7 `definitions` 在 Eino JSON Schema 的普通反序列化中会丢失，本批通过 `Extras` 原样序列化参数定义。执行验证仍由现有 gojsonschema 负责。

## 新建 `internal/runtime/tooling/eino.go`

```go
package tooling

import (
	"context"
	"encoding/json"
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
```

## 依赖与验证

在仓库根目录执行 `gofmt -w internal/runtime/tooling/eino.go`、`go mod tidy`、`go test ./internal/runtime/tooling`。沿用已锁定的 `github.com/eino-contrib/jsonschema v1.0.3`，直接 import 后整理 `go.mod` 的依赖标记。

验证记录见 [当前状态](../STATE.md)。学员已按下一批完整代码保存，ToolsNode 回归已合入工具测试并通过。完整适配器已并入[按固定版本构造 Eino 工具](G07-tool-build-by-version.md)候选，请采用该批完整 eino.go。聊天界面在完整调用链就绪后验收。
