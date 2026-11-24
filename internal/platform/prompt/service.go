package prompt

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash/crc32"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yz1qaq/asterflow/internal/domain"
	"github.com/yz1qaq/asterflow/internal/runtime/promptcache"
)

var ErrInvalidArgument = errors.New("invalid prompt argument")

type Store interface {
	CreatePromptWithInitialVersion(context.Context, *Prompt, *PromptVersion) error
	AppendPromptVersion(context.Context, string, *PromptVersion) error
	GetPromptVersion(context.Context, string, string) (*PromptVersion, error)
	ArchiveDraft(ctx context.Context, workspaceID, promptID, versionID string) error
	GetPrompt(context.Context, string, string) (*Prompt, error)
	ListPrompts(context.Context, string) ([]*Prompt, error)
	ListPromptVersions(context.Context, string, string) ([]*PromptVersion, error)

	GetEnvBinding(ctx context.Context, workspaceID, promptID, env string) (*EnvBinding, error)
	PublishEnv(ctx context.Context, workspaceID, promptID, env, versionID, action, actor string) error
	ListPromptEnvs(ctx context.Context, workspaceID, promptID string) ([]*EnvBinding, error)

	GetPromptVersionByNumber(ctx context.Context, workspaceID, promptID string, version int) (*PromptVersion, error)

	GetActiveExperiment(ctx context.Context, workspaceID, promptID, env string) (*domain.PromptExperiment, error)
	UpsertExperiment(ctx context.Context, workspaceID string, exp *domain.PromptExperiment) error
	AppendRolloutEvent(ctx context.Context, workspaceID string, event *domain.PromptRolloutEvent) error
	CompleteRollout(ctx context.Context, workspaceID string, exp *domain.PromptExperiment, event *domain.PromptRolloutEvent) error
	GetExperiment(ctx context.Context, workspaceID, experimentID string) (*domain.PromptExperiment, error)
	ListRolloutEvents(ctx context.Context, workspaceID, experimentID string) ([]*domain.PromptRolloutEvent, error)
	ListPromptEnvEvents(ctx context.Context, workspaceID, promptID, env string) ([]*domain.PromptEnvEvent, error)
}

type ModelProfileValidator interface {
	ValidateProfileVersion(context.Context, string, string) error
}

type Publisher interface {
	Publish(ctx context.Context, channel, message string) error
}

type Service struct {
	store  Store
	models ModelProfileValidator
	cache  *promptcache.Cache
	pub    Publisher
}

func NewService(store Store, models ModelProfileValidator) *Service {
	return NewServiceWithCache(store, models, promptcache.NewCache(), NoopPublisher{})
}

type NoopPublisher struct{}

func (NoopPublisher) Publish(context.Context, string, string) error { return nil }

func NewServiceWithCache(store Store, models ModelProfileValidator, cache *promptcache.Cache, pub Publisher) *Service {
	if cache == nil {
		cache = promptcache.NewCache()
	}
	if pub == nil {
		pub = NoopPublisher{}
	}
	return &Service{store: store, models: models, cache: cache, pub: pub}
}

type VersionInput struct {
	Template              string                  `json:"template"`
	VariablesSchema       string                  `json:"variables_schema"`
	ModelProfileVersionID string                  `json:"model_profile_version_id"`
	GenerationConfig      domain.GenerationConfig `json:"generation_config"`
}

