package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/yz1qaq/asterflow/internal/runtime/engine"
)

// Chat collects the same committed turn and ordered events used by SSE.
func (s *Service) Chat(ctx context.Context, req ChatTurnRequest) (string, error) {
	var collector chatCollector
	if _, err := s.RunTurn(ctx, req, &collector); err != nil {
		return "", err
	}
	if !collector.completed {
		return "", fmt.Errorf("conversation turn did not complete")
	}
	return collector.content.String(), nil
}

type chatCollector struct {
	content   strings.Builder
	completed bool
}

func (c *chatCollector) Emit(_ context.Context, event engine.Event) error {
	switch event.Type {
	case engine.EventModelDelta:
		var delta engine.ModelDelta
		if err := json.Unmarshal(event.Data, &delta); err != nil {
			return fmt.Errorf("decode model delta: %w", err)
		}
		c.content.WriteString(delta.Content)
	case engine.EventRunCompleted:
		c.completed = true
	}
	return nil
}

// turnEmitter adds a terminal event for service failures occurring after the SSE
// stream starts. An engine terminal event or a transport failure closes that path.
type turnEmitter struct {
	next     engine.Emitter
	lastSeq  int64
	terminal bool
}

func (e *turnEmitter) Emit(ctx context.Context, event engine.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch event.Type {
	case engine.EventRunCompleted, engine.EventRunFailed, engine.EventRunCancelled:
		e.terminal = true
	}
	if err := e.next.Emit(ctx, event); err != nil {
		e.terminal = true
		return err
	}
	e.lastSeq = event.Seq
	return nil
}

func (e *turnEmitter) fail(ctx context.Context, err error) {
	if e.lastSeq == 0 || e.terminal || ctx.Err() != nil {
		return
	}

	event := engine.Event{Seq: e.lastSeq + 1, Type: engine.EventRunFailed}
	switch {
	case errors.Is(err, ErrTurnLost):
		event.Data = engine.EncodeData(engine.RunFailed{Code: "turn_lease_lost", Message: "会话租约已失效，请重新发起"})
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		event.Type = engine.EventRunCancelled
		event.Data = engine.EncodeData(engine.RunCancelled{Reason: err.Error()})
	default:
		event.Data = engine.EncodeData(engine.RunFailed{Code: "turn_failed", Message: "本轮会话未能完成"})
	}
	_ = e.Emit(ctx, event)
}
