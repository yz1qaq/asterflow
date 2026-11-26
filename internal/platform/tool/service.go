package tool

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yz1qaq/asterflow/internal/domain"
)

var (
	ErrToolNotFound           = errors.New("tool not found")
	ErrInvalidArgument        = errors.New("invalid tool argument")
	ErrToolAlreadyExists      = errors.New("tool name already exists in workspace")
	ErrToolVersionConflict    = errors.New("tool version changed; refresh and try again")
	ErrNoSuccessfulTestRun    = errors.New("no successful test run for tool version")
	ErrToolVersionNotDraft    = errors.New("tool version is not in draft status")
	ErrToolTestRunExpired     = errors.New("successful tool test run is more than seven days old")
	ErrNoPublishedToolVersion = errors.New("tool has no published version")
)

type Store interface {
	CreateToolWithVersion(ctx context.Context, t *domain.Tool, v *domain.ToolVersion) error
	GetTool(ctx context.Context, toolID string) (*domain.Tool, error)
	ListTools(ctx context.Context, workspaceID string) ([]*domain.Tool, error)
	ListToolVersions(ctx context.Context, toolID string) ([]*domain.ToolVersion, error)

	GetToolCurrentVersion(ctx context.Context, toolID string) (*domain.ToolVersion, error)
	AppendToolVersion(ctx context.Context, workspaceID string, baseVersion int, v *domain.ToolVersion) error
	GetToolVersion(ctx context.Context, versionID string) (*domain.ToolVersion, error)

	CreateTestRun(ctx context.Context, run *domain.ToolTestRun) error
	GetToolLastSuccessfulTestRunForVersion(ctx context.Context, versionID string) (*domain.ToolTestRun, error)

	PublishToolVersion(ctx context.Context, toolID, versionID string) error
	GetToolLatestPublishedVersionID(ctx context.Context, toolID string) (string, error)
}

type Service struct {
	store  Store
	cipher CredentialCipher
	policy *EndpointPolicy
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) ListTools(ctx context.Context, workspaceID string) ([]*domain.Tool, error) {
	return s.store.ListTools(ctx, workspaceID)
}

func (s *Service) EnsureToolWorkspace(ctx context.Context, toolID, workspaceID string) error {
	item, err := s.store.GetTool(ctx, toolID)
	if err != nil {
		return err
	}
	if item.WorkspaceID != workspaceID {
		return ErrToolNotFound
	}
	return nil
}

func (s *Service) ListToolVersions(
	ctx context.Context, workspaceID, toolID string,
) ([]*domain.ToolVersion, error) {
	if err := s.EnsureToolWorkspace(ctx, toolID, workspaceID); err != nil {
		return nil, err
	}

	versions, err := s.store.ListToolVersions(ctx, toolID)
	if err != nil {
		return nil, err
	}

	result := make([]*domain.ToolVersion, 0, len(versions))
	for _, version := range versions {
		result = append(result, publicToolVersion(version))
	}
	return result, nil
}

