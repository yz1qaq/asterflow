package v1

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yz1qaq/asterflow/internal/api/middleware"
	"github.com/yz1qaq/asterflow/internal/platform/agent"
	"github.com/yz1qaq/asterflow/internal/platform/modelconfig"
	"github.com/yz1qaq/asterflow/internal/platform/prompt"
)

type AgentHandler struct {
	service *agent.Service
}

func NewAgentHandler(service *agent.Service) *AgentHandler {
	return &AgentHandler{service: service}
}

func writeAgentError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, agent.ErrInvalidArgument):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, prompt.ErrPromptVersionNotFound),
		errors.Is(err, modelconfig.ErrProfileVersionNotFound),
		errors.Is(err, agent.ErrAgentNotFound),
		errors.Is(err, agent.ErrAgentVersionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "agent or referenced version not found"})
	case errors.Is(err, agent.ErrAgentEnvNotBound):
		c.JSON(http.StatusNotFound, gin.H{"error": "agent environment is not bound"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "agent operation failed"})
	}
}

func (h *AgentHandler) ListAgents(c *gin.Context) {
	ctx := c.Request.Context()
	workspaceID := middleware.GetWorkspaceIDFromContext(ctx)
	agents, err := h.service.ListAgents(ctx, workspaceID)
	if err != nil {
		writeAgentError(c, err)
		return
	}
	c.JSON(http.StatusOK, agents)
}

func (h *AgentHandler) CreateAgent(c *gin.Context) {
	var req agent.CreateAgentRequest
	if err := decodeJSON(c.Writer, c.Request, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	ctx := c.Request.Context()
	req.WorkspaceID = middleware.GetWorkspaceIDFromContext(ctx)
	req.CreatedBy = middleware.GetUserIDFromContext(ctx)

	created, version, err := h.service.CreateAgent(ctx, req)
	if err != nil {
		writeAgentError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"agent": created, "version": version})
}

func (h *AgentHandler) GetAgent(c *gin.Context) {
	ctx := c.Request.Context()
	workspaceID := middleware.GetWorkspaceIDFromContext(ctx)
	found, err := h.service.GetAgent(ctx, workspaceID, c.Param("agent_id"))
	if err != nil {
		writeAgentError(c, err)
		return
	}
	c.JSON(http.StatusOK, found)
}

func (h *AgentHandler) ListVersions(c *gin.Context) {
	ctx := c.Request.Context()
	workspaceID := middleware.GetWorkspaceIDFromContext(ctx)
	versions, err := h.service.ListVersions(ctx, workspaceID, c.Param("agent_id"))
	if err != nil {
		writeAgentError(c, err)
		return
	}
	c.JSON(http.StatusOK, versions)
}

func (h *AgentHandler) CreateVersion(c *gin.Context) {
	var req agent.VersionInput
	if err := decodeJSON(c.Writer, c.Request, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	ctx := c.Request.Context()
	workspaceID := middleware.GetWorkspaceIDFromContext(ctx)
	version, err := h.service.AppendVersion(ctx, workspaceID, c.Param("agent_id"), req,
		middleware.GetUserIDFromContext(ctx))
	if err != nil {
		writeAgentError(c, err)
		return
	}
	c.JSON(http.StatusCreated, version)
}

func (h *AgentHandler) ListEnvs(c *gin.Context) {
	ctx := c.Request.Context()
	workspaceID := middleware.GetWorkspaceIDFromContext(ctx)
	bindings, err := h.service.ListEnvBindings(ctx, workspaceID, c.Param("agent_id"))
	if err != nil {
		writeAgentError(c, err)
		return
	}
	c.JSON(http.StatusOK, bindings)
}

func (h *AgentHandler) Promote(c *gin.Context) {
	var req struct {
		Env       string `json:"env"`
		VersionID string `json:"version_id"`
	}
	if err := decodeJSON(c.Writer, c.Request, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	ctx := c.Request.Context()
	workspaceID := middleware.GetWorkspaceIDFromContext(ctx)
	if err := h.service.Promote(ctx, workspaceID, c.Param("agent_id"), req.Env, req.VersionID); err != nil {
		writeAgentError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "promoted"})
}
