package iam

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"github.com/google/uuid"
	"github.com/yz1qaq/asterflow/internal/domain"
	"golang.org/x/crypto/bcrypt"
)

const (
	GlobalRoleAdmin  = "admin"
	GlobalRoleMember = "member"

	WorkspaceRoleOwner  = "owner"
	WorkspaceRoleAdmin  = "admin"
	WorkspaceRoleEditor = "editor"
	WorkspaceRoleMember = "member"
	WorkspaceRoleViewer = "viewer"
)

var (
	ErrUserNotFound = errors.New("user not found")
	ErrEmailExists  = errors.New("email already exists")

	ErrInvalidEmail = errors.New("invalid email")
	ErrWeakPassword = errors.New("password must be at least 8 bytes")
	ErrNameRequired = errors.New("name is required")

	ErrWorkspaceNotFound = errors.New("workspace not found")

	ErrWorkspaceMemberNotFound = errors.New("workspace member not found")
)

type Store interface {
	GetUserByEmail(ctx context.Context, email string) (*domain.User, error)
	GetUserByID(ctx context.Context, userID string) (*domain.User, error)
	CreateUser(ctx context.Context, user *domain.User) error
	ListUsers(ctx context.Context, limit, offset int32) ([]*domain.User, error)
	SetUserRole(ctx context.Context, userID, role string) error

	GetWorkspaceByID(
		ctx context.Context,
		workspaceID string,
	) (*domain.Workspace, error)

	ListWorkspaces(
		ctx context.Context,
		limit, offset int32,
	) ([]*domain.Workspace, error)

	ListUserWorkspaces(
		ctx context.Context,
		userID string,
		limit, offset int32,
	) ([]*domain.Workspace, error)

	CreateWorkspaceWithOwner(
		ctx context.Context,
		workspace *domain.Workspace,
		userID string,
	) error

	GetWorkspaceMember(
		ctx context.Context,
		workspaceID, userID string,
	) (*domain.WorkspaceMember, error)

	ListWorkspaceMembers(
		ctx context.Context,
		workspaceID string,
	) ([]*domain.WorkspaceMember, error)

	UpsertWorkspaceMemberGuarded(
		ctx context.Context,
		actorID string,
		member *domain.WorkspaceMember,
	) error

	DeleteWorkspaceMemberGuarded(
		ctx context.Context,
		actorID string,
		workspaceID, userID string,
	) (bool, error)
}

type Service struct {
	store  Store
	jwtKey []byte
}

func NewService(store Store, jwtKey []byte) *Service {
	return &Service{
		store:  store,
		jwtKey: jwtKey,
	}
}

func (s *Service) CreateUser(
	ctx context.Context,
	email, password, name string,
) (*domain.User, error) {
	email = normalizeEmail(email)
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email {
		return nil, ErrInvalidEmail
	}

	if len(password) < 8 {
		return nil, ErrWeakPassword
	}

	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrNameRequired
	}

	hash, err := bcrypt.GenerateFromPassword(
		[]byte(password),
		bcrypt.DefaultCost,
	)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	user := &domain.User{
		ID:           uuid.NewString(),
		Email:        email,
		Name:         name,
		Role:         GlobalRoleMember,
		PasswordHash: string(hash),
		Status:       "active",
	}

	if err := s.store.CreateUser(ctx, user); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return user, nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// 创建管理员,如果存在就直接提权
func (s *Service) EnsureSeedAdmin(
	ctx context.Context,
	email, password string,
) error {
	user, err := s.store.GetUserByEmail(ctx, normalizeEmail(email))
	switch {
	//如果不存在,就创建
	case errors.Is(err, ErrUserNotFound):
		user, err = s.CreateUser(ctx, email, password, "Admin")
		//这里防止并发问题,不存在的时候创建失败,可能是另外一个实例刚才创建了
		if errors.Is(err, ErrEmailExists) {
			user, err = s.store.GetUserByEmail(ctx, normalizeEmail(email))
		}
		if err != nil {
			return fmt.Errorf("create seed admin: %w", err)
		}

	case err != nil:
		return fmt.Errorf("get seed admin: %w", err)
	}

	if err := s.store.SetUserRole(ctx, user.ID, GlobalRoleAdmin); err != nil {
		return fmt.Errorf("grant seed admin role: %w", err)
	}
	return nil
}

func (s *Service) GetUser(
	ctx context.Context,
	userID string,
) (*domain.User, error) {
	return s.store.GetUserByID(ctx, userID)
}

func (s *Service) ListUsers(
	ctx context.Context,
	limit, offset int32,
) ([]*domain.User, error) {
	if limit <= 0 {
		limit = 50
	}
	return s.store.ListUsers(ctx, limit, offset)
}
