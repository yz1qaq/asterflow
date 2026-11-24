package modelconfig

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yz1qaq/asterflow/internal/runtime/llm"
)

var (
	ErrProviderAccountInUse = errors.New("provider account has model deployments")
	ErrProviderNotReady     = errors.New("provider account is not ready")
)

type ProviderTestResult struct {
	Model     string `json:"model"`
	LatencyMS int64  `json:"latency_ms"`
}

type ProviderTestError struct {
	Timeout        bool
	Network        bool
	UpstreamStatus int
}

func (e *ProviderTestError) Error() string { return "provider test request failed" }

func (s *Service) DeleteProviderAccount(ctx context.Context, workspaceID, accountID string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	id, err := uuid.Parse(strings.TrimSpace(accountID))
	if workspaceID == "" || err != nil {
		return fmt.Errorf("%w: workspace_id and a UUID account_id are required", ErrInvalidArgument)
	}
	return s.store.DeleteUnusedProviderAccount(ctx, workspaceID, id.String())
}

func (s *Service) TestProviderAccount(ctx context.Context, workspaceID, accountID, model string) (*ProviderTestResult, error) {
	workspaceID, model = strings.TrimSpace(workspaceID), strings.TrimSpace(model)
	id, err := uuid.Parse(strings.TrimSpace(accountID))
	if workspaceID == "" || err != nil || model == "" || len(model) > 200 {
		return nil, fmt.Errorf("%w: workspace_id, a UUID account_id and a model name of at most 200 bytes are required", ErrInvalidArgument)
	}
	account, err := s.store.GetProviderAccount(ctx, id.String())
	if err != nil {
		return nil, err
	}
	if account.WorkspaceID != workspaceID {
		return nil, ErrProviderAccountNotFound
	}
	if account.Status != "active" || len(account.APIKeyCiphertext) == 0 || s.cipher == nil {
		return nil, ErrProviderNotReady
	}
	key, err := s.cipher.Decrypt(account.APIKeyCiphertext)
	if err != nil {
		return nil, ErrProviderNotReady
	}

	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	start := time.Now()
	limit := 128
	reply, err := llm.NewOpenAIProbeClient().Generate(ctx, llm.ResolvedDeployment{
		BaseURL: account.BaseURL, APIKey: key, Model: model,
	}, llm.GenerateRequest{
		MaxOutputTokens: &limit,
		Messages:        []llm.Message{{Role: "user", Content: "Reply with OK only."}},
	})
	if err != nil {
		failure := &ProviderTestError{Timeout: errors.Is(err, context.DeadlineExceeded)}
		var transport *url.Error
		failure.Network = errors.As(err, &transport)
		var upstream *llm.ProviderError
		if errors.As(err, &upstream) {
			failure.UpstreamStatus = upstream.StatusCode
		}
		return nil, failure
	}
	if strings.TrimSpace(reply.Content) == "" {
		return nil, &ProviderTestError{}
	}
	return &ProviderTestResult{Model: model, LatencyMS: time.Since(start).Milliseconds()}, nil
}
