package v1

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/yz1qaq/asterflow/internal/api/middleware"
	"github.com/yz1qaq/asterflow/internal/domain"
	"github.com/yz1qaq/asterflow/internal/platform/modelconfig"
	"github.com/yz1qaq/asterflow/internal/platform/prompt"
)

type PromptHandler struct {
	service *prompt.Service
}

func NewPromptHandler(service *prompt.Service) *PromptHandler {
	return &PromptHandler{service: service}
}

func writePromptError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, prompt.ErrInvalidArgument),
		errors.Is(err, modelconfig.ErrInvalidArgument):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, prompt.ErrPromptNotFound),
		errors.Is(err, prompt.ErrPromptVersionNotFound),
		errors.Is(err, modelconfig.ErrProfileVersionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "prompt or referenced version not found"})
	case errors.Is(err, prompt.ErrPromptEnvNotBound):
		c.JSON(http.StatusNotFound, gin.H{"error": "prompt environment is not bound"})
	case errors.Is(err, prompt.ErrActivePromptExperiment):
		c.JSON(http.StatusConflict, gin.H{"error": "another active experiment exists in this environment"})
	case errors.Is(err, prompt.ErrPromptExperimentNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "prompt experiment not found"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "prompt operation failed"})
	}
}

func (h *PromptHandler) ListPrompts(c *gin.Context) {
	ctx := c.Request.Context()
	workspaceID := middleware.GetWorkspaceIDFromContext(ctx)

	items, err := h.service.ListPrompts(ctx, workspaceID)
	if err != nil {
		writePromptError(c, err)
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *PromptHandler) CreatePrompt(c *gin.Context) {
	var req prompt.CreatePromptRequest
	if err := decodeJSON(c.Writer, c.Request, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	ctx := c.Request.Context()
	req.WorkspaceID = middleware.GetWorkspaceIDFromContext(ctx)
	req.CreatedBy = middleware.GetUserIDFromContext(ctx)

	p, version, err := h.service.CreatePrompt(ctx, req)
	if err != nil {
		writePromptError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"prompt": p, "version": version})
}

func (h *PromptHandler) CreateVersion(c *gin.Context) {
	var req prompt.CreateVersionRequest
	if err := decodeJSON(c.Writer, c.Request, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	ctx := c.Request.Context()
	req.WorkspaceID = middleware.GetWorkspaceIDFromContext(ctx)
	req.CreatedBy = middleware.GetUserIDFromContext(ctx)

	promptID := c.Param("prompt_id")
	version, err := h.service.CreateVersion(ctx, promptID, req)
	if err != nil {
		writePromptError(c, err)
		return
	}
	c.JSON(http.StatusCreated, version)
}

func (h *PromptHandler) ListVersions(c *gin.Context) {
	ctx := c.Request.Context()
	workspaceID := middleware.GetWorkspaceIDFromContext(ctx)
	promptID := c.Param("prompt_id")

	versions, err := h.service.ListVersions(ctx, workspaceID, promptID)
	if err != nil {
		writePromptError(c, err)
		return
	}
	c.JSON(http.StatusOK, versions)
}

func (h *PromptHandler) Render(c *gin.Context) {
	var req struct {
		VersionID string         `json:"version_id"`
		Env       string         `json:"env"`
		Vars      map[string]any `json:"vars"`
	}
	if err := decodeJSON(c.Writer, c.Request, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if req.VersionID != "" && req.Env != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "version_id and env are mutually exclusive"})
		return
	}

	ctx := c.Request.Context()
	workspaceID := middleware.GetWorkspaceIDFromContext(ctx)
	promptID := c.Param("prompt_id")

	var text string
	var err error
	if req.VersionID != "" {
		text, err = h.service.RenderByVersion(ctx, workspaceID, promptID, req.VersionID, req.Vars)
	} else {
		text, err = h.service.RenderByEnv(ctx, workspaceID, promptID, req.Env, middleware.GetUserIDFromContext(ctx), req.Vars)
	}
	if err != nil {
		writePromptError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"rendered": text})
}

func (h *PromptHandler) Promote(c *gin.Context) {
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
	if err := h.service.Promote(ctx, workspaceID, c.Param("prompt_id"), req.Env, req.VersionID,
		middleware.GetUserIDFromContext(ctx)); err != nil {
		writePromptError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "promoted"})
}

func (h *PromptHandler) Rollback(c *gin.Context) {
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
	if err := h.service.Rollback(ctx, workspaceID, c.Param("prompt_id"), req.Env, req.VersionID,
		middleware.GetUserIDFromContext(ctx)); err != nil {
		writePromptError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "rolled_back"})
}

func (h *PromptHandler) ListEnvs(c *gin.Context) {
	ctx := c.Request.Context()
	workspaceID := middleware.GetWorkspaceIDFromContext(ctx)

	bindings, err := h.service.ListEnvBindings(ctx, workspaceID, c.Param("prompt_id"))
	if err != nil {
		writePromptError(c, err)
		return
	}
	c.JSON(http.StatusOK, bindings)
}