type CreatePromptRequest struct {
	WorkspaceID string `json:"-"`
	CreatedBy   string `json:"-"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	VersionInput
}

type CreateVersionRequest struct {
	WorkspaceID string `json:"-"`
	CreatedBy   string `json:"-"`
	VersionInput
}

func (s *Service) prepareVersion(
	ctx context.Context, workspaceID, promptID, createdBy string, input VersionInput,
) (*PromptVersion, error) {
	versionID := uuid.NewString()
	schema := input.VariablesSchema
	if strings.TrimSpace(schema) == "" {
		schema = "{}"
	}

	compiled, err := promptcache.Compile(versionID, input.Template, schema)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidArgument, err)
	}

	modelVersionID := strings.TrimSpace(input.ModelProfileVersionID)
	if modelVersionID != "" {
		if s.models == nil {
			return nil, fmt.Errorf("model profile validator is unavailable")
		}
		//检查这个proversionVersion的profile是否属于这个workspace
		if err := s.models.ValidateProfileVersion(ctx, workspaceID, modelVersionID); err != nil {
			return nil, fmt.Errorf("validate prompt model profile version: %w", err)
		}
	}

	sum := sha256.Sum256([]byte(input.Template))
	return &PromptVersion{
		ID:                    versionID,
		PromptID:              promptID,
		Template:              input.Template,
		VariablesSchema:       schema,
		ModelProfileVersionID: modelVersionID,
		GenerationConfig:      input.GenerationConfig,
		Hash:                  fmt.Sprintf("%x", sum),
		TokenEstimate:         compiled.EstTokens,
		CreatedBy:             createdBy,
	}, nil
}

func (s *Service) CreatePrompt(
	ctx context.Context, req CreatePromptRequest,
) (*Prompt, *PromptVersion, error) {
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	name := strings.TrimSpace(req.Name)
	if workspaceID == "" || name == "" {
		return nil, nil, fmt.Errorf("%w: workspace_id and name are required", ErrInvalidArgument)
	}

	promptID := uuid.NewString()
	v, err := s.prepareVersion(ctx, workspaceID, promptID, req.CreatedBy, req.VersionInput)
	if err != nil {
		return nil, nil, err
	}

	v.Version = 1
	p := &Prompt{
		ID:          promptID,
		WorkspaceID: workspaceID,
		Name:        name,
		Category:    strings.TrimSpace(req.Category),
		CreatedBy:   req.CreatedBy,
	}
	if err := s.store.CreatePromptWithInitialVersion(ctx, p, v); err != nil {
		return nil, nil, err
	}
	return p, v, nil
}

func (s *Service) CreateVersion(
	ctx context.Context, promptID string, req CreateVersionRequest,
) (*PromptVersion, error) {
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf("%w: workspace_id is required", ErrInvalidArgument)
	}

	id, err := uuid.Parse(strings.TrimSpace(promptID))
	if err != nil {
		return nil, fmt.Errorf("%w: prompt_id must be a UUID", ErrInvalidArgument)
	}

	v, err := s.prepareVersion(ctx, workspaceID, id.String(), req.CreatedBy, req.VersionInput)
	if err != nil {
		return nil, err
	}

	if err := s.store.AppendPromptVersion(ctx, workspaceID, v); err != nil {
		return nil, err
	}
	return v, nil
}

func (s *Service) ListVersions(
	ctx context.Context, workspaceID, promptID string,
) ([]*PromptVersion, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf("%w: workspace_id is required", ErrInvalidArgument)
	}

	id, err := uuid.Parse(strings.TrimSpace(promptID))
	if err != nil {
		return nil, fmt.Errorf("%w: prompt_id must be a UUID", ErrInvalidArgument)
	}

	//要保证promot存在
	if _, err := s.store.GetPrompt(ctx, workspaceID, id.String()); err != nil {
		return nil, err
	}
	return s.store.ListPromptVersions(ctx, workspaceID, id.String())
}

func (s *Service) ArchiveDraft(ctx context.Context, workspaceID, promptID, versionID string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return fmt.Errorf("%w: workspace_id is required", ErrInvalidArgument)
	}
	pid, promptErr := uuid.Parse(strings.TrimSpace(promptID))
	vid, versionErr := uuid.Parse(strings.TrimSpace(versionID))
	if promptErr != nil || versionErr != nil {
		return fmt.Errorf("%w: prompt_id and version_id must be UUIDs", ErrInvalidArgument)
	}
	return s.store.ArchiveDraft(ctx, workspaceID, pid.String(), vid.String())
}

func (s *Service) RenderByVersion(ctx context.Context, workspaceID, promptID, versionID string, vars map[string]any) (string, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return "", fmt.Errorf("%w: workspace_id is required", ErrInvalidArgument)
	}
	pid, err := uuid.Parse(strings.TrimSpace(promptID))
	if err != nil {
		return "", fmt.Errorf("%w: prompt_id must be a UUID", ErrInvalidArgument)
	}
	vid, err := uuid.Parse(strings.TrimSpace(versionID))
	if err != nil {
		return "", fmt.Errorf("%w: version_id must be a UUID", ErrInvalidArgument)
	}

	version, err := s.store.GetPromptVersion(ctx, workspaceID, vid.String())
	if err != nil {
		return "", err
	}
	if version.PromptID != pid.String() || version.Archived {
		return "", ErrPromptVersionNotFound
	}
	return s.renderVersion(ctx, version, vars)
}

func (s *Service) ListPrompts(ctx context.Context, workspaceID string) ([]*Prompt, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf("%w: workspace_id is required", ErrInvalidArgument)
	}
	return s.store.ListPrompts(ctx, workspaceID)
}

func (s *Service) GetEnvBinding(
	ctx context.Context, workspaceID, promptID, env string,
) (*EnvBinding, error) {
	workspaceID, env = strings.TrimSpace(workspaceID), strings.TrimSpace(env)
	if workspaceID == "" || env == "" {
		return nil, fmt.Errorf("%w: workspace_id and env are required", ErrInvalidArgument)
	}
	pid, err := uuid.Parse(strings.TrimSpace(promptID))
	if err != nil {
		return nil, fmt.Errorf("%w: prompt_id must be a UUID", ErrInvalidArgument)
	}
	return s.store.GetEnvBinding(ctx, workspaceID, pid.String(), env)
}

func (s *Service) publishEnv(
	ctx context.Context, workspaceID, promptID, env, versionID, action, actor string,
) error {
	workspaceID, env = strings.TrimSpace(workspaceID), strings.TrimSpace(env)
	if workspaceID == "" || env == "" {
		return fmt.Errorf("%w: workspace_id and env are required", ErrInvalidArgument)
	}
	pid, err := uuid.Parse(strings.TrimSpace(promptID))
	if err != nil {
		return fmt.Errorf("%w: prompt_id must be a UUID", ErrInvalidArgument)
	}
	vid, err := uuid.Parse(strings.TrimSpace(versionID))
	if err != nil {
		return fmt.Errorf("%w: version_id must be a UUID", ErrInvalidArgument)
	}

	if err := s.store.PublishEnv(ctx, workspaceID, pid.String(), env, vid.String(), action, actor); err != nil {
		return err
	}
	s.cacheVersion(ctx, workspaceID, pid.String(), env, vid.String())
	return nil
}

func (s *Service) cacheVersion(ctx context.Context, workspaceID, promptID, env, versionID string) {
	version, err := s.store.GetPromptVersion(ctx, workspaceID, versionID)
	if err != nil {
		return
	}
	compiled, err := promptcache.Compile(version.ID, version.Template, version.VariablesSchema)
	if err != nil {
		return
	}
	s.cache.Put(promptID, env, compiled)
	_ = s.pub.Publish(ctx, promptcache.InvalidateChannel(promptID, env), versionID)
}

func (s *Service) Promote(
	ctx context.Context, workspaceID, promptID, env, versionID, actor string,
) error {
	return s.publishEnv(ctx, workspaceID, promptID, env, versionID, "published", actor)
}

// 分桶函数,实验+用户,分配应该用的版本ID
func selectExperimentVersion(exp *domain.PromptExperiment, userID string) string {
	if exp == nil || len(exp.Variants) == 0 {
		return ""
	}
	bucket := int(crc32.ChecksumIEEE([]byte(userID+exp.ID)) % 100)
	cumulative := 0
	for _, variant := range exp.Variants {
		cumulative += variant.Traffic
		if bucket < cumulative {
			return variant.VersionID
		}
	}
	return ""
}

func (s *Service) ResolveVersion(
	ctx context.Context, workspaceID, promptID, env, userID string,
) (string, error) {
	selection, err := s.ResolveSelection(ctx, workspaceID, promptID, env, userID)
	return selection.VersionID, err
}

func (s *Service) ResolveSelection(
	ctx context.Context, workspaceID, promptID, env, userID string,
) (domain.PromptSelection, error) {
	workspaceID, env = strings.TrimSpace(workspaceID), strings.TrimSpace(env)
	if workspaceID == "" {
		return domain.PromptSelection{}, fmt.Errorf("%w: workspace_id is required", ErrInvalidArgument)
	}
	if env == "" {
		env = "dev"
	}
	pid, err := uuid.Parse(strings.TrimSpace(promptID))
	if err != nil {
		return domain.PromptSelection{}, fmt.Errorf("%w: prompt_id must be a UUID", ErrInvalidArgument)
	}

	exp, err := s.store.GetActiveExperiment(ctx, workspaceID, pid.String(), env)
	if err != nil {
		return domain.PromptSelection{}, err
	}

	var selection domain.PromptSelection
	if exp != nil && len(exp.Variants) > 0 {
		selection.ExperimentID = exp.ID
		selection.ExperimentVariant = "baseline"
		selection.VersionID = selectExperimentVersion(exp, userID)
		if selection.VersionID != "" {
			selection.ExperimentVariant = "variant"
			if exp.CandidateVersionID != "" && exp.TrafficPercent > 0 {
				selection.ExperimentVariant = "candidate"
			}
			return selection, nil
		}
	}

	binding, err := s.GetEnvBinding(ctx, workspaceID, pid.String(), env)
	if err != nil {
		return domain.PromptSelection{}, err
	}
	selection.VersionID = binding.VersionID
	return selection, nil
}

func (s *Service) RenderByEnv(
	ctx context.Context, workspaceID, promptID, env, userID string, vars map[string]any,
) (string, error) {
	versionID, err := s.ResolveVersion(ctx, workspaceID, promptID, env, userID)
	if err != nil {
		return "", err
	}
	pid, err := uuid.Parse(strings.TrimSpace(promptID))
	if err != nil {
		return "", fmt.Errorf("%w: prompt_id must be a UUID", ErrInvalidArgument)
	}
	env = strings.TrimSpace(env)
	if env == "" {
		env = "dev"
	}
	//如果缓存有直接返回
	if compiled, ok := s.cache.Get(pid.String(), env); ok && compiled.VersionID == versionID {
		return compiled.Render(ctx, vars)
	}
	version, err := s.store.GetPromptVersion(ctx, workspaceID, versionID)
	if err != nil {
		return "", err
	}
	compiled, err := promptcache.Compile(version.ID, version.Template, version.VariablesSchema)
	if err != nil {
		return "", err
	}
	if binding, err := s.GetEnvBinding(ctx, workspaceID, pid.String(), env); err == nil && binding.VersionID == version.ID {
		s.cache.Put(pid.String(), env, compiled)
	}
	return compiled.Render(ctx, vars)
}

// 校验、返回干净的workspace promptID env
func experimentScope(workspaceID, promptID, env string) (string, string, string, error) {
	workspaceID, env = strings.TrimSpace(workspaceID), strings.TrimSpace(env)
	if workspaceID == "" || env == "" {
		return "", "", "", fmt.Errorf("%w: workspace_id and env are required", ErrInvalidArgument)
	}
	pid, err := uuid.Parse(strings.TrimSpace(promptID))
	if err != nil {
		return "", "", "", fmt.Errorf("%w: prompt_id must be a UUID", ErrInvalidArgument)
	}
	return workspaceID, pid.String(), env, nil
}

// 拼一条事件,交给Store
func (s *Service) recordRollout(
	ctx context.Context, workspaceID, experimentID, action string, from, to int, actor, detail string,
) error {
	return s.store.AppendRolloutEvent(ctx, workspaceID, &domain.PromptRolloutEvent{
		ID: uuid.NewString(), ExperimentID: experimentID, Action: action,
		FromTraffic: from, ToTraffic: to, Actor: actor, Detail: detail, CreatedAt: time.Now().UTC(),
	})
}

func (s *Service) StartExperiment(
	ctx context.Context, workspaceID, promptID, env string, variants []domain.ExperimentVariant, createdBy string,
) (*domain.PromptExperiment, error) {
	workspaceID, pid, env, err := experimentScope(workspaceID, promptID, env)
	if err != nil {
		return nil, err
	}
	if len(variants) == 0 {
		return nil, fmt.Errorf("%w: at least one variant is required", ErrInvalidArgument)
	}
	total := 0
	for _, variant := range variants {
		vid, err := uuid.Parse(strings.TrimSpace(variant.VersionID))
		if err != nil {
			return nil, fmt.Errorf("%w: variant version_id must be a UUID", ErrInvalidArgument)
		}
		if variant.Traffic < 0 {
			return nil, fmt.Errorf("%w: variant traffic must not be negative", ErrInvalidArgument)
		}
		version, err := s.store.GetPromptVersion(ctx, workspaceID, vid.String())
		if err != nil {
			return nil, err
		}
		if version.PromptID != pid {
			return nil, ErrPromptVersionNotFound
		}
		total += variant.Traffic
	}
	if total <= 0 || total > 100 {
		return nil, fmt.Errorf("%w: variant traffic must sum to between 1 and 100", ErrInvalidArgument)
	}

	exp := &domain.PromptExperiment{
		ID: uuid.NewString(), PromptID: pid, Env: env, Status: "active",
		Variants: variants, StartedAt: time.Now().UTC(), CreatedBy: createdBy,
	}
	if err := s.store.UpsertExperiment(ctx, workspaceID, exp); err != nil {
		return nil, err
	}
	_ = s.recordRollout(ctx, workspaceID, exp.ID, "started", 0, total, createdBy,
		fmt.Sprintf("multi-variant: %d variants, %d%% traffic", len(variants), total))
	return exp, nil
}

func (s *Service) StartRollout(
	ctx context.Context, workspaceID, promptID, env, candidateID, actor string, traffic int,
) (*domain.PromptExperiment, error) {
	workspaceID, pid, env, err := experimentScope(workspaceID, promptID, env)
	if err != nil {
		return nil, err
	}
	if traffic <= 0 || traffic >= 100 {
		return nil, fmt.Errorf("%w: traffic_percent must be between 1 and 99", ErrInvalidArgument)
	}
	binding, err := s.GetEnvBinding(ctx, workspaceID, pid, env)
	if err != nil {
		return nil, err
	}
	vid, err := uuid.Parse(strings.TrimSpace(candidateID))
	if err != nil {
		return nil, fmt.Errorf("%w: candidate version_id must be a UUID", ErrInvalidArgument)
	}
	candidate, err := s.store.GetPromptVersion(ctx, workspaceID, vid.String())
	if err != nil {
		return nil, err
	}
	if candidate.PromptID != pid || candidate.ID == binding.VersionID {
		return nil, fmt.Errorf("%w: candidate must be another version of this prompt", ErrInvalidArgument)
	}

	exp := &domain.PromptExperiment{
		ID: uuid.NewString(), PromptID: pid, Env: env, Status: "active",
		BaselineVersionID: binding.VersionID, CandidateVersionID: candidate.ID,
		TrafficPercent: traffic,
		Variants:       []domain.ExperimentVariant{{VersionID: candidate.ID, Traffic: traffic}},
		StartedAt:      time.Now().UTC(), CreatedBy: actor,
	}
	if err := s.store.UpsertExperiment(ctx, workspaceID, exp); err != nil {
		return nil, err
	}
	_ = s.recordRollout(ctx, workspaceID, exp.ID, "started", 0, traffic, actor, "")
	return exp, nil
}

func (s *Service) Rollback(
	ctx context.Context, workspaceID, promptID, env, versionID, actor string,
) error {
	return s.publishEnv(ctx, workspaceID, promptID, env, versionID, "rolled_back", actor)
}

func (s *Service) ListEnvEvents(
	ctx context.Context, workspaceID, promptID, env string,
) ([]*domain.PromptEnvEvent, error) {
	workspaceID, env = strings.TrimSpace(workspaceID), strings.TrimSpace(env)
	if workspaceID == "" || env == "" {
		return nil, fmt.Errorf("%w: workspace_id and env are required", ErrInvalidArgument)
	}
	pid, err := uuid.Parse(strings.TrimSpace(promptID))
	if err != nil {
		return nil, fmt.Errorf("%w: prompt_id must be a UUID", ErrInvalidArgument)
	}
	if _, err := s.store.GetPrompt(ctx, workspaceID, pid.String()); err != nil {
		return nil, err
	}
	return s.store.ListPromptEnvEvents(ctx, workspaceID, pid.String(), env)
}

func (s *Service) ListEnvBindings(
	ctx context.Context, workspaceID, promptID string,
) ([]*EnvBinding, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf("%w: workspace_id is required", ErrInvalidArgument)
	}
	pid, err := uuid.Parse(strings.TrimSpace(promptID))
	if err != nil {
		return nil, fmt.Errorf("%w: prompt_id must be a UUID", ErrInvalidArgument)
	}
	if _, err := s.store.GetPrompt(ctx, workspaceID, pid.String()); err != nil {
		return nil, err
	}
	return s.store.ListPromptEnvs(ctx, workspaceID, pid.String())
}

func (s *Service) Diff(
	ctx context.Context, workspaceID, promptID string, fromVersion, toVersion int,
) (string, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return "", fmt.Errorf("%w: workspace_id is required", ErrInvalidArgument)
	}
	if fromVersion < 1 || toVersion < 1 {
		return "", fmt.Errorf("%w: from and to must be positive version numbers", ErrInvalidArgument)
	}
	pid, err := uuid.Parse(strings.TrimSpace(promptID))
	if err != nil {
		return "", fmt.Errorf("%w: prompt_id must be a UUID", ErrInvalidArgument)
	}

	from, err := s.store.GetPromptVersionByNumber(ctx, workspaceID, pid.String(), fromVersion)
	if err != nil {
		return "", err
	}
	to, err := s.store.GetPromptVersionByNumber(ctx, workspaceID, pid.String(), toVersion)
	if err != nil {
		return "", err
	}

	if from.VariablesSchema == to.VariablesSchema {
		return UnifiedDiff(from.Template, to.Template), nil
	}
	return "--- template\n" + UnifiedDiff(from.Template, to.Template) +
		"--- variables_schema\n" + UnifiedDiff(from.VariablesSchema, to.VariablesSchema), nil
}

func activeRollout(
	ctx context.Context, store Store, workspaceID, promptID, env string,
) (*domain.PromptExperiment, error) {
	exp, err := store.GetActiveExperiment(ctx, workspaceID, promptID, env)
	if err != nil {
		return nil, err
	}
	if exp == nil {
		return nil, fmt.Errorf("%w: active experiment not found", ErrInvalidArgument)
	}
	return exp, nil
}

func (s *Service) UpdateRolloutTraffic(
	ctx context.Context, workspaceID, promptID, env string, traffic int, actor string,
) (*domain.PromptExperiment, error) {
	workspaceID, pid, env, err := experimentScope(workspaceID, promptID, env)
	if err != nil {
		return nil, err
	}
	if traffic <= 0 || traffic >= 100 {
		return nil, fmt.Errorf("%w: traffic_percent must be between 1 and 99", ErrInvalidArgument)
	}
	exp, err := activeRollout(ctx, s.store, workspaceID, pid, env)
	if err != nil {
		return nil, err
	}
	if exp.CandidateVersionID == "" {
		return nil, fmt.Errorf("%w: active experiment is not a rollout", ErrInvalidArgument)
	}
	from := exp.TrafficPercent
	exp.TrafficPercent = traffic
	exp.Variants = []domain.ExperimentVariant{{VersionID: exp.CandidateVersionID, Traffic: traffic}}
	if err := s.store.UpsertExperiment(ctx, workspaceID, exp); err != nil {
		return nil, err
	}
	_ = s.recordRollout(ctx, workspaceID, exp.ID, "traffic_changed", from, traffic, actor, "")
	return exp, nil
}

func (s *Service) CompleteRollout(
	ctx context.Context, workspaceID, promptID, env, actor string,
) error {
	workspaceID, pid, env, err := experimentScope(workspaceID, promptID, env)
	if err != nil {
		return err
	}
	exp, err := activeRollout(ctx, s.store, workspaceID, pid, env)
	if err != nil {
		return err
	}
	if exp.CandidateVersionID == "" {
		return fmt.Errorf("%w: active experiment is not a rollout", ErrInvalidArgument)
	}
	if _, err := s.store.GetPromptVersion(ctx, workspaceID, exp.CandidateVersionID); err != nil {
		return err
	}

	now := time.Now().UTC()
	previous := exp.TrafficPercent
	exp.Status = "completed"
	exp.TrafficPercent = 100
	exp.CompletedAt = &now
	event := &domain.PromptRolloutEvent{
		ID: uuid.NewString(), ExperimentID: exp.ID, Action: "completed",
		FromTraffic: previous, ToTraffic: 100, Actor: actor,
		Detail: "candidate became baseline", CreatedAt: now,
	}
	if err := s.store.CompleteRollout(ctx, workspaceID, exp, event); err != nil {
		return err
	}
	s.cacheVersion(ctx, workspaceID, pid, env, exp.CandidateVersionID)
	return nil
}

func (s *Service) RollbackRollout(
	ctx context.Context, workspaceID, promptID, env, actor string,
) error {
	workspaceID, pid, env, err := experimentScope(workspaceID, promptID, env)
	if err != nil {
		return err
	}
	exp, err := activeRollout(ctx, s.store, workspaceID, pid, env)
	if err != nil {
		return err
	}
	from := exp.TrafficPercent
	detail := ""
	if exp.CandidateVersionID == "" {
		from = 0
		for _, variant := range exp.Variants {
			from += variant.Traffic
		}
		detail = fmt.Sprintf("multi-variant experiment stopped (%d variants)", len(exp.Variants))
	}
	now := time.Now().UTC()
	exp.Status = "rolled_back"
	exp.TrafficPercent = 0
	exp.CompletedAt = &now
	if err := s.store.UpsertExperiment(ctx, workspaceID, exp); err != nil {
		return err
	}
	_ = s.recordRollout(ctx, workspaceID, exp.ID, "rolled_back", from, 0, actor, detail)
	return nil
}

func (s *Service) GetActiveExperiment(
	ctx context.Context, workspaceID, promptID, env string,
) (*domain.PromptExperiment, error) {
	workspaceID, pid, env, err := experimentScope(workspaceID, promptID, env)
	if err != nil {
		return nil, err
	}
	if _, err := s.store.GetPrompt(ctx, workspaceID, pid); err != nil {
		return nil, err
	}
	return s.store.GetActiveExperiment(ctx, workspaceID, pid, env)
}

func (s *Service) ListRolloutEvents(
	ctx context.Context, workspaceID, experimentID string,
) ([]*domain.PromptRolloutEvent, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf("%w: workspace_id is required", ErrInvalidArgument)
	}
	id, err := uuid.Parse(strings.TrimSpace(experimentID))
	if err != nil {
		return nil, fmt.Errorf("%w: experiment_id must be a UUID", ErrInvalidArgument)
	}
	if _, err := s.store.GetExperiment(ctx, workspaceID, id.String()); err != nil {
		return nil, err
	}
	return s.store.ListRolloutEvents(ctx, workspaceID, id.String())
}

// 确保version存在
func (s *Service) EnsureVersion(ctx context.Context, workspaceID, versionID string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return fmt.Errorf("%w: workspace_id is required", ErrInvalidArgument)
	}
	vid, err := uuid.Parse(strings.TrimSpace(versionID))
	if err != nil {
		return fmt.Errorf("%w: version_id must be a UUID", ErrInvalidArgument)
	}
	version, err := s.store.GetPromptVersion(ctx, workspaceID, vid.String())
	if err != nil {
		return err
	}
	if version.Archived {
		return ErrPromptVersionNotFound
	}
	return nil
}

func (s *Service) RenderVersionByID(ctx context.Context, workspaceID, versionID string, vars map[string]any) (string, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return "", fmt.Errorf("%w: workspace_id is required", ErrInvalidArgument)
	}
	vid, err := uuid.Parse(strings.TrimSpace(versionID))
	if err != nil {
		return "", fmt.Errorf("%w: version_id must be a UUID", ErrInvalidArgument)
	}
	version, err := s.store.GetPromptVersion(ctx, workspaceID, vid.String())
	if err != nil {
		return "", err
	}
	return s.renderVersion(ctx, version, vars)
}

func (s *Service) EnsurePrompt(ctx context.Context, workspaceID, promptID string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return fmt.Errorf("%w: workspace_id is required", ErrInvalidArgument)
	}
	pid, err := uuid.Parse(strings.TrimSpace(promptID))
	if err != nil {
		return fmt.Errorf("%w: prompt_id must be a UUID", ErrInvalidArgument)
	}
	_, err = s.store.GetPrompt(ctx, workspaceID, pid.String())
	return err
}

// renderVersion compiles and renders an already-loaded version.
func (s *Service) renderVersion(ctx context.Context, version *PromptVersion, vars map[string]any) (string, error) {
	compiled, err := promptcache.Compile(version.ID, version.Template, version.VariablesSchema)
	if err != nil {
		return "", fmt.Errorf("compile stored prompt version: %w", err)
	}
	text, err := compiled.Render(ctx, vars)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", fmt.Errorf("%w: %v", ErrInvalidArgument, err)
	}
	return text, nil
}
