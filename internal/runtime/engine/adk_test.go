package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yz1qaq/asterflow/internal/runtime/llm"
)

type streamingGateway struct {
	llm.Gateway
	stream llm.Stream
}

func (g streamingGateway) Stream(context.Context, string, string, llm.GenerateRequest) (llm.Stream, error) {
	return g.stream, nil
}

type cancellableModelStream struct {
	started bool
	closed  chan struct{}
}

func (s *cancellableModelStream) Recv(ctx context.Context) (*llm.StreamChunk, error) {
	if !s.started {
		s.started = true
		return &llm.StreamChunk{Content: "partial"}, nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (s *cancellableModelStream) Close() error {
	close(s.closed)
	return nil
}

type captureEvent func(context.Context, Event) error

func (f captureEvent) Emit(ctx context.Context, ev Event) error {
	return f(ctx, ev)
}

func TestADKTurnCancellationClosesGatewayStream(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	stream := &cancellableModelStream{closed: make(chan struct{})}
	executor := NewExecutor(nil, streamingGateway{stream: stream})
	sawDelta := false
	_, err := executor.Run(ctx, RunInput{
		ConversationID: "conversation", AgentVersionID: "v1", TurnRevision: 4,
		RuntimeConfigJSON: `{}`, UserInput: "hello",
	}, captureEvent(func(_ context.Context, event Event) error {
		if event.Type == EventModelDelta {
			sawDelta = true
			cancel()
		}
		return nil
	}))
	if !sawDelta || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled run: saw delta = %t, err = %v", sawDelta, err)
	}
	select {
	case <-stream.closed:
	case <-time.After(time.Second):
		t.Fatal("ADK did not release the Gateway stream")
	}
}
