package v1

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yz1qaq/asterflow/internal/runtime/engine"
)

type sseRecorder struct {
	*httptest.ResponseRecorder
	deadlines []time.Time
	flushErr  error
}

func (w *sseRecorder) SetWriteDeadline(deadline time.Time) error {
	w.deadlines = append(w.deadlines, deadline)
	return nil
}

func (w *sseRecorder) FlushError() error { return w.flushErr }

func TestSSEEmitterBoundsWriteAndReturnsFlushError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	recorder := &sseRecorder{ResponseRecorder: httptest.NewRecorder(), flushErr: context.DeadlineExceeded}
	c, _ := gin.CreateTestContext(recorder)
	emitter, err := newSSEEmitter(c.Writer)
	if err != nil {
		t.Fatal(err)
	}
	if err := emitter.Emit(ctx, engine.Event{Seq: 1, Type: engine.EventRunStarted}); !errors.Is(err, recorder.flushErr) {
		t.Fatalf("flush error = %v", err)
	}
	deadline, _ := ctx.Deadline()
	if len(recorder.deadlines) != 2 || !recorder.deadlines[0].Equal(deadline) || !recorder.deadlines[1].IsZero() {
		t.Fatalf("write deadlines = %v", recorder.deadlines)
	}
	if !c.Writer.Written() || recorder.Header().Get("Content-Type") != "text/event-stream" || recorder.Code != http.StatusOK {
		t.Fatalf("SSE response was not written through Gin: %+v", recorder.Result())
	}
}
