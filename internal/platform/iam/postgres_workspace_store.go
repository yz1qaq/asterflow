package iam

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/yz1qaq/asterflow/internal/domain"
	pgstore "github.com/yz1qaq/asterflow/internal/infrastructure/postgres/sqlc"
)

func (s *PostgresStore) GetWorkspaceByID(
	ctx context.Context,
	workspaceID string,
) (*domain.Workspace, error) {
	row, err := s.q.GetWorkspaceByID(ctx, workspaceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrWorkspaceNotFound
		}
		return nil, fmt.Errorf("get workspace: %w", err)
	}
	return workspaceFromRow(row), nil
}

func (s *PostgresStore) ListWorkspaces(
	ctx context.Context,
	limit, offset int32,
) ([]*domain.Workspace, error) {
	rows, err := s.q.ListWorkspaces(ctx, pgstore.ListWorkspacesParams{
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	return workspacesFromRows(rows), nil
}

func (s *PostgresStore) ListUserWorkspaces(
	ctx context.Context,
	userID string,
	limit, offset int32,
) ([]*domain.Workspace, error) {
	id, err := uuid.Parse(userID)
	if err != nil {
		return nil, fmt.Errorf("parse user id: %w", err)
	}

	rows, err := s.q.ListUserWorkspaces(ctx, pgstore.ListUserWorkspacesParams{
		UserID: id,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list user workspaces: %w", err)
	}
	return workspacesFromRows(rows), nil
}

func workspaceFromRow(row pgstore.Workspace) *domain.Workspace {
	workspace := &domain.Workspace{
		ID:          row.ID,
		Name:        row.Name,
		Description: row.Description,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
	if row.ParentID.Valid {
		parentID := row.ParentID.String
		workspace.ParentID = &parentID
	}
	return workspace
}

func workspacesFromRows(rows []pgstore.Workspace) []*domain.Workspace {
	workspaces := make([]*domain.Workspace, 0, len(rows))
	for _, row := range rows {
		workspaces = append(workspaces, workspaceFromRow(row))
	}
	return workspaces
}

func (s *PostgresStore) GetWorkspaceMember(
	ctx context.Context,
	workspaceID, userID string,
) (*domain.WorkspaceMember, error) {
	id, err := uuid.Parse(userID)
	if err != nil {
		return nil, fmt.Errorf("parse user id: %w", err)
	}

	row, err := s.q.GetWorkspaceMember(ctx, pgstore.GetWorkspaceMemberParams{
		WorkspaceID: workspaceID,
		UserID:      id,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrWorkspaceMemberNotFound
		}
		return nil, fmt.Errorf("get workspace member: %w", err)
	}

	return &domain.WorkspaceMember{
		WorkspaceID: row.WorkspaceID,
		UserID:      row.UserID.String(),
		Email:       row.Email,
		Name:        row.Name,
		Role:        row.Role,
		CreatedAt:   row.CreatedAt,
	}, nil
}

func (s *PostgresStore) ListWorkspaceMembers(
	ctx context.Context,
	workspaceID string,
) ([]*domain.WorkspaceMember, error) {
	rows, err := s.q.ListWorkspaceMembers(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list workspace members: %w", err)
	}

	members := make([]*domain.WorkspaceMember, 0, len(rows))
	for _, row := range rows {
		members = append(members, &domain.WorkspaceMember{
			WorkspaceID: row.WorkspaceID,
			UserID:      row.UserID.String(),
			Email:       row.Email,
			Name:        row.Name,
			Role:        row.Role,
			CreatedAt:   row.CreatedAt,
		})
	}
	return members, nil
}

// memer:写入的目标成员信息  allowOwnerManagement:操作者是否有操作管理员的权限,这里的memer信息,是修改后的信息
func (s *PostgresStore) UpsertWorkspaceMemberGuarded(
	ctx context.Context,
	actorID string,
	member *domain.WorkspaceMember,
) error {
	if member == nil {
		return fmt.Errorf("workspace member is required")
	}
	userID, err := uuid.Parse(member.UserID)
	if err != nil {
		return fmt.Errorf("parse user id: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.ReadCommitted,
	})
	if err != nil {
		return fmt.Errorf("begin membership transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.q.WithTx(tx)
	if _, err := q.LockWorkspace(ctx, member.WorkspaceID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrWorkspaceNotFound
		}
		return fmt.Errorf("lock workspace: %w", err)
	}

	allowOwnerManagement, err := workspaceManagementAuthorityTx(
		ctx,
		q,
		actorID,
		member.WorkspaceID,
	)
	if err != nil {
		return err
	}

	//查询要操作的成员是否已经存在,必须要存在才会插入
	existing, err := q.GetWorkspaceMember(
		ctx,
		pgstore.GetWorkspaceMemberParams{
			WorkspaceID: member.WorkspaceID,
			UserID:      userID,
		},
	)
	exists := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("get existing member: %w", err)
	}

	isOwner := exists && existing.Role == WorkspaceRoleOwner
	// 设置owner或修改现有owner，都要求操作者具有管理owner的权限。
	if (member.Role == WorkspaceRoleOwner || isOwner) &&
		!allowOwnerManagement {
		return ErrForbidden
	}

	//如果被操作的成员本身是owner,但是我们要改为不是owner,也就是把owner降级
	if isOwner && member.Role != WorkspaceRoleOwner {
		owners, err := q.CountWorkspaceOwners(ctx, member.WorkspaceID)
		if err != nil {
			return fmt.Errorf("count workspace owners: %w", err)
		}
		if owners <= 1 {
			return ErrLastOwner
		}
	}

	row, err := q.UpsertWorkspaceMember(
		ctx,
		pgstore.UpsertWorkspaceMemberParams{
			WorkspaceID: member.WorkspaceID,
			UserID:      userID,
			Role:        member.Role,
		},
	)
	if err != nil {
		return fmt.Errorf("upsert workspace member: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit membership transaction: %w", err)
	}

	member.UserID = row.UserID.String()
	member.CreatedAt = row.CreatedAt
	return nil
}

func (s *PostgresStore) DeleteWorkspaceMemberGuarded(
	ctx context.Context,
	actorID string,
	workspaceID, userID string,
) (bool, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return false, fmt.Errorf("parse user id: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.ReadCommitted,
	})
	if err != nil {
		return false, fmt.Errorf("begin membership transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.q.WithTx(tx)
	if _, err := q.LockWorkspace(ctx, workspaceID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrWorkspaceNotFound
		}
		return false, fmt.Errorf("lock workspace: %w", err)
	}

	allowOwnerManagement, err := workspaceManagementAuthorityTx(
		ctx,
		q,
		actorID,
		workspaceID,
	)
	if err != nil {
		return false, err
	}

	existing, err := q.GetWorkspaceMember(
		ctx,
		pgstore.GetWorkspaceMemberParams{
			WorkspaceID: workspaceID,
			UserID:      uid,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get existing member: %w", err)
	}

	//如果要删除的成员是owner,但是不允许操作owner
	if existing.Role == WorkspaceRoleOwner {
		//如果不允许操作owner,返回
		if !allowOwnerManagement {
			return false, ErrForbidden
		}

		//如果允许,需要判断是否是最后一个owner
		owners, err := q.CountWorkspaceOwners(ctx, workspaceID)
		if err != nil {
			return false, fmt.Errorf("count workspace owners: %w", err)
		}
		if owners <= 1 {
			return false, ErrLastOwner
		}
	}

	rows, err := q.DeleteWorkspaceMember(
		ctx,
		pgstore.DeleteWorkspaceMemberParams{
			WorkspaceID: workspaceID,
			UserID:      uid,
		},
	)
	if err != nil {
		return false, fmt.Errorf("delete workspace member: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit membership transaction: %w", err)
	}

	return rows > 0, nil
}
