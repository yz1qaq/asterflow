package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

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

type sseEmitter struct {
	writer  http.ResponseWriter
	flusher http.Flusher
}

func newSSEEmitter(w http.ResponseWriter) (engine.Emitter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("response writer does not support flushing")
	}
	return &sseEmitter{writer: w, flusher: flusher}, nil
}

func (e *sseEmitter) Emit(_ context.Context, event engine.Event) error {
	encoded, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(e.writer, "data: %s\n\n", encoded); err != nil {
		return err
	}
	e.flusher.Flush()
	return nil
}

func (h *ConversationChatHandler) Chat(c *gin.Context) {
	emitter, err := newSSEEmitter(c.Writer)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming is unavailable"})
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

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
		writeConversationError(c, err)
		return
	}
	c.Status(http.StatusOK)
}
