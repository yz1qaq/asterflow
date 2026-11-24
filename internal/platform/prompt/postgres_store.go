package prompt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yz1qaq/asterflow/internal/domain"
	pgstore "github.com/yz1qaq/asterflow/internal/infrastructure/postgres/sqlc"
)

type PostgresStore struct {
	db *pgxpool.Pool
	q  *pgstore.Queries
}

var (
	ErrPromptNotFound        = errors.New("prompt not found")
	ErrPromptVersionNotFound = errors.New("prompt version not found")
	ErrPromptVersionInUse    = errors.New("prompt version is published or referenced")
	ErrPromptEnvNotBound     = errors.New("prompt environment is not bound")

	ErrActivePromptExperiment   = errors.New("another active prompt experiment exists in this environment")
	ErrPromptExperimentNotFound = errors.New("prompt experiment not found")
)

func NewPostgresStore(db *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{db: db, q: pgstore.New(db)}
}

func promptFromRow(row pgstore.Prompt) *Prompt {
	return &Prompt{
		ID:          row.ID.String(),
		WorkspaceID: row.WorkspaceID,
		Name:        row.Name,
		Category:    row.Category,
		CreatedBy:   row.CreatedBy,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}

func promptVersionFromRow(row pgstore.PromptVersion) (*PromptVersion, error) {
	var generation domain.GenerationConfig
	if err := json.Unmarshal(row.GenerationConfig, &generation); err != nil {
		return nil, fmt.Errorf("decode prompt generation config: %w", err)
	}

	var modelVersionID string
	if row.ModelProfileVersionID.Valid {
		modelVersionID = uuid.UUID(row.ModelProfileVersionID.Bytes).String()
	}

	return &PromptVersion{
		Archived:              row.IsArchived,
		ID:                    row.ID.String(),
		PromptID:              row.PromptID.String(),
		Version:               int(row.Version),
		Template:              row.Template,
		VariablesSchema:       row.VariablesSchema,
		ModelProfileVersionID: modelVersionID,
		GenerationConfig:      generation,
		Hash:                  row.Hash,
		TokenEstimate:         int(row.TokenEstimate),
		CreatedBy:             row.CreatedBy,
		CreatedAt:             row.CreatedAt,
	}, nil
}

// 解析参数
func promptVersionParams(v *PromptVersion) (pgstore.CreatePromptVersionParams, error) {
	var params pgstore.CreatePromptVersionParams

	versionID, err := uuid.Parse(v.ID)
	if err != nil {
		return params, fmt.Errorf("parse prompt version id: %w", err)
	}
	promptID, err := uuid.Parse(v.PromptID)
	if err != nil {
		return params, fmt.Errorf("parse prompt id: %w", err)
	}
	if v.Version < 1 || v.Version > math.MaxInt32 {
		return params, fmt.Errorf("prompt version number is out of range")
	}
	if v.TokenEstimate < 0 || v.TokenEstimate > math.MaxInt32 {
		return params, fmt.Errorf("prompt token estimate is out of range")
	}

	var modelVersionID pgtype.UUID
	if v.ModelProfileVersionID != "" {
		id, err := uuid.Parse(v.ModelProfileVersionID)
		if err != nil {
			return params, fmt.Errorf("parse model profile version id: %w", err)
		}
		modelVersionID = pgtype.UUID{Bytes: id, Valid: true}
	}
	config, err := json.Marshal(v.GenerationConfig)
	if err != nil {
		return params, fmt.Errorf("encode prompt generation config: %w", err)
	}
	return pgstore.CreatePromptVersionParams{
		ID:                    versionID,
		PromptID:              promptID,
		Version:               int32(v.Version),
		Template:              v.Template,
		VariablesSchema:       v.VariablesSchema,
		ModelProfileVersionID: modelVersionID,
		GenerationConfig:      config,
		Hash:                  v.Hash,
		TokenEstimate:         int32(v.TokenEstimate),
		CreatedBy:             v.CreatedBy,
	}, nil
}

func (s *PostgresStore) CreatePromptWithInitialVersion(
	ctx context.Context,
	p *Prompt,
	v *PromptVersion,
) error {
	if p == nil || v == nil {
		return fmt.Errorf("prompt and initial version are required")
	}

	promptID, err := uuid.Parse(p.ID)
	if err != nil {
		return fmt.Errorf("parse prompt id: %w", err)
	}
	params, err := promptVersionParams(v)
	if err != nil {
		return err
	}
	if params.PromptID != promptID || params.Version != 1 {
		return fmt.Errorf("initial version must belong to the prompt and have version number 1")
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin prompt creation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	promptRow, err := q.CreatePrompt(ctx, pgstore.CreatePromptParams{
		ID:          promptID,
		WorkspaceID: p.WorkspaceID,
		Name:        p.Name,
		Category:    p.Category,
		CreatedBy:   p.CreatedBy,
	})
	if err != nil {
		return fmt.Errorf("create prompt: %w", err)
	}

	versionRow, err := q.CreatePromptVersion(ctx, params)
	if err != nil {
		return fmt.Errorf("create initial prompt version: %w", err)
	}

	if _, err := q.UpsertPromptEnv(ctx, pgstore.UpsertPromptEnvParams{
		PromptID:  promptRow.ID,
		Env:       "dev",
		VersionID: versionRow.ID,
	}); err != nil {
		return fmt.Errorf("bind initial prompt version to dev: %w", err)
	}

	savedVersion, err := promptVersionFromRow(versionRow)
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit prompt creation: %w", err)
	}

	*p = *promptFromRow(promptRow)
	*v = *savedVersion
	return nil
}

func (s *PostgresStore) GetPrompt(
	ctx context.Context, workspaceID, promptID string,
) (*Prompt, error) {
	id, err := uuid.Parse(promptID)
	if err != nil {
		return nil, fmt.Errorf("parse prompt id: %w", err)
	}
	row, err := s.q.GetPrompt(ctx, pgstore.GetPromptParams{
		PromptID: id, WorkspaceID: workspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPromptNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get prompt: %w", err)
	}
	return promptFromRow(row), nil
}

func (s *PostgresStore) ListPrompts(ctx context.Context, workspaceID string) ([]*Prompt, error) {
	rows, err := s.q.ListPrompts(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list prompts: %w", err)
	}
	prompts := make([]*Prompt, 0, len(rows))
	for _, row := range rows {
		prompts = append(prompts, promptFromRow(row))
	}
	return prompts, nil
}

func (s *PostgresStore) GetPromptVersion(
	ctx context.Context, workspaceID, versionID string,
) (*PromptVersion, error) {
	id, err := uuid.Parse(versionID)
	if err != nil {
		return nil, fmt.Errorf("parse prompt version id: %w", err)
	}
	row, err := s.q.GetPromptVersion(ctx, pgstore.GetPromptVersionParams{
		VersionID: id, WorkspaceID: workspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPromptVersionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get prompt version: %w", err)
	}
	return promptVersionFromRow(row)
}

func (s *PostgresStore) ListPromptVersions(
	ctx context.Context, workspaceID, promptID string,
) ([]*PromptVersion, error) {
	id, err := uuid.Parse(promptID)
	if err != nil {
		return nil, fmt.Errorf("parse prompt id: %w", err)
	}
	rows, err := s.q.ListPromptVersions(ctx, pgstore.ListPromptVersionsParams{
		PromptID: id, WorkspaceID: workspaceID,
	})
	if err != nil {
		return nil, fmt.Errorf("list prompt versions: %w", err)
	}

	versions := make([]*PromptVersion, 0, len(rows))
	for _, row := range rows {
		version, err := promptVersionFromRow(row)
		if err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}
	return versions, nil
}

func (s *PostgresStore) AppendPromptVersion(
	ctx context.Context, workspaceID string, v *PromptVersion,
) error {
	if v == nil {
		return fmt.Errorf("prompt version is required")
	}
	promptID, err := uuid.Parse(v.PromptID)
	if err != nil {
		return fmt.Errorf("parse prompt id: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin prompt version append: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	if _, err := q.LockPrompt(ctx, pgstore.LockPromptParams{
		PromptID: promptID, WorkspaceID: workspaceID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPromptNotFound
		}
		return fmt.Errorf("lock prompt: %w", err)
	}

	next, err := q.NextPromptVersion(ctx, promptID)
	if err != nil {
		return fmt.Errorf("allocate prompt version number: %w", err)
	}
	if next < 1 || next > math.MaxInt32 {
		return fmt.Errorf("prompt version number is out of range")
	}

	draft := *v
	draft.Version = int(next)
	params, err := promptVersionParams(&draft)
	if err != nil {
		return err
	}

	row, err := q.CreatePromptVersion(ctx, params)
	if err != nil {
		return fmt.Errorf("append prompt version: %w", err)
	}
	saved, err := promptVersionFromRow(row)
	if err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit prompt version append: %w", err)
	}
	*v = *saved
	return nil
}

func (s *PostgresStore) GetEnvBinding(
	ctx context.Context, workspaceID, promptID, env string,
) (*EnvBinding, error) {
	id, err := uuid.Parse(promptID)
	if err != nil {
		return nil, fmt.Errorf("parse prompt id: %w", err)
	}
	row, err := s.q.GetPromptEnv(ctx, pgstore.GetPromptEnvParams{
		PromptID: id, WorkspaceID: workspaceID, Env: env,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := s.GetPrompt(ctx, workspaceID, promptID); err != nil {
			return nil, err
		}
		return nil, ErrPromptEnvNotBound
	}
	if err != nil {
		return nil, fmt.Errorf("get prompt environment: %w", err)
	}
	return &EnvBinding{
		PromptID:  row.PromptID.String(),
		Env:       row.Env,
		VersionID: row.VersionID.String(),
		UpdatedAt: row.UpdatedAt,
	}, nil
}

func (s *PostgresStore) SetEnvBinding(
	ctx context.Context, workspaceID, promptID, env, versionID string,
) error {
	pid, err := uuid.Parse(promptID)
	if err != nil {
		return fmt.Errorf("parse prompt id: %w", err)
	}
	vid, err := uuid.Parse(versionID)
	if err != nil {
		return fmt.Errorf("parse prompt version id: %w", err)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin prompt environment update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	if _, err := q.LockPrompt(ctx, pgstore.LockPromptParams{
		PromptID: pid, WorkspaceID: workspaceID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPromptNotFound
		}
		return fmt.Errorf("lock prompt: %w", err)
	}

	version, err := q.GetPromptVersion(ctx, pgstore.GetPromptVersionParams{
		VersionID: vid, WorkspaceID: workspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrPromptVersionNotFound
	}
	if err != nil {
		return fmt.Errorf("get target prompt version: %w", err)
	}
	if version.PromptID != pid || version.IsArchived {
		return ErrPromptVersionNotFound
	}

	if _, err := q.UpsertPromptEnv(ctx, pgstore.UpsertPromptEnvParams{
		PromptID: pid, Env: env, VersionID: vid,
	}); err != nil {
		return fmt.Errorf("set prompt environment: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit prompt environment update: %w", err)
	}
	return nil
}

// 列出某prompt全部环境绑定
func (s *PostgresStore) ListPromptEnvs(
	ctx context.Context, workspaceID, promptID string,
) ([]*EnvBinding, error) {
	id, err := uuid.Parse(promptID)
	if err != nil {
		return nil, fmt.Errorf("parse prompt id: %w", err)
	}
	rows, err := s.q.ListPromptEnvs(ctx, pgstore.ListPromptEnvsParams{
		PromptID: id, WorkspaceID: workspaceID,
	})
	if err != nil {
		return nil, fmt.Errorf("list prompt environments: %w", err)
	}
	bindings := make([]*EnvBinding, 0, len(rows))
	for _, row := range rows {
		bindings = append(bindings, &EnvBinding{
			PromptID:  row.PromptID.String(),
			Env:       row.Env,
			VersionID: row.VersionID.String(),
			UpdatedAt: row.UpdatedAt,
		})
	}
	return bindings, nil
}

func (s *PostgresStore) GetPromptVersionByNumber(
	ctx context.Context, workspaceID, promptID string, version int,
) (*PromptVersion, error) {
	pid, err := uuid.Parse(promptID)
	if err != nil {
		return nil, fmt.Errorf("parse prompt id: %w", err)
	}
	if version < 1 || version > math.MaxInt32 {
		return nil, fmt.Errorf("prompt version number is out of range")
	}
	row, err := s.q.GetPromptVersionByNumber(ctx, pgstore.GetPromptVersionByNumberParams{
		PromptID: pid, WorkspaceID: workspaceID, Version: int32(version),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPromptVersionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get prompt version by number: %w", err)
	}
	return promptVersionFromRow(row)
}

func experimentFromRow(row pgstore.PromptExperiment) (*domain.PromptExperiment, error) {
	var variants []domain.ExperimentVariant
	if err := json.Unmarshal(row.Variants, &variants); err != nil {
		return nil, fmt.Errorf("decode prompt experiment variants: %w", err)
	}
	exp := &domain.PromptExperiment{
		ID:             row.ID.String(),
		PromptID:       row.PromptID.String(),
		Env:            row.Env,
		Status:         row.Status,
		TrafficPercent: int(row.TrafficPercent),
		Variants:       variants,
		CreatedBy:      row.CreatedBy,
		CreatedAt:      row.CreatedAt,
	}
	if row.BaselineVersionID.Valid {
		exp.BaselineVersionID = uuid.UUID(row.BaselineVersionID.Bytes).String()
	}
	if row.CandidateVersionID.Valid {
		exp.CandidateVersionID = uuid.UUID(row.CandidateVersionID.Bytes).String()
	}
	if row.StartedAt.Valid {
		exp.StartedAt = row.StartedAt.Time
	}
	if row.CompletedAt.Valid {
		completed := row.CompletedAt.Time
		exp.CompletedAt = &completed
	}
	return exp, nil
}

func experimentParams(exp *domain.PromptExperiment) (pgstore.UpsertPromptExperimentParams, error) {
	var params pgstore.UpsertPromptExperimentParams

	id, err := uuid.Parse(exp.ID)
	if err != nil {
		return params, fmt.Errorf("parse experiment id: %w", err)
	}
	promptID, err := uuid.Parse(exp.PromptID)
	if err != nil {
		return params, fmt.Errorf("parse prompt id: %w", err)
	}
	switch exp.Status {
	case "active", "completed", "rolled_back", "stopped":
	default:
		return params, fmt.Errorf("unsupported experiment status %q", exp.Status)
	}
	if exp.TrafficPercent < 0 || exp.TrafficPercent > 100 {
		return params, fmt.Errorf("experiment traffic must be between 0 and 100")
	}
	total := 0
	for _, variant := range exp.Variants {
		if _, err := uuid.Parse(variant.VersionID); err != nil {
			return params, fmt.Errorf("parse variant version id: %w", err)
		}
		if variant.Traffic < 0 || variant.Traffic > 100 {
			return params, fmt.Errorf("variant traffic must be between 0 and 100")
		}
		total += variant.Traffic
	}
	if total > 100 {
		return params, fmt.Errorf("variant traffic sums to %d (>100)", total)
	}

	variants, err := json.Marshal(exp.Variants)
	if err != nil {
		return params, fmt.Errorf("encode experiment variants: %w", err)
	}

	var baseline, candidate pgtype.UUID
	if exp.BaselineVersionID != "" {
		parsed, err := uuid.Parse(exp.BaselineVersionID)
		if err != nil {
			return params, fmt.Errorf("parse baseline version id: %w", err)
		}
		baseline = pgtype.UUID{Bytes: parsed, Valid: true}
	}
	if exp.CandidateVersionID != "" {
		parsed, err := uuid.Parse(exp.CandidateVersionID)
		if err != nil {
			return params, fmt.Errorf("parse candidate version id: %w", err)
		}
		candidate = pgtype.UUID{Bytes: parsed, Valid: true}
	}
	if baseline.Valid && candidate.Valid && baseline.Bytes == candidate.Bytes {
		return params, fmt.Errorf("candidate version must differ from the baseline")
	}

	var started, completed pgtype.Timestamptz
	if !exp.StartedAt.IsZero() {
		started = pgtype.Timestamptz{Time: exp.StartedAt, Valid: true}
	}
	if exp.CompletedAt != nil {
		completed = pgtype.Timestamptz{Time: *exp.CompletedAt, Valid: true}
	}

	return pgstore.UpsertPromptExperimentParams{
		ID:                 id,
		PromptID:           promptID,
		Env:                strings.TrimSpace(exp.Env),
		Status:             exp.Status,
		BaselineVersionID:  baseline,
		CandidateVersionID: candidate,
		TrafficPercent:     int32(exp.TrafficPercent),
		Variants:           variants,
		StartedAt:          started,
		CompletedAt:        completed,
		CreatedBy:          exp.CreatedBy,
	}, nil
}

// 查某个环境正在跑什么实验
func (s *PostgresStore) GetActiveExperiment(
	ctx context.Context, workspaceID, promptID, env string,
) (*domain.PromptExperiment, error) {
	pid, err := uuid.Parse(promptID)
	if err != nil {
		return nil, fmt.Errorf("parse prompt id: %w", err)
	}
	row, err := s.q.GetActivePromptExperiment(ctx, pgstore.GetActivePromptExperimentParams{
		PromptID: pid, WorkspaceID: workspaceID, Env: env,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get active prompt experiment: %w", err)
	}
	return experimentFromRow(row)
}

func (s *PostgresStore) UpsertExperiment(
	ctx context.Context, workspaceID string, exp *domain.PromptExperiment,
) error {
	if exp == nil {
		return fmt.Errorf("prompt experiment is required")
	}
	if _, err := s.GetPrompt(ctx, workspaceID, exp.PromptID); err != nil {
		return err
	}
	params, err := experimentParams(exp)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if _, err := q.LockPrompt(ctx, pgstore.LockPromptParams{WorkspaceID: workspaceID, PromptID: params.PromptID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPromptNotFound
		}
		return err
	}
	for _, variant := range exp.Variants {
		id, err := uuid.Parse(variant.VersionID)
		if err != nil {
			return err
		}
		v, err := q.GetPromptVersion(ctx, pgstore.GetPromptVersionParams{WorkspaceID: workspaceID, VersionID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPromptVersionNotFound
		}
		if err != nil {
			return err
		}
		if v.PromptID != params.PromptID || v.IsArchived {
			return ErrPromptVersionNotFound
		}
	}
	row, err := q.UpsertPromptExperiment(ctx, params)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrActivePromptExperiment
	}
	if err != nil {
		return fmt.Errorf("upsert prompt experiment: %w", err)
	}
	saved, err := experimentFromRow(row)
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	*exp = *saved
	return nil
}

func rolloutEventParams(event *domain.PromptRolloutEvent) (pgstore.InsertPromptRolloutEventParams, error) {
	var params pgstore.InsertPromptRolloutEventParams
	if event == nil {
		return params, fmt.Errorf("prompt rollout event is required")
	}
	id, err := uuid.Parse(event.ID)
	if err != nil {
		return params, fmt.Errorf("parse rollout event id: %w", err)
	}
	experimentID, err := uuid.Parse(event.ExperimentID)
	if err != nil {
		return params, fmt.Errorf("parse experiment id: %w", err)
	}
	if strings.TrimSpace(event.Action) == "" {
		return params, fmt.Errorf("rollout event action is required")
	}
	if event.FromTraffic < 0 || event.FromTraffic > 100 || event.ToTraffic < 0 || event.ToTraffic > 100 {
		return params, fmt.Errorf("rollout traffic must be between 0 and 100")
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	return pgstore.InsertPromptRolloutEventParams{
		ID:           id,
		ExperimentID: experimentID,
		Action:       event.Action,
		FromTraffic:  int32(event.FromTraffic),
		ToTraffic:    int32(event.ToTraffic),
		Actor:        event.Actor,
		Detail:       event.Detail,
	}, nil
}

// 记录一次操作
func (s *PostgresStore) AppendRolloutEvent(
	ctx context.Context, workspaceID string, event *domain.PromptRolloutEvent,
) error {
	params, err := rolloutEventParams(event)
	if err != nil {
		return err
	}

	if _, err := s.q.LockPromptExperiment(ctx, pgstore.LockPromptExperimentParams{
		ExperimentID: params.ExperimentID, WorkspaceID: workspaceID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPromptExperimentNotFound
		}
		return fmt.Errorf("lock prompt experiment: %w", err)
	}

	if _, err := s.q.InsertPromptRolloutEvent(ctx, params); err != nil {
		return fmt.Errorf("insert prompt rollout event: %w", err)
	}
	return nil
}

func (s *PostgresStore) CompleteRollout(
	ctx context.Context, workspaceID string, exp *domain.PromptExperiment, event *domain.PromptRolloutEvent,
) error {
	if exp == nil {
		return fmt.Errorf("prompt experiment is required")
	}
	promptID, err := uuid.Parse(exp.PromptID)
	if err != nil {
		return fmt.Errorf("parse prompt id: %w", err)
	}
	candidateID, err := uuid.Parse(exp.CandidateVersionID)
	if err != nil {
		return fmt.Errorf("parse candidate version id: %w", err)
	}
	experimentID, err := uuid.Parse(exp.ID)
	if err != nil {
		return fmt.Errorf("parse experiment id: %w", err)
	}
	eventParams, err := rolloutEventParams(event)
	if err != nil {
		return err
	}

	completedAt := time.Now().UTC()
	if exp.CompletedAt != nil {
		completedAt = *exp.CompletedAt
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin rollout completion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	if _, err := q.LockPrompt(ctx, pgstore.LockPromptParams{
		PromptID: promptID, WorkspaceID: workspaceID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPromptNotFound
		}
		return fmt.Errorf("lock prompt: %w", err)
	}
	version, err := q.GetPromptVersion(ctx, pgstore.GetPromptVersionParams{
		VersionID: candidateID, WorkspaceID: workspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrPromptVersionNotFound
	}
	if err != nil {
		return fmt.Errorf("get candidate version: %w", err)
	}
	if version.PromptID != promptID || version.IsArchived {
		return ErrPromptVersionNotFound
	}
	if _, err := q.LockPromptExperiment(ctx, pgstore.LockPromptExperimentParams{
		ExperimentID: experimentID, WorkspaceID: workspaceID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPromptExperimentNotFound
		}
		return fmt.Errorf("lock prompt experiment: %w", err)
	}

	if _, err := q.UpsertPromptEnv(ctx, pgstore.UpsertPromptEnvParams{
		PromptID: promptID, Env: exp.Env, VersionID: candidateID,
	}); err != nil {
		return fmt.Errorf("promote rollout candidate: %w", err)
	}

	row, err := q.MarkPromptExperimentCompleted(ctx, pgstore.MarkPromptExperimentCompletedParams{
		ID:          experimentID,
		CompletedAt: pgtype.Timestamptz{Time: completedAt, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrPromptExperimentNotFound
	}
	if err != nil {
		return fmt.Errorf("complete prompt experiment: %w", err)
	}
	var baseline pgtype.UUID
	if exp.BaselineVersionID != "" {
		parsed, err := uuid.Parse(exp.BaselineVersionID)
		if err != nil {
			return fmt.Errorf("parse baseline version id: %w", err)
		}
		baseline = pgtype.UUID{Bytes: parsed, Valid: true}
	}
	if _, err := q.InsertPromptEnvEvent(ctx, pgstore.InsertPromptEnvEventParams{
		ID:            uuid.New(),
		PromptID:      promptID,
		Env:           exp.Env,
		Action:        "published",
		FromVersionID: baseline,
		ToVersionID:   candidateID,
		Actor:         event.Actor,
		Detail:        "rollout completed",
	}); err != nil {
		return fmt.Errorf("record rollout promotion: %w", err)
	}

	if _, err := q.InsertPromptRolloutEvent(ctx, eventParams); err != nil {
		return fmt.Errorf("insert prompt rollout event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit rollout completion: %w", err)
	}

	saved, err := experimentFromRow(row)
	if err != nil {
		return err
	}
	*exp = *saved
	return nil
}

func (s *PostgresStore) GetExperiment(
	ctx context.Context, workspaceID, experimentID string,
) (*domain.PromptExperiment, error) {
	id, err := uuid.Parse(experimentID)
	if err != nil {
		return nil, fmt.Errorf("parse experiment id: %w", err)
	}
	row, err := s.q.GetPromptExperiment(ctx, pgstore.GetPromptExperimentParams{
		ExperimentID: id, WorkspaceID: workspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPromptExperimentNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get prompt experiment: %w", err)
	}
	return experimentFromRow(row)
}

func (s *PostgresStore) ListRolloutEvents(
	ctx context.Context, workspaceID, experimentID string,
) ([]*domain.PromptRolloutEvent, error) {
	id, err := uuid.Parse(experimentID)
	if err != nil {
		return nil, fmt.Errorf("parse experiment id: %w", err)
	}
	rows, err := s.q.ListPromptRolloutEvents(ctx, pgstore.ListPromptRolloutEventsParams{
		ExperimentID: id, WorkspaceID: workspaceID,
	})
	if err != nil {
		return nil, fmt.Errorf("list prompt rollout events: %w", err)
	}
	events := make([]*domain.PromptRolloutEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, &domain.PromptRolloutEvent{
			ID:           row.ID.String(),
			ExperimentID: row.ExperimentID.String(),
			Action:       row.Action,
			FromTraffic:  int(row.FromTraffic),
			ToTraffic:    int(row.ToTraffic),
			Actor:        row.Actor,
			Detail:       row.Detail,
			CreatedAt:    row.CreatedAt,
		})
	}
	return events, nil
}

// PublishEnv moves one environment pointer and records the operation in the same transaction.
func (s *PostgresStore) PublishEnv(
	ctx context.Context, workspaceID, promptID, env, versionID, action, actor string,
) error {
	switch action {
	case "published", "rolled_back":
	default:
		return fmt.Errorf("unsupported environment action %q", action)
	}
	pid, err := uuid.Parse(promptID)
	if err != nil {
		return fmt.Errorf("parse prompt id: %w", err)
	}
	vid, err := uuid.Parse(versionID)
	if err != nil {
		return fmt.Errorf("parse prompt version id: %w", err)
	}
	env = strings.TrimSpace(env)
	if env == "" {
		return fmt.Errorf("prompt environment is required")
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin prompt environment update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	if _, err := q.LockPrompt(ctx, pgstore.LockPromptParams{
		PromptID: pid, WorkspaceID: workspaceID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPromptNotFound
		}
		return fmt.Errorf("lock prompt: %w", err)
	}

	version, err := q.GetPromptVersion(ctx, pgstore.GetPromptVersionParams{
		VersionID: vid, WorkspaceID: workspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrPromptVersionNotFound
	}
	if err != nil {
		return fmt.Errorf("get target prompt version: %w", err)
	}
	if version.PromptID != pid || version.IsArchived {
		return ErrPromptVersionNotFound
	}

	var from pgtype.UUID
	current, err := q.GetPromptEnv(ctx, pgstore.GetPromptEnvParams{
		PromptID: pid, WorkspaceID: workspaceID, Env: env,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return fmt.Errorf("get current prompt environment: %w", err)
	default:
		from = pgtype.UUID{Bytes: current.VersionID, Valid: true}
	}

	if _, err := q.UpsertPromptEnv(ctx, pgstore.UpsertPromptEnvParams{
		PromptID: pid, Env: env, VersionID: vid,
	}); err != nil {
		return fmt.Errorf("set prompt environment: %w", err)
	}

	if _, err := q.InsertPromptEnvEvent(ctx, pgstore.InsertPromptEnvEventParams{
		ID:            uuid.New(),
		PromptID:      pid,
		Env:           env,
		Action:        action,
		FromVersionID: from,
		ToVersionID:   vid,
		Actor:         actor,
		Detail:        "",
	}); err != nil {
		return fmt.Errorf("insert prompt environment event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit prompt environment update: %w", err)
	}
	return nil
}

func envEventFromRow(row pgstore.PromptEnvEvent) *domain.PromptEnvEvent {
	event := &domain.PromptEnvEvent{
		ID:          row.ID.String(),
		PromptID:    row.PromptID.String(),
		Env:         row.Env,
		Action:      row.Action,
		ToVersionID: row.ToVersionID.String(),
		Actor:       row.Actor,
		Detail:      row.Detail,
		CreatedAt:   row.CreatedAt,
	}
	if row.FromVersionID.Valid {
		event.FromVersionID = uuid.UUID(row.FromVersionID.Bytes).String()
	}
	return event
}

func (s *PostgresStore) ListPromptEnvEvents(
	ctx context.Context, workspaceID, promptID, env string,
) ([]*domain.PromptEnvEvent, error) {
	pid, err := uuid.Parse(promptID)
	if err != nil {
		return nil, fmt.Errorf("parse prompt id: %w", err)
	}
	rows, err := s.q.ListPromptEnvEvents(ctx, pgstore.ListPromptEnvEventsParams{
		PromptID: pid, WorkspaceID: workspaceID, Env: env,
	})
	if err != nil {
		return nil, fmt.Errorf("list prompt environment events: %w", err)
	}
	events := make([]*domain.PromptEnvEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, envEventFromRow(row))
	}
	return events, nil
}

func (s *PostgresStore) ArchiveDraft(ctx context.Context, workspaceID, promptID, versionID string) error {
	pid, err := uuid.Parse(promptID)
	if err != nil {
		return err
	}
	vid, err := uuid.Parse(versionID)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin prompt draft archival: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if _, err := q.LockPrompt(ctx, pgstore.LockPromptParams{PromptID: pid, WorkspaceID: workspaceID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPromptNotFound
		}
		return err
	}
	archived, err := q.LockPromptVersionForArchive(ctx, pgstore.LockPromptVersionForArchiveParams{PromptID: pid, VersionID: vid})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrPromptVersionNotFound
	}
	if err != nil {
		return err
	}
	if archived {
		return ErrPromptVersionNotFound
	}
	n, err := q.ArchiveUnusedPromptDraft(ctx, pgstore.ArchiveUnusedPromptDraftParams{
		WorkspaceID: workspaceID, PromptID: pid, VersionID: vid,
	})
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrPromptVersionInUse
	}
	return tx.Commit(ctx)
}
