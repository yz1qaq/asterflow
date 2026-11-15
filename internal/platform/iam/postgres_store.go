package iam

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yz1qaq/asterflow/internal/domain"
	pgstore "github.com/yz1qaq/asterflow/internal/infrastructure/postgres/sqlc"
)

type PostgresStore struct {
	q *pgstore.Queries
}

func NewPostgresStore(db *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{
		q: pgstore.New(db),
	}
}

var _ Store = (*PostgresStore)(nil)

func (s *PostgresStore) GetUserByEmail(
	ctx context.Context,
	email string,
) (*domain.User, error) {
	row, err := s.q.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, userLookupError(err)
	}
	return userFromRow(row), nil
}

func (s *PostgresStore) GetUserByID(
	ctx context.Context,
	userID string,
) (*domain.User, error) {
	id, err := uuid.Parse(userID)
	if err != nil {
		return nil, fmt.Errorf("parse user id: %w", err)
	}

	row, err := s.q.GetUserByID(ctx, id)
	if err != nil {
		return nil, userLookupError(err)
	}
	return userFromRow(row), nil
}

func (s *PostgresStore) CreateUser(
	ctx context.Context,
	user *domain.User,
) error {
	if user == nil {
		return fmt.Errorf("user is required")
	}

	id, err := uuid.Parse(user.ID)
	if err != nil {
		return fmt.Errorf("parse user id: %w", err)
	}

	role := user.Role
	if role == "" {
		role = GlobalRoleMember
	}
	status := user.Status
	if status == "" {
		status = "active"
	}

	row, err := s.q.CreateUser(ctx, pgstore.CreateUserParams{
		ID:           id,
		Email:        user.Email,
		PasswordHash: user.PasswordHash,
		Name:         user.Name,
		Role:         role,
		Status:       status,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) &&
			pgErr.Code == "23505" &&
			pgErr.ConstraintName == "users_email_key" {
			return ErrEmailExists
		}
		return fmt.Errorf("create user: %w", err)
	}

	*user = *userFromRow(row)
	return nil
}

func (s *PostgresStore) ListUsers(
	ctx context.Context,
	limit, offset int32,
) ([]*domain.User, error) {
	rows, err := s.q.ListUsers(ctx, pgstore.ListUsersParams{
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}

	users := make([]*domain.User, 0, len(rows))
	for _, row := range rows {
		users = append(users, userFromRow(row))
	}
	return users, nil
}

func (s *PostgresStore) SetUserRole(
	ctx context.Context,
	userID, role string,
) error {
	id, err := uuid.Parse(userID)
	if err != nil {
		return fmt.Errorf("parse user id: %w", err)
	}

	if err := s.q.SetUserRole(ctx, pgstore.SetUserRoleParams{
		ID:   id,
		Role: role,
	}); err != nil {
		return fmt.Errorf("set user role: %w", err)
	}
	return nil
}

func userFromRow(row pgstore.User) *domain.User {
	return &domain.User{
		ID:           row.ID.String(),
		Email:        row.Email,
		Name:         row.Name,
		Role:         row.Role,
		PasswordHash: row.PasswordHash,
		Status:       row.Status,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
}

func userLookupError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUserNotFound
	}
	return fmt.Errorf("query user: %w", err)
}
