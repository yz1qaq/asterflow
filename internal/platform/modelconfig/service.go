package modelconfig

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
)

var ErrInvalidArgument = errors.New("invalid model configuration argument")

type Store interface {
	CreateProviderAccount(context.Context, *providerAccountRecord) error
	GetProviderAccount(context.Context, string) (*providerAccountRecord, error)
	ListProviderAccounts(context.Context, string) ([]*providerAccountRecord, error)
	UpdateProviderAPIKey(context.Context, string, []byte) error
	DeleteUnusedProviderAccount(ctx context.Context, workspaceID, accountID string) error

	CreateDeployment(context.Context, *ModelDeployment) error
	GetDeployment(context.Context, string) (*ModelDeployment, error)
	ListDeployments(context.Context, string) ([]*ModelDeployment, error)

	CreateProfileWithVersion(context.Context, *ModelProfile, *ModelProfileVersion) error
	GetProfile(context.Context, string) (*ModelProfile, error)
	ListProfiles(context.Context, string) ([]*ModelProfile, error)
	CreateProfileVersion(context.Context, *ModelProfileVersion) error
	ListProfileVersions(context.Context, string) ([]*ModelProfileVersion, error)

	GetProfileVersion(context.Context, string) (*ModelProfileVersion, error)
	ListWorkspaceProfileVersions(context.Context, string) ([]*ModelProfileVersion, error)
	GetProjectBinding(context.Context, string, string) (*ProjectBinding, error)
	WithWorkspaceModelWrite(context.Context, string, func(WorkspaceModelWriter) error) error
}

type Service struct {
	store  Store
	cipher *Cipher
}

func NewService(store Store, cipher *Cipher) *Service {
	return &Service{
		store:  store,
		cipher: cipher,
	}
}

type CreateProviderAccountRequest struct {
	WorkspaceID string `json:"-"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	BaseURL     string `json:"base_url"`
	APIKey      string `json:"api_key"`
	CreatedBy   string `json:"-"`
}

func (s *Service) CreateProviderAccount(
	ctx context.Context,
	req CreateProviderAccountRequest,
) (*ProviderAccount, error) {
	req.WorkspaceID = strings.TrimSpace(req.WorkspaceID)
	req.Name = strings.TrimSpace(req.Name)
	req.Kind = strings.TrimSpace(req.Kind)
	req.BaseURL = strings.TrimSpace(req.BaseURL)

	//参数校验
	if req.WorkspaceID == "" ||
		req.Name == "" ||
		req.Kind == "" ||
		req.BaseURL == "" ||
		strings.TrimSpace(req.APIKey) == "" {
		return nil, fmt.Errorf(
			"%w: workspace_id, name, kind, base_url and api_key are required",
			ErrInvalidArgument,
		)
	}

	endpoint, err := url.Parse(req.BaseURL)
	if err != nil ||
		(endpoint.Scheme != "http" && endpoint.Scheme != "https") ||
		endpoint.Hostname() == "" ||
		endpoint.User != nil ||
		endpoint.RawQuery != "" ||
		endpoint.ForceQuery ||
		endpoint.Fragment != "" {
		return nil, fmt.Errorf(
			"%w: base_url must be an absolute HTTP(S) URL without user info, query, or fragment",
			ErrInvalidArgument,
		)
	}

	//对APIkey加密
	encrypted, err := s.cipher.Encrypt(req.APIKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt provider api key: %w", err)
	}

	record := &providerAccountRecord{
		ProviderAccount: ProviderAccount{
			ID:          uuid.NewString(),
			WorkspaceID: req.WorkspaceID,
			Name:        req.Name,
			Kind:        req.Kind,
			BaseURL:     req.BaseURL,
			Status:      "active",
			CreatedBy:   req.CreatedBy,
		},
		APIKeyCiphertext: encrypted,
	}

	if err := s.store.CreateProviderAccount(ctx, record); err != nil {
		return nil, fmt.Errorf("create provider account: %w", err)
	}

	out := record.ProviderAccount
	return &out, nil
}

func (s *Service) ListProviderAccounts(
	ctx context.Context,
	workspaceID string,
) ([]*ProviderAccount, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf(
			"%w: workspace_id is required",
			ErrInvalidArgument,
		)
	}

	rows, err := s.store.ListProviderAccounts(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list provider accounts: %w", err)
	}

	accounts := make([]*ProviderAccount, 0, len(rows))
	for _, row := range rows {
		account := row.ProviderAccount
		accounts = append(accounts, &account)
	}

	return accounts, nil
}

// 轮换API
func (s *Service) RotateProviderAPIKey(
	ctx context.Context,
	workspaceID, accountID, apiKey string,
) error {
	workspaceID = strings.TrimSpace(workspaceID)
	accountID = strings.TrimSpace(accountID)

	if workspaceID == "" || strings.TrimSpace(apiKey) == "" {
		return fmt.Errorf(
			"%w: workspace_id and api_key are required",
			ErrInvalidArgument,
		)
	}

	if _, err := uuid.Parse(accountID); err != nil {
		return fmt.Errorf(
			"%w: account_id must be a UUID",
			ErrInvalidArgument,
		)
	}

	account, err := s.store.GetProviderAccount(ctx, accountID)
	if err != nil {
		return fmt.Errorf("get provider account: %w", err)
	}
	if account.WorkspaceID != workspaceID {
		return ErrProviderAccountNotFound
	}

	encrypted, err := s.cipher.Encrypt(apiKey)
	if err != nil {
		return fmt.Errorf("encrypt provider api key: %w", err)
	}

	if err := s.store.UpdateProviderAPIKey(
		ctx,
		account.ID,
		encrypted,
	); err != nil {
		return fmt.Errorf("rotate provider api key: %w", err)
	}

	return nil
}
