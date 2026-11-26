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

type ToolVersionLoader interface {
	GetToolConfigByVersion(ctx context.Context, workspaceId, versionID string) (*tool.ExecutionConfig, error)
}

func (f ExecutorFunc) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	return f(ctx, args)
}

type BuiltTool struct {
	ToolID    string
	VersionID string
	Executor  Executor
}

type Registry struct {
	client   *http.Client
	sdk      map[string]Executor
	versions ToolVersionLoader
}

func NewRegistry(policy *tool.EndpointPolicy) (*Registry, error) {
	client, err := policy.HTTPClient(30 * time.Second)
	if err != nil {
		return nil, err
	}
	return &Registry{client: client, sdk: make(map[string]Executor)}, nil
}

// ConfigureVersionLoader 在启动时设置按版本读取配置的服务。
func (r *Registry) ConfigureVersionLoader(loader ToolVersionLoader) {
	r.versions = loader
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
