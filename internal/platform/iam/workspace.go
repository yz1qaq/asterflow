package iam

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/yz1qaq/asterflow/internal/domain"
	pgstore "github.com/yz1qaq/asterflow/internal/infrastructure/postgres/sqlc"
)

var (
	ErrForbidden            = errors.New("forbidden")
	ErrLastOwner            = errors.New("workspace must keep at least one owner")
	ErrInvalidWorkspaceRole = errors.New("invalid workspace role")
	ErrInvalidUserID        = errors.New("invalid user id")
)

// 检查user是否active
func (s *Service) activeUser(
	ctx context.Context,
	userID string,
) (*domain.User, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, ErrForbidden
	}

	user, err := s.store.GetUserByID(ctx, userID)
	if errors.Is(err, ErrUserNotFound) {
		return nil, ErrForbidden
	}
	if err != nil {
		return nil, fmt.Errorf("get active user: %w", err)
	}
	if user.Status != "active" {
		return nil, ErrForbidden
	}
	return user, nil
}

func (s *Service) CreateWorkspaceForUser(
	ctx context.Context,
	actorID, name, description string,
	parentID *string,
) (*domain.Workspace, error) {
	user, err := s.activeUser(ctx, actorID)
	if err != nil {
		return nil, err
	}

	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrNameRequired
	}

	workspace := &domain.Workspace{
		ID:          uuid.NewString(),
		Name:        name,
		Description: description,
		ParentID:    parentID,
	}
	if err := s.store.CreateWorkspaceWithOwner(ctx, workspace, user.ID); err != nil {
		return nil, fmt.Errorf("create workspace with owner: %w", err)
	}
	return workspace, nil
}

func (s *Service) ListAccessibleWorkspaces(
	ctx context.Context,
	userID string,
	limit, offset int32,
) ([]*domain.Workspace, error) {
	user, err := s.activeUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}

	if user.Role == GlobalRoleAdmin {
		return s.store.ListWorkspaces(ctx, limit, offset)
	}
	return s.store.ListUserWorkspaces(ctx, user.ID, limit, offset)
}

// 根据用户 ID 和空间 ID，确定这个用户在该空间的有效角色
func (s *Service) WorkspaceRole(
	ctx context.Context,
	userID, workspaceID string,
) (string, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return "", ErrForbidden
	}

	user, err := s.activeUser(ctx, userID)
	if err != nil {
		return "", err
	}

	//先确认空间存在
	if _, err = s.store.GetWorkspaceByID(ctx, workspaceID); err != nil {
		return "", fmt.Errorf("get workspace: %w", err)
	}

	//如果是全局管理员,就直接认作空间管理者
	if user.Role == GlobalRoleAdmin {
		return WorkspaceRoleAdmin, nil
	}

	member, err := s.store.GetWorkspaceMember(ctx, workspaceID, user.ID)
	if errors.Is(err, ErrWorkspaceMemberNotFound) {
		return "", ErrForbidden
	}
	if err != nil {
		return "", fmt.Errorf("get workspace role: %w", err)
	}
	return member.Role, nil
}

// 判断是否能进行owner的操作
func (s *Service) workspaceManagementAuthority(
	ctx context.Context,
	actorID, workspaceID string,
) (bool, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return false, ErrForbidden
	}
	user, err := s.activeUser(ctx, actorID)
	if err != nil {
		return false, err
	}
	if user.Role == GlobalRoleAdmin {
		return true, nil
	}

	//不是全局管理者的情况下
	member, err := s.store.GetWorkspaceMember(
		ctx,
		workspaceID,
		user.ID,
	)
	if errors.Is(err, ErrWorkspaceMemberNotFound) {
		return false, ErrForbidden
	}
	if err != nil {
		return false, fmt.Errorf("get management role: %w", err)
	}

	switch member.Role {
	case WorkspaceRoleOwner:
		return true, nil
	case WorkspaceRoleAdmin:
		return false, nil
	default:
		return false, ErrForbidden
	}
}

func validWorkspaceRole(role string) bool {
	switch role {
	case WorkspaceRoleOwner, WorkspaceRoleAdmin, WorkspaceRoleEditor,
		WorkspaceRoleMember, WorkspaceRoleViewer:
		return true
	default:
		return false
	}
}

func (s *Service) UpsertWorkspaceMember(
	ctx context.Context,
	actorID, workspaceID, userID, role string,
) (*domain.WorkspaceMember, error) {

	_, err := s.workspaceManagementAuthority(
		ctx,
		actorID,
		workspaceID,
	)
	if err != nil {
		return nil, err
	}
	if !validWorkspaceRole(role) {
		return nil, ErrInvalidWorkspaceRole
	}

	id, err := uuid.Parse(userID)
	if err != nil {
		return nil, ErrInvalidUserID
	}
	target, err := s.store.GetUserByID(ctx, id.String())
	if err != nil {
		return nil, fmt.Errorf("get member user: %w", err)
	}

	member := &domain.WorkspaceMember{
		WorkspaceID: workspaceID,
		UserID:      target.ID,
		Role:        role,
	}
	if err := s.store.UpsertWorkspaceMemberGuarded(
		ctx,
		actorID,
		member,
	); err != nil {
		return nil, err
	}
	return s.store.GetWorkspaceMember(ctx, workspaceID, target.ID)
}

func (s *Service) ListWorkspaceMembers(
	ctx context.Context,
	actorID, workspaceID string,
) ([]*domain.WorkspaceMember, error) {
	if _, err := s.workspaceManagementAuthority(
		ctx,
		actorID,
		workspaceID,
	); err != nil {
		return nil, err
	}
	return s.store.ListWorkspaceMembers(ctx, workspaceID)
}

func (s *Service) DeleteWorkspaceMember(
	ctx context.Context,
	actorID, workspaceID, userID string,
) error {
	_, err := s.workspaceManagementAuthority(
		ctx,
		actorID,
		workspaceID,
	)
	if err != nil {
		return err
	}

	id, err := uuid.Parse(userID)
	if err != nil {
		return ErrInvalidUserID
	}

	deleted, err := s.store.DeleteWorkspaceMemberGuarded(
		ctx,
		actorID,
		workspaceID,
		id.String(),
	)
	if err != nil {
		return err
	}
	if !deleted {
		return ErrWorkspaceMemberNotFound
	}
	return nil
}

func workspaceManagementAuthorityTx(
	ctx context.Context,
	q *pgstore.Queries,
	actorID, workspaceID string,
) (bool, error) {
	uid, err := uuid.Parse(actorID)
	if err != nil {
		return false, ErrForbidden
	}

	actor, err := q.GetUserByID(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrForbidden
	}
	if err != nil {
		return false, fmt.Errorf("get transaction actor: %w", err)
	}
	if actor.Status != "active" {
		return false, ErrForbidden
	}
	if actor.Role == GlobalRoleAdmin {
		return true, nil
	}

	member, err := q.GetWorkspaceMember(
		ctx,
		pgstore.GetWorkspaceMemberParams{
			WorkspaceID: workspaceID,
			UserID:      uid,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrForbidden
	}
	if err != nil {
		return false, fmt.Errorf("get transaction actor role: %w", err)
	}

	switch member.Role {
	case WorkspaceRoleOwner:
		return true, nil
	case WorkspaceRoleAdmin:
		return false, nil
	default:
		return false, ErrForbidden
	}
}
