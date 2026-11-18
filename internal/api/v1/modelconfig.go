package v1

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yz1qaq/asterflow/internal/api/middleware"
	"github.com/yz1qaq/asterflow/internal/platform/modelconfig"
)

type ModelConfigHandler struct {
	service *modelconfig.Service
}

func NewModelConfigHandler(
	service *modelconfig.Service,
) *ModelConfigHandler {
	return &ModelConfigHandler{service: service}
}

func writeModelConfigError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, modelconfig.ErrInvalidArgument):
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})

	case errors.Is(err, modelconfig.ErrProjectBindingNotFound):
		c.JSON(http.StatusNotFound, gin.H{
			"error": "project model binding not found",
			"code":  "model_binding_not_found",
		})

	case errors.Is(err, modelconfig.ErrProviderAccountNotFound),
		errors.Is(err, modelconfig.ErrDeploymentNotFound),
		errors.Is(err, modelconfig.ErrProfileNotFound),
		errors.Is(err, modelconfig.ErrProfileVersionNotFound):
		c.JSON(http.StatusNotFound, gin.H{
			"error": "model configuration not found",
		})
	case errors.Is(err, modelconfig.ErrDeploymentPriceRequired):
		c.JSON(http.StatusConflict, gin.H{
			"error": "deployment pricing is required by a positive-budget binding",
			"code":  "deployment_price_required",
		})
	case errors.Is(err, modelconfig.ErrProviderAccountNameExists),
		errors.Is(err, modelconfig.ErrDeploymentNameExists),
		errors.Is(err, modelconfig.ErrProfileNameExists):
		c.JSON(http.StatusConflict, gin.H{
			"error": "model configuration name already exists",
		})

	default:
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "model configuration operation failed",
		})
	}
}

func (h *ModelConfigHandler) CreateProviderAccount(c *gin.Context) {
	var req modelconfig.CreateProviderAccountRequest
	if err := decodeJSON(c.Writer, c.Request, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request body",
		})
		return
	}

	ctx := c.Request.Context()
	req.WorkspaceID = middleware.GetWorkspaceIDFromContext(ctx)
	req.CreatedBy = middleware.GetUserIDFromContext(ctx)

	account, err := h.service.CreateProviderAccount(ctx, req)
	if err != nil {
		writeModelConfigError(c, err)
		return
	}

	c.JSON(http.StatusCreated, account)
}

func (h *ModelConfigHandler) ListProviderAccounts(c *gin.Context) {
	ctx := c.Request.Context()
	accounts, err := h.service.ListProviderAccounts(
		ctx,
		middleware.GetWorkspaceIDFromContext(ctx),
	)
	if err != nil {
		writeModelConfigError(c, err)
		return
	}

	c.JSON(http.StatusOK, accounts)
}

func (h *ModelConfigHandler) RotateProviderAPIKey(c *gin.Context) {
	var req struct {
		APIKey string `json:"api_key"`
	}
	if err := decodeJSON(c.Writer, c.Request, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request body",
		})
		return
	}

	ctx := c.Request.Context()
	if err := h.service.RotateProviderAPIKey(
		ctx,
		middleware.GetWorkspaceIDFromContext(ctx),
		c.Param("account_id"),
		req.APIKey,
	); err != nil {
		writeModelConfigError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *ModelConfigHandler) CreateDeployment(c *gin.Context) {
	var req modelconfig.CreateDeploymentRequest
	if err := decodeJSON(c.Writer, c.Request, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request body",
		})
		return
	}
	ctx := c.Request.Context()
	req.WorkspaceID = middleware.GetWorkspaceIDFromContext(ctx)
	req.CreatedBy = middleware.GetUserIDFromContext(ctx)

	deployment, err := h.service.CreateDeployment(ctx, req)
	if err != nil {
		writeModelConfigError(c, err)
		return
	}

	c.JSON(http.StatusCreated, deployment)
}

func (h *ModelConfigHandler) ListDeployments(c *gin.Context) {
	ctx := c.Request.Context()
	deployments, err := h.service.ListDeployments(
		ctx,
		middleware.GetWorkspaceIDFromContext(ctx),
	)
	if err != nil {
		writeModelConfigError(c, err)
		return
	}

	c.JSON(http.StatusOK, deployments)
}

