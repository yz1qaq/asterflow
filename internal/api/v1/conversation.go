package v1

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/yz1qaq/asterflow/internal/api/middleware"
	"github.com/yz1qaq/asterflow/internal/platform/agent"
	"github.com/yz1qaq/asterflow/internal/platform/conversation"
	"github.com/yz1qaq/asterflow/internal/platform/prompt"
	"github.com/yz1qaq/asterflow/internal/runtime/engine"
)

type ConversationHandler struct {
	service *conversation.Service
}

func NewConversationHandler(service *conversation.Service) *ConversationHandler {
	return &ConversationHandler{service: service}
}

func writeConversationError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, conversation.ErrInvalidArgument),
		errors.Is(err, engine.ErrInvalidUserPrompt),
		errors.Is(err, prompt.ErrInvalidArgument):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})

	case errors.Is(err, conversation.ErrTurnBusy), errors.Is(err, conversation.ErrTurnLost):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})

	case errors.Is(err, conversation.ErrConversationNotFound),
		errors.Is(err, agent.ErrAgentNotFound),
		errors.Is(err, agent.ErrAgentVersionNotFound),
		errors.Is(err, prompt.ErrPromptVersionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "conversation or agent version not found"})

	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "conversation operation failed"})
	}
}

func (h *ConversationHandler) Create(c *gin.Context) {
	var req conversation.CreateConversationRequest
	if err := decodeJSON(c.Writer, c.Request, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	ctx := c.Request.Context()
	req.WorkspaceID = middleware.GetWorkspaceIDFromContext(ctx)
	req.UserID = middleware.GetUserIDFromContext(ctx)

	created, err := h.service.Create(ctx, req)
	if err != nil {
		writeConversationError(c, err)
		return
	}
	c.JSON(http.StatusCreated, created)
}

func (h *ConversationHandler) List(c *gin.Context) {
	ctx := c.Request.Context()
	workspaceID := middleware.GetWorkspaceIDFromContext(ctx)
	allowAll := isWorkspaceAdmin(middleware.GetWorkspaceRoleFromContext(ctx))
	limit, _ := strconv.ParseInt(c.DefaultQuery("limit", "20"), 10, 32)
	offset, _ := strconv.ParseInt(c.DefaultQuery("offset", "0"), 10, 32)

	items, err := h.service.List(ctx, workspaceID, middleware.GetUserIDFromContext(ctx), allowAll, int32(limit), int32(offset))
	if err != nil {
		writeConversationError(c, err)
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *ConversationHandler) Get(c *gin.Context) {
	ctx := c.Request.Context()
	workspaceID := middleware.GetWorkspaceIDFromContext(ctx)
	userID := middleware.GetUserIDFromContext(ctx)
	allowAll := isWorkspaceAdmin(middleware.GetWorkspaceRoleFromContext(ctx))
	conversationID := c.Param("conversation_id")

	found, err := h.service.Get(ctx, workspaceID, conversationID, userID, allowAll)
	if err != nil {
		writeConversationError(c, err)
		return
	}
	messages, err := h.service.Messages(ctx, workspaceID, conversationID, userID, allowAll)
	if err != nil {
		writeConversationError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"conversation": found, "messages": messages})
}

func (h *ConversationHandler) Delete(c *gin.Context) {
	ctx := c.Request.Context()
	err := h.service.Delete(ctx, middleware.GetWorkspaceIDFromContext(ctx),
		c.Param("conversation_id"), middleware.GetUserIDFromContext(ctx))
	if err != nil {
		writeConversationError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func isWorkspaceAdmin(role string) bool {
	return role == "owner" || role == "admin"
}
