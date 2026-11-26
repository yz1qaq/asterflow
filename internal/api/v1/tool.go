package v1

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yz1qaq/asterflow/internal/api/middleware"
	"github.com/yz1qaq/asterflow/internal/platform/tool"
	"github.com/yz1qaq/asterflow/internal/runtime/tooling"
)

type ToolHandler struct {
	service  *tool.Service
	registry *tooling.Registry
}

func NewToolHandler(service *tool.Service, registry *tooling.Registry) *ToolHandler {
	return &ToolHandler{service: service, registry: registry}
}

func (h *ToolHandler) ListTools(c *gin.Context) {
	ctx := c.Request.Context()
	workspaceID := middleware.GetWorkspaceIDFromContext(ctx)

	tools, err := h.service.ListTools(ctx, workspaceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "tool operation failed"})
		return
	}
	c.JSON(http.StatusOK, tools)
}

func (h *ToolHandler) ListToolVersions(c *gin.Context) {
	ctx := c.Request.Context()
	workspaceID := middleware.GetWorkspaceIDFromContext(ctx)
	toolID := c.Param("tool_id")

	versions, err := h.service.ListToolVersions(ctx, workspaceID, toolID)
	if errors.Is(err, tool.ErrToolNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "tool not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "tool operation failed"})
		return
	}
	c.JSON(http.StatusOK, versions)
}

func (h *ToolHandler) CreateTool(c *gin.Context) {
	var req tool.CreateToolRequest
	if err := decodeJSON(c.Writer, c.Request, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	// 空间和创建者来自鉴权中间件写入的上下文。
	ctx := c.Request.Context()
	req.WorkspaceID = middleware.GetWorkspaceIDFromContext(ctx)
	req.CreatedBy = middleware.GetUserIDFromContext(ctx)

	item, err := h.service.CreateTool(ctx, req)
	if err != nil {
		switch {
		case errors.Is(err, tool.ErrInvalidArgument):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, tool.ErrToolAlreadyExists):
			c.JSON(http.StatusConflict, gin.H{"error": "tool name already exists in workspace"})
		default:
			// 数据库和加密失败统一返回服务端错误。
			c.JSON(http.StatusInternalServerError, gin.H{"error": "tool operation failed"})
		}
		return
	}

	// 初始版本由 Service 一起保存，响应返回工具主记录。
	c.JSON(http.StatusCreated, item)
}

func (h *ToolHandler) CreateToolVersion(c *gin.Context) {
	var req tool.CreateToolVersionRequest
	if err := decodeJSON(c.Writer, c.Request, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	// 工具 ID 来自路由，空间和创建者来自鉴权上下文。
	ctx := c.Request.Context()
	req.CreatedBy = middleware.GetUserIDFromContext(ctx)
	workspaceID := middleware.GetWorkspaceIDFromContext(ctx)

	version, err := h.service.CreateToolVersion(ctx, workspaceID, c.Param("tool_id"), req)
	switch {
	case errors.Is(err, tool.ErrToolNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "tool not found"})
	case errors.Is(err, tool.ErrToolVersionConflict):
		c.JSON(http.StatusConflict, gin.H{
			"code": "tool_version_conflict", "error": err.Error(),
		})
	case errors.Is(err, tool.ErrInvalidArgument):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "tool operation failed"})
	default:
		c.JSON(http.StatusCreated, version)
	}
}

func (h *ToolHandler) TestTool(c *gin.Context) {
	var body struct {
		Input json.RawMessage `json:"input"`
	}
	if err := decodeJSON(c.Writer, c.Request, &body); err != nil || len(body.Input) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid input is required"})
		return
	}

	ctx := c.Request.Context()
	workspaceID := middleware.GetWorkspaceIDFromContext(ctx)

	// 只在执行前选一次版本，结果用这次选中的版本 ID 保存。
	target, err := h.service.GetToolConfigForTest(ctx, workspaceID, c.Param("tool_id"))
	if errors.Is(err, tool.ErrToolNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "tool not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "tool configuration could not be loaded"})
		return
	}

	built, err := h.registry.Build(target)
	if errors.Is(err, tooling.ErrSensitiveToolRequiresApproval) {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	start := time.Now()
	output, runErr := built.Executor.Execute(ctx, body.Input)
	latencyMs := int(time.Since(start).Milliseconds())

	// 即使执行失败，也保存一次试跑；数据库写失败才返回 HTTP 500。
	run, err := h.service.RecordTestRun(
		ctx, workspaceID, built.ToolID, built.VersionID, string(body.Input), output, latencyMs, runErr,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "tool test run could not be recorded"})
		return
	}
	c.JSON(http.StatusOK, run)
}

func (h *ToolHandler) PublishToolVersion(c *gin.Context) {
	ctx := c.Request.Context()
	workspaceID := middleware.GetWorkspaceIDFromContext(ctx)
	err := h.service.PublishToolVersion(ctx, workspaceID, c.Param("tool_id"), c.Param("version_id"))

	switch {
	case errors.Is(err, tool.ErrToolNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "tool version not found"})
	case errors.Is(err, tool.ErrNoSuccessfulTestRun), errors.Is(err, tool.ErrToolTestRunExpired):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, tool.ErrToolVersionNotDraft):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "tool version could not be published"})
	default:
		c.JSON(http.StatusOK, gin.H{"status": "published"})
	}
}