func (h *ModelConfigHandler) UpdateDeploymentPricing(c *gin.Context) {
	var body struct {
		InputPricePerMillion       *float64 `json:"input_price_per_million"`
		OutputPricePerMillion      *float64 `json:"output_price_per_million"`
		CachedInputPricePerMillion *float64 `json:"cached_input_price_per_million"`
	}
	if err := decodeJSON(c.Writer, c.Request, &body); err != nil ||
		body.InputPricePerMillion == nil ||
		body.OutputPricePerMillion == nil ||
		body.CachedInputPricePerMillion == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "all three prices are required"})
		return
	}

	ctx := c.Request.Context()
	req := modelconfig.UpdateDeploymentPricingRequest{
		WorkspaceID:                middleware.GetWorkspaceIDFromContext(ctx),
		InputPricePerMillion:       *body.InputPricePerMillion,
		OutputPricePerMillion:      *body.OutputPricePerMillion,
		CachedInputPricePerMillion: *body.CachedInputPricePerMillion,
	}

	deployment, err := h.service.UpdateDeploymentPricing(
		ctx,
		c.Param("deployment_id"),
		req,
	)
	if err != nil {
		writeModelConfigError(c, err)
		return
	}
	c.JSON(http.StatusOK, deployment)
}

func (h *ModelConfigHandler) CreateProfile(c *gin.Context) {
	var req modelconfig.CreateProfileRequest
	if err := decodeJSON(c.Writer, c.Request, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request body",
		})
		return
	}

	ctx := c.Request.Context()
	req.WorkspaceID = middleware.GetWorkspaceIDFromContext(ctx)
	req.CreatedBy = middleware.GetUserIDFromContext(ctx)

	profile, version, err := h.service.CreateProfile(ctx, req)
	if err != nil {
		writeModelConfigError(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"profile": profile,
		"version": version,
	})
}

func (h *ModelConfigHandler) ListProfiles(c *gin.Context) {
	ctx := c.Request.Context()
	profiles, err := h.service.ListProfiles(
		ctx,
		middleware.GetWorkspaceIDFromContext(ctx),
	)
	if err != nil {
		writeModelConfigError(c, err)
		return
	}

	c.JSON(http.StatusOK, profiles)
}

func (h *ModelConfigHandler) CreateProfileVersion(c *gin.Context) {
	var req modelconfig.CreateProfileVersionRequest
	if err := decodeJSON(c.Writer, c.Request, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request body",
		})
		return
	}

	ctx := c.Request.Context()
	req.WorkspaceID = middleware.GetWorkspaceIDFromContext(ctx)
	req.CreatedBy = middleware.GetUserIDFromContext(ctx)

	version, err := h.service.CreateProfileVersion(
		ctx,
		c.Param("profile_id"),
		req,
	)
	if err != nil {
		writeModelConfigError(c, err)
		return
	}

	c.JSON(http.StatusCreated, version)
}

func (h *ModelConfigHandler) ListProfileVersions(c *gin.Context) {
	ctx := c.Request.Context()
	versions, err := h.service.ListProfileVersions(
		ctx,
		middleware.GetWorkspaceIDFromContext(ctx),
		c.Param("profile_id"),
	)
	if err != nil {
		writeModelConfigError(c, err)
		return
	}

	c.JSON(http.StatusOK, versions)
}

func (h *ModelConfigHandler) ListWorkspaceProfileVersions(c *gin.Context) {
	ctx := c.Request.Context()
	versions, err := h.service.ListWorkspaceProfileVersions(
		ctx,
		middleware.GetWorkspaceIDFromContext(ctx),
	)
	if err != nil {
		writeModelConfigError(c, err)
		return
	}
	c.JSON(http.StatusOK, versions)
}

func (h *ModelConfigHandler) GetProjectBinding(c *gin.Context) {
	ctx := c.Request.Context()
	binding, err := h.service.GetProjectBinding(
		ctx,
		middleware.GetWorkspaceIDFromContext(ctx),
		c.Query("env"),
	)
	if err != nil {
		writeModelConfigError(c, err)
		return
	}
	c.JSON(http.StatusOK, binding)
}

func (h *ModelConfigHandler) BindProject(c *gin.Context) {
	var req modelconfig.BindProjectRequest
	if err := decodeJSON(c.Writer, c.Request, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	ctx := c.Request.Context()
	req.WorkspaceID = middleware.GetWorkspaceIDFromContext(ctx)

	binding, err := h.service.BindProject(ctx, req)
	if err != nil {
		writeModelConfigError(c, err)
		return
	}
	c.JSON(http.StatusOK, binding)
}
