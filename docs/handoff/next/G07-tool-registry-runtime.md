# G07 Registry 与工具参数校验

固定 Final 使用 Registry 根据 source_type 选择工厂，使用 gojsonschema v1.2.0 编译参数 Schema，并把校验器包在执行器外。AsterFlow 本批覆盖 rest_api 和 internal_sdk，使用当前执行配置里的 ToolID、VersionID。空 Schema 直接沿用执行器，有 Schema 则校验通过才执行。允许同一 Schema 文档内的引用，拒绝额外加载外部文档。敏感工具在 G12 的审批能力接入前受拦截。

学员保存的 REST 已加正式回归，真实 Crossborder/一次性 PostgreSQL 验证成功和 404。下列代码已由学员保存，正式工作区通过隔离 Crossborder/PG race，覆盖 SDK 注册、参数拒绝不执行、文档内引用、外部引用不请求、敏感拦截与 Registry 组装的真实电商请求。

## Schema

新建 `internal/runtime/tooling/schema.go`。

```go
package tooling

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/xeipuuv/gojsonschema"
)

type schemaValidatingExecutor struct {
	next   Executor
	schema *gojsonschema.Schema
}

func withSchemaValidation(next Executor, schema map[string]any) (Executor, error) {
	if len(schema) == 0 {
		return next, nil
	}

	// 编译时使用本地配置。文档内引用由库处理，外部加载统一拒绝。
	loader := inlineSchemaLoader{JSONLoader: gojsonschema.NewGoLoader(schema)}
	compiled, err := gojsonschema.NewSchema(loader)
	if err != nil {
		return nil, fmt.Errorf("compile tool input schema: %w", err)
	}
	return &schemaValidatingExecutor{next: next, schema: compiled}, nil
}

func (e *schemaValidatingExecutor) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if !json.Valid(args) {
		return "", errors.New("tool input must be valid JSON")
	}
	result, err := e.schema.Validate(gojsonschema.NewBytesLoader(args))
	if err != nil {
		return "", fmt.Errorf("validate tool input: %w", err)
	}

	// 参数合法才调用实际执行器，校验失败时返回字段问题。
	if !result.Valid() {
		problems := make([]string, 0, len(result.Errors()))
		for _, problem := range result.Errors() {
			problems = append(problems, problem.String())
		}
		return "", fmt.Errorf("tool input violates schema: %s", strings.Join(problems, ", "))
	}
	return e.next.Execute(ctx, args)
}

type inlineSchemaLoader struct {
	gojsonschema.JSONLoader
}

func (inlineSchemaLoader) LoaderFactory() gojsonschema.JSONLoaderFactory {
	return blockedSchemaFactory{}
}

type blockedSchemaFactory struct{}

func (blockedSchemaFactory) New(source string) gojsonschema.JSONLoader {
	return blockedSchemaLoader{JSONLoader: gojsonschema.NewReferenceLoader(source)}
}

type blockedSchemaLoader struct {
	gojsonschema.JSONLoader
}

func (blockedSchemaLoader) LoadJSON() (any, error) {
	return nil, errors.New("external tool schema references are disabled")
}
```

## Registry

新建 `internal/runtime/tooling/registry.go`。

```go
package tooling

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/yz1qaq/asterflow/internal/platform/tool"
)

var ErrSensitiveToolRequiresApproval = errors.New("sensitive tool requires approval")

type ExecutorFunc func(ctx context.Context, args json.RawMessage) (string, error)

func (f ExecutorFunc) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	return f(ctx, args)
}

type BuiltTool struct {
	ToolID    string
	VersionID string
	Executor  Executor
}

type Registry struct {
	client *http.Client
	sdk    map[string]Executor
}

func NewRegistry(policy *tool.EndpointPolicy) (*Registry, error) {
	client, err := policy.HTTPClient(30 * time.Second)
	if err != nil {
		return nil, err
	}
	return &Registry{client: client, sdk: make(map[string]Executor)}, nil
}

// RegisterSDK 在启动阶段注册内置 Go 执行器。
func (r *Registry) RegisterSDK(name string, executor Executor) {
	r.sdk[name] = executor
}

func (r *Registry) Build(target *tool.ExecutionConfig) (*BuiltTool, error) {
	if target == nil || target.Tool == nil || target.Config == nil {
		return nil, errors.New("tool execution config is required")
	}
	if target.Tool.Sensitive {
		return nil, ErrSensitiveToolRequiresApproval
	}

	cfg := target.Config
	var executor Executor

	// 工具的来源类型决定如何构造执行器。
	switch cfg.SourceType {
	case "rest_api":
		executor = NewRESTExecutor(r.client, cfg)
	case "internal_sdk":
		name, _ := cfg.EndpointConfig["sdk_name"].(string)
		executor = r.sdk[name]
		if executor == nil {
			return nil, fmt.Errorf("internal_sdk %q is not registered", name)
		}
	default:
		return nil, fmt.Errorf("unsupported tool source: %s", cfg.SourceType)
	}

	validated, err := withSchemaValidation(executor, cfg.Schema)
	if err != nil {
		return nil, err
	}
	// 返回同一次读取拿到的版本 ID，调用方可以据此保存试跑记录。
	return &BuiltTool{ToolID: target.Tool.ID, VersionID: target.VersionID, Executor: validated}, nil
}
```

新增上述文件后，执行 `go get github.com/xeipuuv/gojsonschema@v1.2.0`、`go mod tidy`、`gofmt -w internal/runtime/tooling/schema.go internal/runtime/tooling/registry.go`、`go build ./...`。RegisterSDK 在启动阶段完成，后续请求共享只读的执行器注册表。

助手已写入正式 Registry 回归。接下来实现[试跑 HTTP 入口](G07-tool-test-http.md)，将鉴权上下文、GetToolConfigForTest、Registry.Build、Execute 和 RecordTestRun 串起来，再接网页试跑。保持 VersionID 来自本次构建，发布仍按同版本的近期成功记录判断。
