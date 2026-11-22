package llm

import (
	"context"
	"errors"
	"testing"
)

type fakeResolver struct {
	profile *ResolvedModelProfile
	err     error
}

func (f fakeResolver) ResolveProfile(context.Context, string, string) (*ResolvedModelProfile, error) {
	return f.profile, f.err
}

type fakeStream struct {
	closed bool
	chunks []StreamChunk
}

func (s *fakeStream) Recv(context.Context) (*StreamChunk, error) {
	if len(s.chunks) == 0 {
		return &StreamChunk{Done: true}, nil
	}
	chunk := s.chunks[0]
	s.chunks = s.chunks[1:]
	return &chunk, nil
}

func (s *fakeStream) Close() error { s.closed = true; return nil }

type fakeClient struct {
	calls       []string
	generateErr map[string]error
	streamErr   map[string]error
	stream      *fakeStream
}

func (f *fakeClient) Generate(_ context.Context, dep ResolvedDeployment, _ GenerateRequest) (*GenerateResponse, error) {
	f.calls = append(f.calls, dep.ID)
	if err := f.generateErr[dep.ID]; err != nil {
		return nil, err
	}
	return &GenerateResponse{Content: "from " + dep.ID, Model: dep.Model, DeploymentID: dep.ID}, nil
}

func (f *fakeClient) Stream(_ context.Context, dep ResolvedDeployment, _ GenerateRequest) (Stream, error) {
	f.calls = append(f.calls, dep.ID)
	if err := f.streamErr[dep.ID]; err != nil {
		return nil, err
	}
	return f.stream, nil
}

func profileWith(deployments ...ResolvedDeployment) *ResolvedModelProfile {
	return &ResolvedModelProfile{VersionID: "m1", ClassificationMax: "internal", Deployments: deployments}
}

func TestGatewayGenerateFailover(t *testing.T) {
	ctx := context.Background()
	deps := []ResolvedDeployment{{ID: "primary", Model: "a"}, {ID: "backup", Model: "b"}}
	client := &fakeClient{generateErr: map[string]error{"primary": &ProviderError{Err: errors.New("503"), Retryable: true}}}
	gateway := NewFailoverGateway(fakeResolver{profile: profileWith(deps...)}, client)

	response, err := gateway.Generate(ctx, "space-a", "internal", GenerateRequest{ModelProfileVersionID: "m1"})
	if err != nil || response.DeploymentID != "backup" || len(client.calls) != 2 {
		t.Fatalf("failover = %+v, %v, calls=%v", response, err, client.calls)
	}

	client = &fakeClient{generateErr: map[string]error{"primary": &ProviderError{Err: errors.New("401"), Retryable: false}}}
	gateway = NewFailoverGateway(fakeResolver{profile: profileWith(deps...)}, client)
	if _, err := gateway.Generate(ctx, "space-a", "internal", GenerateRequest{ModelProfileVersionID: "m1"}); err == nil || len(client.calls) != 1 {
		t.Fatalf("non-retryable must stop: calls=%v err=%v", client.calls, err)
	}

	client = &fakeClient{generateErr: map[string]error{
		"primary": &ProviderError{Err: errors.New("429"), Retryable: true},
		"backup":  &ProviderError{Err: errors.New("500"), Retryable: true},
	}}
	gateway = NewFailoverGateway(fakeResolver{profile: profileWith(deps...)}, client)
	if _, err := gateway.Generate(ctx, "space-a", "internal", GenerateRequest{ModelProfileVersionID: "m1"}); !errors.Is(err, ErrModelUnavailable) {
		t.Fatalf("all retryable = %v", err)
	}

	gateway = NewFailoverGateway(fakeResolver{profile: profileWith(deps...)}, &fakeClient{})
	if _, err := gateway.Generate(ctx, "space-a", "secret", GenerateRequest{ModelProfileVersionID: "m1"}); !errors.Is(err, ErrClassificationDenied) {
		t.Fatalf("classification = %v", err)
	}
	gateway = NewFailoverGateway(fakeResolver{profile: &ResolvedModelProfile{VersionID: "m1"}}, &fakeClient{})
	if _, err := gateway.Generate(ctx, "space-a", "internal", GenerateRequest{ModelProfileVersionID: "m1"}); !errors.Is(err, ErrModelUnavailable) {
		t.Fatalf("empty deployments = %v", err)
	}
	if _, err := gateway.Generate(ctx, "", "internal", GenerateRequest{ModelProfileVersionID: "m1"}); err == nil {
		t.Fatal("missing workspace accepted")
	}
	if _, err := gateway.Generate(ctx, "space-a", "internal", GenerateRequest{}); err == nil {
		t.Fatal("missing profile version accepted")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	gateway = NewFailoverGateway(fakeResolver{profile: profileWith(deps...)}, &fakeClient{})
	if _, err := gateway.Generate(canceled, "space-a", "internal", GenerateRequest{ModelProfileVersionID: "m1"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context = %v", err)
	}
}

func TestGatewayStreamFailoverBeforeStart(t *testing.T) {
	ctx := context.Background()
	stream := &fakeStream{chunks: []StreamChunk{{Content: "hi"}}}
	client := &fakeClient{
		streamErr: map[string]error{"primary": &ProviderError{Err: errors.New("timeout"), Retryable: true}},
		stream:    stream,
	}
	gateway := NewFailoverGateway(fakeResolver{profile: profileWith(
		ResolvedDeployment{ID: "primary"}, ResolvedDeployment{ID: "backup"},
	)}, client)

	got, err := gateway.Stream(ctx, "space-a", "internal", GenerateRequest{ModelProfileVersionID: "m1"})
	if err != nil || len(client.calls) != 2 {
		t.Fatalf("stream failover = %v, calls=%v", err, client.calls)
	}
	chunk, err := got.Recv(ctx)
	if err != nil || chunk.Content != "hi" {
		t.Fatalf("chunk = %+v, %v", chunk, err)
	}
	if err := got.Close(); err != nil || !stream.closed {
		t.Fatalf("close = %v, closed=%v", err, stream.closed)
	}

	client = &fakeClient{streamErr: map[string]error{"primary": &ProviderError{Err: errors.New("401"), Retryable: false}}, stream: &fakeStream{}}
	gateway = NewFailoverGateway(fakeResolver{profile: profileWith(
		ResolvedDeployment{ID: "primary"}, ResolvedDeployment{ID: "backup"},
	)}, client)
	if _, err := gateway.Stream(ctx, "space-a", "internal", GenerateRequest{ModelProfileVersionID: "m1"}); err == nil || len(client.calls) != 1 {
		t.Fatalf("non-retryable stream = %v, calls=%v", err, client.calls)
	}
}