func (h *PromptHandler) Diff(c *gin.Context) {
	from, err := strconv.Atoi(c.Query("from"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "from must be a positive integer"})
		return
	}
	to, err := strconv.Atoi(c.Query("to"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "to must be a positive integer"})
		return
	}

	ctx := c.Request.Context()
	workspaceID := middleware.GetWorkspaceIDFromContext(ctx)
	diff, err := h.service.Diff(ctx, workspaceID, c.Param("prompt_id"), from, to)
	if err != nil {
		writePromptError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"diff": diff})
}

func (h *PromptHandler) StartExperiment(c *gin.Context) {
	var req struct {
		Env      string                     `json:"env"`
		Variants []domain.ExperimentVariant `json:"variants"`
	}
	if err := decodeJSON(c.Writer, c.Request, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	ctx := c.Request.Context()
	workspaceID := middleware.GetWorkspaceIDFromContext(ctx)
	exp, err := h.service.StartExperiment(ctx, workspaceID, c.Param("prompt_id"), req.Env, req.Variants,
		middleware.GetUserIDFromContext(ctx))
	if err != nil {
		writePromptError(c, err)
		return
	}
	c.JSON(http.StatusCreated, exp)
}

func (h *PromptHandler) StartRollout(c *gin.Context) {
	var req struct {
		Env                string `json:"env"`
		CandidateVersionID string `json:"candidate_version_id"`
		Traffic            int    `json:"traffic"`
	}
	if err := decodeJSON(c.Writer, c.Request, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	ctx := c.Request.Context()
	workspaceID := middleware.GetWorkspaceIDFromContext(ctx)
	exp, err := h.service.StartRollout(ctx, workspaceID, c.Param("prompt_id"), req.Env,
		req.CandidateVersionID, middleware.GetUserIDFromContext(ctx), req.Traffic)
	if err != nil {
		writePromptError(c, err)
		return
	}
	c.JSON(http.StatusCreated, exp)
}

func (h *PromptHandler) UpdateRolloutTraffic(c *gin.Context) {
	var req struct {
		Env     string `json:"env"`
		Traffic int    `json:"traffic"`
	}
	if err := decodeJSON(c.Writer, c.Request, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	ctx := c.Request.Context()
	workspaceID := middleware.GetWorkspaceIDFromContext(ctx)
	exp, err := h.service.UpdateRolloutTraffic(ctx, workspaceID, c.Param("prompt_id"), req.Env, req.Traffic,
		middleware.GetUserIDFromContext(ctx))
	if err != nil {
		writePromptError(c, err)
		return
	}
	c.JSON(http.StatusOK, exp)
}

func (h *PromptHandler) CompleteRollout(c *gin.Context) {
	var req struct {
		Env string `json:"env"`
	}
	if err := decodeJSON(c.Writer, c.Request, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	ctx := c.Request.Context()
	workspaceID := middleware.GetWorkspaceIDFromContext(ctx)
	if err := h.service.CompleteRollout(ctx, workspaceID, c.Param("prompt_id"), req.Env,
		middleware.GetUserIDFromContext(ctx)); err != nil {
		writePromptError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "completed"})
}

func (h *PromptHandler) RollbackRollout(c *gin.Context) {
	var req struct {
		Env string `json:"env"`
	}
	if err := decodeJSON(c.Writer, c.Request, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	ctx := c.Request.Context()
	workspaceID := middleware.GetWorkspaceIDFromContext(ctx)
	if err := h.service.RollbackRollout(ctx, workspaceID, c.Param("prompt_id"), req.Env,
		middleware.GetUserIDFromContext(ctx)); err != nil {
		writePromptError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "rolled_back"})
}

func (h *PromptHandler) GetActiveExperiment(c *gin.Context) {
	ctx := c.Request.Context()
	workspaceID := middleware.GetWorkspaceIDFromContext(ctx)

	exp, err := h.service.GetActiveExperiment(ctx, workspaceID, c.Param("prompt_id"), c.Query("env"))
	if err != nil {
		writePromptError(c, err)
		return
	}
	c.JSON(http.StatusOK, exp)
}

func (h *PromptHandler) ListRolloutEvents(c *gin.Context) {
	ctx := c.Request.Context()
	workspaceID := middleware.GetWorkspaceIDFromContext(ctx)

	events, err := h.service.ListRolloutEvents(ctx, workspaceID, c.Param("experiment_id"))
	if err != nil {
		writePromptError(c, err)
		return
	}
	c.JSON(http.StatusOK, events)
}

func (h *PromptHandler) ListEnvEvents(c *gin.Context) {
	ctx := c.Request.Context()
	workspaceID := middleware.GetWorkspaceIDFromContext(ctx)

	events, err := h.service.ListEnvEvents(ctx, workspaceID, c.Param("prompt_id"), c.Query("env"))
	if err != nil {
		writePromptError(c, err)
		return
	}
	c.JSON(http.StatusOK, events)
}