func (s *Service) CreateTool(ctx context.Context, req CreateToolRequest) (*domain.Tool, error) {
	// 先检查注册必填项和工具来源。
	req.WorkspaceID = strings.TrimSpace(req.WorkspaceID)
	if req.WorkspaceID == "" {
		return nil, fmt.Errorf("%w: workspace id is required", ErrInvalidArgument)
	}
	req.Name = strings.TrimSpace(req.Name)
	if !toolNamePattern.MatchString(req.Name) {
		return nil, fmt.Errorf("%w: tool name must match [A-Za-z0-9_-]{1,64}", ErrInvalidArgument)
	}
	if !isValidSourceType(req.SourceType) {
		return nil, fmt.Errorf("%w: invalid source_type", ErrInvalidArgument)
	}

	// 配置解析成功后，再确认端点符合当前部署的访问策略。
	cfg, err := toolConfigFromStrings(
		req.SourceType, req.SchemaJSON, req.EndpointConfig, req.AuthConfig,
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	if err := validateToolEndpointKeys(req.EndpointConfig); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	if s.policy == nil {
		return nil, errors.New("tool endpoint policy is not configured")
	}
	if err := s.policy.validateConfig(ctx, cfg.SourceType, cfg.EndpointConfig); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}

	// 工具和草稿各有一个 UUID，草稿通过 ToolID 关联主记录。
	now := time.Now().UTC()
	item := &domain.Tool{
		ID:          uuid.NewString(),
		WorkspaceID: req.WorkspaceID,
		Name:        req.Name,
		SourceType:  req.SourceType,
		Description: req.Description,
		Sensitive:   req.Sensitive,
		CreatedBy:   req.CreatedBy,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	version := &domain.ToolVersion{
		ID:             uuid.NewString(),
		ToolID:         item.ID,
		Version:        1,
		SchemaJSON:     req.SchemaJSON,
		EndpointConfig: req.EndpointConfig,
		AuthConfig:     req.AuthConfig,
		RetryPolicy:    `{"max_attempts":3,"backoff_ms":1000}`,
		Status:         "draft",
		CreatedBy:      req.CreatedBy,
		CreatedAt:      now,
	}

	// 加密成功后再通过 Store 原子写入两张表。
	if err := s.prepareVersionAuth(version); err != nil {
		return nil, err
	}
	if err := s.store.CreateToolWithVersion(ctx, item, version); err != nil {
		return nil, fmt.Errorf("create tool and version: %w", err)
	}
	return item, nil
}

func (s *Service) CreateToolVersion(
	ctx context.Context, workspaceID, toolID string, req CreateToolVersionRequest,
) (*domain.ToolVersion, error) {
	// 先确认工具归属，再读取它的最新版本和凭据。
	item, err := s.store.GetTool(ctx, toolID)
	if err != nil {
		return nil, err
	}
	if item.WorkspaceID != workspaceID {
		return nil, ErrToolNotFound
	}
	current, err := s.store.GetToolCurrentVersion(ctx, item.ID)
	if err != nil {
		return nil, err
	}

	// 空字符串表示沿用。显式传入 "{}" 可以清空鉴权配置。
	if req.SchemaJSON == "" {
		req.SchemaJSON = current.SchemaJSON
	}
	if req.EndpointConfig == "" {
		req.EndpointConfig = current.EndpointConfig
	}
	inheritAuth := req.AuthConfig == ""
	if inheritAuth {
		req.AuthConfig, err = s.authPlain(current)
		if err != nil {
			return nil, err
		}
	}
	if req.RetryPolicy == "" {
		req.RetryPolicy = current.RetryPolicy
	}

	// 校验合并后的完整配置，继承的端点也要符合当前策略。
	cfg, err := toolConfigFromStrings(item.SourceType, req.SchemaJSON, req.EndpointConfig, req.AuthConfig)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	if _, err := parseJSONObject("retry_policy", req.RetryPolicy); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	if err := validateToolEndpointKeys(req.EndpointConfig); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	if s.policy == nil {
		return nil, errors.New("tool endpoint policy is not configured")
	}
	if err := s.policy.validateConfig(ctx, cfg.SourceType, cfg.EndpointConfig); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	// 沿用旧凭据时，REST 目标必须保持同源；换目标时需显式配置。
	if inheritAuth && item.SourceType == "rest_api" && len(cfg.AuthConfig) > 0 {
		oldEndpoint, err := parseJSONObject("endpoint_config", current.EndpointConfig)
		if err != nil {
			return nil, fmt.Errorf("read previous tool endpoint: %w", err)
		}
		oldURL, oldErr := url.Parse(stringValue(oldEndpoint["url"]))
		newURL, newErr := url.Parse(stringValue(cfg.EndpointConfig["url"]))
		if oldErr != nil || newErr != nil || !sameOrigin(oldURL, newURL) {
			return nil, fmt.Errorf("%w: endpoint origin changed; provide auth_config or clear it with {}", ErrInvalidArgument)
		}
	}
	if len(cfg.AuthConfig) == 0 {
		req.AuthConfig = "{}"
	}

	version := &domain.ToolVersion{
		ID:             uuid.NewString(),
		ToolID:         item.ID,
		SchemaJSON:     req.SchemaJSON,
		EndpointConfig: req.EndpointConfig,
		AuthConfig:     req.AuthConfig,
		RetryPolicy:    req.RetryPolicy,
		CreatedBy:      req.CreatedBy,
	}

	// 凭据加密后再落库，Store 负责版本号、草稿状态与创建时间。
	if err := s.prepareVersionAuth(version); err != nil {
		return nil, err
	}
	// 把继承配置时读到的版本号交给 Store，在锁内验证它仍是最新版本。
	if err := s.store.AppendToolVersion(ctx, workspaceID, current.Version, version); err != nil {
		return nil, err
	}

	return publicToolVersion(version), nil
}

func publicToolVersion(version *domain.ToolVersion) *domain.ToolVersion {
	safeVersion := *version
	safeVersion.AuthConfig = "{}"
	safeVersion.AuthConfigEncrypted = nil
	safeVersion.EndpointConfig, safeVersion.EndpointConfigRedacted = publicToolEndpointConfig(version.EndpointConfig)
	return &safeVersion
}

func publicToolEndpointConfig(raw string) (string, bool) {
	config, err := parseJSONObject("endpoint_config", raw)
	if err != nil || validateToolEndpointKeys(raw) != nil {
		return "{}", true
	}
	// 隐藏整份不合规的旧配置，避免把删减后的地址当作原配置继续编辑。
	for _, field := range []string{"url", "card_url"} {
		value, exists := config[field]
		if !exists || value == "" {
			continue
		}
		endpoint, ok := value.(string)
		if !ok {
			return "{}", true
		}
		if _, err := parseConfiguredToolEndpointURL(endpoint); err != nil {
			return "{}", true
		}
	}
	return raw, false
}

func (s *Service) RecordTestRun(
	ctx context.Context,
	workspaceID, toolID, versionID string,
	input, output string,
	latencyMs int,
	runErr error,
) (*domain.ToolTestRun, error) {
	if err := s.EnsureToolWorkspace(ctx, toolID, workspaceID); err != nil {
		return nil, err
	}

	// 使用执行开始时选定的版本，避免并发追加版本后记错试跑归属。
	run := &domain.ToolTestRun{
		ID:            uuid.NewString(),
		ToolID:        toolID,
		ToolVersionID: versionID,
		Input:         input,
		Output:        output,
		Status:        "success",
		LatencyMs:     latencyMs,
	}
	if runErr != nil {
		message := runErr.Error()
		run.Status = "error"
		run.Error = &message
	}

	if err := s.store.CreateTestRun(ctx, run); err != nil {
		return nil, err
	}
	return run, nil
}

func (s *Service) PublishToolVersion(ctx context.Context, workspaceID, toolID, versionID string) error {
	item, err := s.store.GetTool(ctx, toolID)
	if err != nil {
		return err
	}
	if item.WorkspaceID != workspaceID {
		return ErrToolNotFound
	}

	// URL 中的工具与版本必须匹配。
	version, err := s.store.GetToolVersion(ctx, versionID)
	if err != nil {
		return err
	}
	if version.ToolID != item.ID {
		return ErrToolNotFound
	}
	if version.Status != "draft" {
		return ErrToolVersionNotDraft
	}

	// 按目标版本找最近一次成功试跑，并检查七天有效期。
	lastTest, err := s.store.GetToolLastSuccessfulTestRunForVersion(ctx, version.ID)
	if err != nil {
		return err
	}
	if time.Since(lastTest.CreatedAt) > 7*24*time.Hour {
		return ErrToolTestRunExpired
	}
	return s.store.PublishToolVersion(ctx, item.ID, version.ID)
}

func (s *Service) ResolvePublishedToolVersionID(ctx context.Context, workspaceID, toolID string) (string, error) {
	// 先校验工具归属，再解析它在当前时刻可绑定的版本。
	if err := s.EnsureToolWorkspace(ctx, toolID, workspaceID); err != nil {
		return "", err
	}
	return s.store.GetToolLatestPublishedVersionID(ctx, toolID)
}
