package modelconfig

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	pgstore "github.com/yz1qaq/asterflow/internal/infrastructure/postgres/sqlc"
)

var (
	ErrProviderAccountNotFound   = errors.New("provider account not found")
	ErrProviderAccountNameExists = errors.New("provider account name already exists")
)

type PostgresStore struct {
	db *pgxpool.Pool
	q  *pgstore.Queries
}

func NewPostgresStore(db *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{
		db: db,
		q:  pgstore.New(db),
	}
}

func providerAccountFromRow(
	row pgstore.Provider,
) *providerAccountRecord {
	return &providerAccountRecord{
		ProviderAccount: ProviderAccount{
			ID:          row.ID.String(),
			WorkspaceID: row.WorkspaceID,
			Name:        row.Name,
			Kind:        row.Kind,
			BaseURL:     row.BaseUrl,
			Status:      row.Status,
			HasAPIKey:   len(row.ApiKeyCiphertext) > 0,
			CreatedBy:   row.CreatedBy,
			CreatedAt:   row.CreatedAt,
			UpdatedAt:   row.UpdatedAt,
		},
		APIKeyCiphertext: row.ApiKeyCiphertext,
	}
}

func (s *PostgresStore) CreateProviderAccount(
	ctx context.Context,
	account *providerAccountRecord,
) error {
	if account == nil {
		return fmt.Errorf("provider account is required")
	}
	id, err := uuid.Parse(account.ID)
	if err != nil {
		return fmt.Errorf("parse provider account id: %w", err)
	}
	row, err := s.q.CreateProviderAccount(
		ctx,
		pgstore.CreateProviderAccountParams{
			ID:               id,
			WorkspaceID:      account.WorkspaceID,
			Name:             account.Name,
			Kind:             account.Kind,
			BaseUrl:          account.BaseURL,
			ApiKeyCiphertext: account.APIKeyCiphertext,
			Status:           account.Status,
			CreatedBy:        account.CreatedBy,
		},
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) &&
			pgErr.Code == "23505" &&
			pgErr.ConstraintName == "providers_workspace_id_name_key" {
			return ErrProviderAccountNameExists
		}
		return fmt.Errorf("create provider account: %w", err)
	}
	*account = *providerAccountFromRow(row)
	return nil
}

func (s *PostgresStore) GetProviderAccount(
	ctx context.Context,
	accountID string,
) (*providerAccountRecord, error) {
	id, err := uuid.Parse(accountID)
	if err != nil {
		return nil, fmt.Errorf("parse provider account id: %w", err)
	}

	row, err := s.q.GetProviderAccount(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrProviderAccountNotFound
		}
		return nil, fmt.Errorf("get provider account: %w", err)
	}

	return providerAccountFromRow(row), nil
}

func (s *PostgresStore) ListProviderAccounts(
	ctx context.Context,
	workspaceID string,
) ([]*providerAccountRecord, error) {
	rows, err := s.q.ListProviderAccounts(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list provider accounts: %w", err)
	}

	accounts := make([]*providerAccountRecord, 0, len(rows))
	for _, row := range rows {
		accounts = append(accounts, providerAccountFromRow(row))
	}
	return accounts, nil
}

func (s *PostgresStore) UpdateProviderAPIKey(
	ctx context.Context,
	accountID string,
	ciphertext []byte,
) error {
	id, err := uuid.Parse(accountID)
	if err != nil {
		return fmt.Errorf("parse provider account id: %w", err)
	}

	affected, err := s.q.UpdateProviderAPIKey(
		ctx,
		pgstore.UpdateProviderAPIKeyParams{
			ID:               id,
			ApiKeyCiphertext: ciphertext,
		},
	)
	if err != nil {
		return fmt.Errorf("rotate provider api key: %w", err)
	}
	if affected != 1 {
		return ErrProviderAccountNotFound
	}
	return nil
}

func (s *PostgresStore) DeleteUnusedProviderAccount(ctx context.Context, workspaceID, accountID string) error {
	id, err := uuid.Parse(accountID)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin provider account deletion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	// The parent row lock conflicts with the key-share lock needed to insert a deployment.
	// The subsequent statement sees any deployment that committed before we acquired it.
	if _, err := q.LockProviderAccount(ctx, pgstore.LockProviderAccountParams{ID: id, WorkspaceID: workspaceID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrProviderAccountNotFound
		}
		return fmt.Errorf("lock provider account: %w", err)
	}
	affected, err := q.DeleteUnusedProviderAccount(ctx, pgstore.DeleteUnusedProviderAccountParams{ID: id, WorkspaceID: workspaceID})
	if err != nil {
		return fmt.Errorf("delete provider account: %w", err)
	}
	if affected == 0 {
		return ErrProviderAccountInUse
	}
	return tx.Commit(ctx)
}
