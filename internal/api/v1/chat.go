package v1

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yz1qaq/asterflow/internal/api/middleware"
	"github.com/yz1qaq/asterflow/internal/platform/conversation"
	"github.com/yz1qaq/asterflow/internal/runtime/engine"
)

type ConversationChatHandler struct {
	service *conversation.Service
}

func NewConversationChatHandler(service *conversation.Service) *ConversationChatHandler {
	return &ConversationChatHandler{service: service}
}

const sseWriteTimeout = 10 * time.Second

type sseEmitter struct {
	writer     http.ResponseWriter
	controller *http.ResponseController
}

func newSSEEmitter(w http.ResponseWriter) (engine.Emitter, error) {
	if _, ok := w.(http.Flusher); !ok {
		return nil, fmt.Errorf("response writer does not support flushing")
	}
	// Write through Gin so its Written flag stays correct. Flush the underlying
	// writer directly so a socket error is visible to the run.
	raw := w
	if wrapper, ok := w.(interface{ Unwrap() http.ResponseWriter }); ok {
		raw = wrapper.Unwrap()
	}
	return &sseEmitter{writer: w, controller: http.NewResponseController(raw)}, nil
}

func (e *sseEmitter) Emit(ctx context.Context, event engine.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(sseWriteTimeout)
	if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
		deadline = limit
	}
	if err := e.controller.SetWriteDeadline(deadline); err == nil {
		defer func() { _ = e.controller.SetWriteDeadline(time.Time{}) }()
	} else if !errors.Is(err, http.ErrNotSupported) {
		return err
	}

	header := e.writer.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("X-Accel-Buffering", "no")
	if _, err := fmt.Fprintf(e.writer, "data: %s\n\n", encoded); err != nil {
		return err
	}
	return e.controller.Flush()
}

func (h *ConversationChatHandler) Chat(c *gin.Context) {
	emitter, err := newSSEEmitter(c.Writer)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming is unavailable"})
		return
	}
	var req conversation.ChatTurnRequest
	if err := decodeJSON(c.Writer, c.Request, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	ctx := c.Request.Context()
	req.WorkspaceID = middleware.GetWorkspaceIDFromContext(ctx)
	req.UserID = middleware.GetUserIDFromContext(ctx)
	req.ConversationID = c.Param("conversation_id")

	if _, err := h.service.RunTurn(ctx, req, emitter); err != nil {
		if !c.Writer.Written() && ctx.Err() == nil {
			writeConversationError(c, err)
		}
		return
	}
	c.Status(http.StatusOK)
}
