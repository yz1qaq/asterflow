package llm

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ResolvedDeployment is one callable provider deployment resolved by the control plane.
type ResolvedDeployment struct {
	ID                         string
	ProviderID                 string
	ProviderKind               string
	BaseURL                    string
	APIKey                     string
	Model                      string
	TimeoutMS                  int
	MaxRetries                 int
	InputPricePerMillion       float64
	OutputPricePerMillion      float64
	CachedInputPricePerMillion float64
}

type ResolvedModelProfile struct {
	VersionID         string
	ClassificationMax string               //允许的最高密级
	Deployments       []ResolvedDeployment //按优先级排好的部署列表
}

// ProfileResolver is implemented by the model-config control plane.
type ProfileResolver interface {
	ResolveProfile(ctx context.Context, workspaceID, versionID string) (*ResolvedModelProfile, error)
}

// DeploymentClient performs one provider call. Adapters (OpenAI-compatible,
// Ollama, test doubles) implement it; Gateway owns failover, not the adapter.
type DeploymentClient interface {
	Generate(ctx context.Context, dep ResolvedDeployment, req GenerateRequest) (*GenerateResponse, error)
	Stream(ctx context.Context, dep ResolvedDeployment, req GenerateRequest) (Stream, error)
}

// ProviderError lets adapters mark errors that are worth failing over.
type ProviderError struct {
	Err       error
	Retryable bool
}

type cancelStream struct {
	Stream
	cancel context.CancelFunc
}

func (s *cancelStream) Close() error {
	err := s.Stream.Close()
	s.cancel()
	return err
}

func (e *ProviderError) Error() string { return e.Err.Error() }
func (e *ProviderError) Unwrap() error { return e.Err }

func retryable(err error) bool {
	var providerErr *ProviderError
	if errors.As(err, &providerErr) {
		return providerErr.Retryable
	}
	return false
}

var classificationRank = map[string]int{
	"public": 0, "internal": 1, "confidential": 2, "secret": 3,
}

// FailoverGateway resolves a profile version and tries deployments in order.
type FailoverGateway struct {
	resolver ProfileResolver
	client   DeploymentClient
}

func NewFailoverGateway(resolver ProfileResolver, client DeploymentClient) *FailoverGateway {
	return &FailoverGateway{resolver: resolver, client: client}
}

func (g *FailoverGateway) profile(ctx context.Context, workspaceID, classification string, req GenerateRequest) (*ResolvedModelProfile, error) {
	if req.ModelProfileVersionID == "" {
		return nil, fmt.Errorf("model_profile_version_id is required")
	}
	if workspaceID == "" {
		return nil, fmt.Errorf("workspace_id is required")
	}
	profile, err := g.resolver.ResolveProfile(ctx, workspaceID, req.ModelProfileVersionID)
	if err != nil {
		return nil, err
	}
	if profile.ClassificationMax != "" {
		//得到这个模型方案这个版本的的最高密级
		allowed, ok := classificationRank[profile.ClassificationMax]
		if !ok {
			return nil, fmt.Errorf("unknown classification_max %q", profile.ClassificationMax)
		}
		//请求的密级
		want, exists := classificationRank[classification]
		if !exists {
			return nil, fmt.Errorf("unknown classification %q", classification)
		}
		//请求的密级不能超过模型方案profile版本允许的密级
		if want > allowed {
			return nil, ErrClassificationDenied
		}
	}
	//部署列表不能为空
	if len(profile.Deployments) == 0 {
		return nil, ErrModelUnavailable
	}
	return profile, nil
}

func (g *FailoverGateway) Generate(ctx context.Context, workspaceID, classification string, req GenerateRequest) (*GenerateResponse, error) {
	//得到方案版本
	profile, err := g.profile(ctx, workspaceID, classification, req)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, deployment := range profile.Deployments {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		callCtx := ctx
		cancel := func() {}
		if deployment.TimeoutMS > 0 {
			callCtx, cancel = context.WithTimeout(ctx, time.Duration(deployment.TimeoutMS)*time.Millisecond)
		}
		response, err := g.client.Generate(callCtx, deployment, req)
		cancel()
		if err == nil {
			return response, nil
		}
		if callCtx.Err() != nil && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !retryable(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("%w: %v", ErrModelUnavailable, lastErr)
}

func (g *FailoverGateway) Stream(ctx context.Context, workspaceID, classification string, req GenerateRequest) (Stream, error) {
	profile, err := g.profile(ctx, workspaceID, classification, req)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, deployment := range profile.Deployments {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		callCtx := ctx
		cancel := func() {}
		if deployment.TimeoutMS > 0 {
			callCtx, cancel = context.WithTimeout(ctx, time.Duration(deployment.TimeoutMS)*time.Millisecond)
		}
		stream, err := g.client.Stream(callCtx, deployment, req)
		if err == nil {
			return &cancelStream{Stream: stream, cancel: cancel}, nil
		}
		cancel()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		lastErr = err
		if !retryable(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("%w: %v", ErrModelUnavailable, lastErr)
}
