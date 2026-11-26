package tool

import (
	"context"
	"errors"

	"github.com/yz1qaq/asterflow/internal/domain"
)

type ExecutionConfig struct {
	Tool      *domain.Tool `json:"tool"`
	VersionID string       `json:"version_id"`
	Config    *ToolConfig  `json:"-"`
}

func (s *Service) GetToolConfigForTest(
	ctx context.Context, workspaceID, toolID string,
) (*ExecutionConfig, error) {
	item, err := s.store.GetTool(ctx, toolID)
	if err != nil {
		return nil, err
	}
	if item.WorkspaceID != workspaceID {
		return nil, ErrToolNotFound
	}

	// 试跑取版本号最大的一版，后续执行和记账都使用这次读到的版本。
	version, err := s.store.GetToolCurrentVersion(ctx, item.ID)
	if err != nil {
		return nil, err
	}
	return s.resolveExecutionConfig(ctx, item, version)
}

func (s *Service) GetToolConfigByVersion(
	ctx context.Context, workspaceID, versionID string,
) (*ExecutionConfig, error) {
	version, err := s.store.GetToolVersion(ctx, versionID)
	if err != nil {
		return nil, err
	}
	item, err := s.store.GetTool(ctx, version.ToolID)
	if err != nil {
		return nil, err
	}
	if item.WorkspaceID != workspaceID {
		return nil, ErrToolNotFound
	}
	return s.resolveExecutionConfig(ctx, item, version)
}

func (s *Service) resolveExecutionConfig(
	ctx context.Context, item *domain.Tool, version *domain.ToolVersion,
) (*ExecutionConfig, error) {
	// 先完成空间校验，再解密凭据并组装执行器需要的配置。
	auth, err := s.authPlain(version)
	if err != nil {
		return nil, err
	}
	cfg, err := toolConfigFromStrings(item.SourceType, version.SchemaJSON, version.EndpointConfig, auth)
	if err != nil {
		return nil, err
	}
	if s.policy == nil {
		return nil, errors.New("tool endpoint policy is not configured")
	}
	if err := s.policy.validateConfig(ctx, cfg.SourceType, cfg.EndpointConfig); err != nil {
		return nil, err
	}

	return &ExecutionConfig{Tool: item, VersionID: version.ID, Config: cfg}, nil
}
