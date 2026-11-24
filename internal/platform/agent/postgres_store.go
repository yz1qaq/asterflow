package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yz1qaq/asterflow/internal/domain"
	pgstore "github.com/yz1qaq/asterflow/internal/infrastructure/postgres/sqlc"
	"github.com/yz1qaq/asterflow/internal/platform/prompt"
)

var (
	ErrAgentNotFound         = errors.New("agent not found")
	ErrAgentVersionNotFound  = errors.New("agent version not found")
	ErrAgentEnvNotBound      = errors.New("agent environment is not bound")
	ErrAgentConversationBusy = errors.New("agent has a running conversation")
)

type PostgresStore struct {
	db *pgxpool.Pool
	q  *pgstore.Queries
}

func NewPostgresStore(db *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{db: db, q: pgstore.New(db)}
}

func agentFromRow(row pgstore.Agent) *domain.Agent {
	return &domain.Agent{
		ID:          row.ID.String(),
		WorkspaceID: row.WorkspaceID,
		Name:        row.Name,
		Description: row.Description,
		Template:    row.Template,
		CreatedBy:   row.CreatedBy,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}

func agentVersionFromRow(row pgstore.AgentVersion) *domain.AgentVersion {
	return &domain.AgentVersion{
		ID:           row.ID.String(),
		AgentID:      row.AgentID.String(),
		Version:      int(row.Version),
		SnapshotJSON: row.SnapshotJson,
		CreatedBy:    row.CreatedBy,
		CreatedAt:    row.CreatedAt,
	}
}

// 验证输入是否符合json
func validateSnapshot(snapshot string) error {
	var object map[string]any
	if err := json.Unmarshal([]byte(snapshot), &object); err != nil {
		return fmt.Errorf("parse agent snapshot: %w", err)
	}
	if object == nil {
		return fmt.Errorf("agent snapshot must be a JSON object")
	}
	return nil
}

func lockSnapshotPromptVersions(ctx context.Context, q *pgstore.Queries, workspaceID, snapshotJSON string) error {
	var refs struct {
		System string `json:"system_prompt_version_id"`
		User   string `json:"user_prompt_version_id"`
	}
	if err := json.Unmarshal([]byte(snapshotJSON), &refs); err != nil {
		return fmt.Errorf("read snapshot prompt references: %w", err)
	}
	for _, ref := range []string{refs.System, refs.User} {
		id, err := uuid.Parse(strings.TrimSpace(ref))
		if err != nil {
			// Service-level validation handles identifiers. The store also reads legacy snapshots.
			continue
		}
		if err := prompt.LockVersionForReference(ctx, q, workspaceID, id); err != nil {
			return fmt.Errorf("lock referenced prompt version: %w", err)
		}
	}
	return nil
}

func orGeneral(template string) string {
	if strings.TrimSpace(template) == "" {
		return "general"
	}
	return strings.TrimSpace(template)
}

// CreateAgentWithInitialVersion writes the agent, immutable v1 and the dev pointer atomically.
func (s *PostgresStore) CreateAgentWithInitialVersion(
	ctx context.Context, a *domain.Agent, v *domain.AgentVersion,
) error {
	if a == nil || v == nil {
		return fmt.Errorf("agent and initial version are required")
	}
	workspaceID := strings.TrimSpace(a.WorkspaceID)
	name := strings.TrimSpace(a.Name)
	if workspaceID == "" || name == "" {
		return fmt.Errorf("workspace_id and name are required")
	}
	if err := validateSnapshot(v.SnapshotJSON); err != nil {
		return err
	}
	agentID, err := uuid.Parse(a.ID)
	if err != nil {
		return fmt.Errorf("parse agent id: %w", err)
	}
	versionID, err := uuid.Parse(v.ID)
	if err != nil {
		return fmt.Errorf("parse agent version id: %w", err)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin agent creation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if err := lockSnapshotPromptVersions(ctx, q, workspaceID, v.SnapshotJSON); err != nil {
		return err
	}

	agentRow, err := q.CreateAgent(ctx, pgstore.CreateAgentParams{
		ID:          agentID,
		WorkspaceID: workspaceID,
		Name:        name,
		Description: a.Description,
		Template:    orGeneral(a.Template),
		CreatedBy:   a.CreatedBy,
	})
	if err != nil {
		return fmt.Errorf("create agent: %w", err)
	}
	versionRow, err := q.CreateAgentVersion(ctx, pgstore.CreateAgentVersionParams{
		ID:           versionID,
		AgentID:      agentID,
		Version:      1,
		SnapshotJson: v.SnapshotJSON,
		CreatedBy:    v.CreatedBy,
	})
	if err != nil {
		return fmt.Errorf("create initial agent version: %w", err)
	}
	if _, err := q.UpsertAgentEnv(ctx, pgstore.UpsertAgentEnvParams{
		AgentID: agentID, Env: "dev", VersionID: versionID,
	}); err != nil {
		return fmt.Errorf("bind initial dev version: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit agent creation: %w", err)
	}

	*a = *agentFromRow(agentRow)
	*v = *agentVersionFromRow(versionRow)
	return nil
}

// AppendAgentVersion locks the parent agent and assigns the next version number.
func (s *PostgresStore) AppendAgentVersion(
	ctx context.Context, workspaceID string, v *domain.AgentVersion,
) error {
	if v == nil {
		return fmt.Errorf("agent version is required")
	}
	if err := validateSnapshot(v.SnapshotJSON); err != nil {
		return err
	}
	agentID, err := uuid.Parse(v.AgentID)
	if err != nil {
		return fmt.Errorf("parse agent id: %w", err)
	}
	versionID, err := uuid.Parse(v.ID)
	if err != nil {
		return fmt.Errorf("parse agent version id: %w", err)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin agent version append: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	if _, err := q.LockAgent(ctx, pgstore.LockAgentParams{
		ID: agentID, WorkspaceID: workspaceID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAgentNotFound
		}
		return fmt.Errorf("lock agent: %w", err)
	}
	if err := lockSnapshotPromptVersions(ctx, q, workspaceID, v.SnapshotJSON); err != nil {
		return err
	}
	next, err := q.NextAgentVersion(ctx, agentID)
	if err != nil {
		return fmt.Errorf("next agent version: %w", err)
	}
	row, err := q.CreateAgentVersion(ctx, pgstore.CreateAgentVersionParams{
		ID:           versionID,
		AgentID:      agentID,
		Version:      next,
		SnapshotJson: v.SnapshotJSON,
		CreatedBy:    v.CreatedBy,
	})
	if err != nil {
		return fmt.Errorf("create agent version: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit agent version: %w", err)
	}
	*v = *agentVersionFromRow(row)
	return nil
}

func (s *PostgresStore) DeleteAgent(ctx context.Context, workspaceID, agentID string) error {
	id, err := uuid.Parse(agentID)
	if err != nil {
		return fmt.Errorf("parse agent id: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin agent deletion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	// Lock the Agent first so a conversation insert cannot acquire the FK's key-share lock.
	if _, err := q.LockAgent(ctx, pgstore.LockAgentParams{ID: id, WorkspaceID: workspaceID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAgentNotFound
		}
		return fmt.Errorf("lock agent for deletion: %w", err)
	}
	// Hold every conversation lock through commit to exclude new turn claims.
	busy, err := q.LockAgentConversationLeases(ctx, pgstore.LockAgentConversationLeasesParams{
		AgentID: id, WorkspaceID: workspaceID,
	})
	if err != nil {
		return fmt.Errorf("lock agent conversations: %w", err)
	}
	if busy {
		return ErrAgentConversationBusy
	}
	if err := q.DeleteAgentConversations(ctx, pgstore.DeleteAgentConversationsParams{
		AgentID: id, WorkspaceID: workspaceID,
	}); err != nil {
		return fmt.Errorf("delete agent conversations: %w", err)
	}
	if err := q.DeleteAgentEnvs(ctx, id); err != nil {
		return fmt.Errorf("delete agent environments: %w", err)
	}
	if err := q.DeleteAgent(ctx, pgstore.DeleteAgentParams{ID: id, WorkspaceID: workspaceID}); err != nil {
		return fmt.Errorf("delete agent: %w", err)
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) GetAgent(
	ctx context.Context, workspaceID, agentID string,
) (*domain.Agent, error) {
	id, err := uuid.Parse(agentID)
	if err != nil {
		return nil, fmt.Errorf("parse agent id: %w", err)
	}
	row, err := s.q.GetAgent(ctx, pgstore.GetAgentParams{ID: id, WorkspaceID: workspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAgentNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get agent: %w", err)
	}
	return agentFromRow(row), nil
}

func (s *PostgresStore) ListAgents(
	ctx context.Context, workspaceID string,
) ([]*domain.Agent, error) {
	rows, err := s.q.ListAgents(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	agents := make([]*domain.Agent, 0, len(rows))
	for _, row := range rows {
		agents = append(agents, agentFromRow(row))
	}
	return agents, nil
}

func (s *PostgresStore) GetAgentVersion(
	ctx context.Context, workspaceID, versionID string,
) (*domain.AgentVersion, error) {
	id, err := uuid.Parse(versionID)
	if err != nil {
		return nil, fmt.Errorf("parse agent version id: %w", err)
	}
	row, err := s.q.GetAgentVersion(ctx, pgstore.GetAgentVersionParams{ID: id, WorkspaceID: workspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAgentVersionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get agent version: %w", err)
	}
	return agentVersionFromRow(row), nil
}

func (s *PostgresStore) ListAgentVersions(
	ctx context.Context, workspaceID, agentID string,
) ([]*domain.AgentVersion, error) {
	id, err := uuid.Parse(agentID)
	if err != nil {
		return nil, fmt.Errorf("parse agent id: %w", err)
	}
	rows, err := s.q.ListAgentVersions(ctx, pgstore.ListAgentVersionsParams{AgentID: id, WorkspaceID: workspaceID})
	if err != nil {
		return nil, fmt.Errorf("list agent versions: %w", err)
	}
	versions := make([]*domain.AgentVersion, 0, len(rows))
	for _, row := range rows {
		versions = append(versions, agentVersionFromRow(row))
	}
	return versions, nil
}

func agentEnvFromRow(row pgstore.AgentEnv) *EnvBinding {
	return &EnvBinding{
		AgentID:   row.AgentID.String(),
		Env:       row.Env,
		VersionID: row.VersionID.String(),
		UpdatedAt: row.UpdatedAt,
	}
}

func (s *PostgresStore) GetAgentEnv(
	ctx context.Context, workspaceID, agentID, env string,
) (*EnvBinding, error) {
	id, err := uuid.Parse(agentID)
	if err != nil {
		return nil, fmt.Errorf("parse agent id: %w", err)
	}
	env = strings.TrimSpace(env)
	if env == "" {
		return nil, fmt.Errorf("agent environment is required")
	}
	row, err := s.q.GetAgentEnv(ctx, pgstore.GetAgentEnvParams{
		AgentID: id, WorkspaceID: workspaceID, Env: env,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := s.GetAgent(ctx, workspaceID, agentID); err != nil {
			return nil, err
		}
		return nil, ErrAgentEnvNotBound
	}
	if err != nil {
		return nil, fmt.Errorf("get agent environment: %w", err)
	}
	return agentEnvFromRow(row), nil
}

// SetAgentEnv moves one environment pointer after verifying the target version belongs to the agent.
func (s *PostgresStore) SetAgentEnv(
	ctx context.Context, workspaceID, agentID, env, versionID string,
) error {
	aid, err := uuid.Parse(agentID)
	if err != nil {
		return fmt.Errorf("parse agent id: %w", err)
	}
	vid, err := uuid.Parse(versionID)
	if err != nil {
		return fmt.Errorf("parse agent version id: %w", err)
	}
	env = strings.TrimSpace(env)
	if env == "" {
		return fmt.Errorf("agent environment is required")
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin agent environment update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	if _, err := q.LockAgent(ctx, pgstore.LockAgentParams{
		ID: aid, WorkspaceID: workspaceID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAgentNotFound
		}
		return fmt.Errorf("lock agent: %w", err)
	}
	version, err := q.GetAgentVersion(ctx, pgstore.GetAgentVersionParams{
		ID: vid, WorkspaceID: workspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAgentVersionNotFound
	}
	if err != nil {
		return fmt.Errorf("get target agent version: %w", err)
	}
	if version.AgentID != aid {
		return ErrAgentVersionNotFound
	}
	if _, err := q.UpsertAgentEnv(ctx, pgstore.UpsertAgentEnvParams{
		AgentID: aid, Env: env, VersionID: vid,
	}); err != nil {
		return fmt.Errorf("set agent environment: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit agent environment update: %w", err)
	}
	return nil
}

func (s *PostgresStore) ListAgentEnvs(
	ctx context.Context, workspaceID, agentID string,
) ([]*EnvBinding, error) {
	id, err := uuid.Parse(agentID)
	if err != nil {
		return nil, fmt.Errorf("parse agent id: %w", err)
	}
	rows, err := s.q.ListAgentEnvs(ctx, pgstore.ListAgentEnvsParams{AgentID: id, WorkspaceID: workspaceID})
	if err != nil {
		return nil, fmt.Errorf("list agent environments: %w", err)
	}
	bindings := make([]*EnvBinding, 0, len(rows))
	for _, row := range rows {
		bindings = append(bindings, agentEnvFromRow(row))
	}
	return bindings, nil
}
