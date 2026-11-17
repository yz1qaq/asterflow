package v1

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yz1qaq/asterflow/internal/api/middleware"
	"github.com/yz1qaq/asterflow/internal/platform/iam"
)

type CreateWorkspaceRequest struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	ParentID    *string `json:"parent_id,omitempty"`
}

func (h *IAMHandler) Me(c *gin.Context) {
	ctx := c.Request.Context()
	c.JSON(http.StatusOK, gin.H{
		"user_id":     middleware.GetUserIDFromContext(ctx),
		"global_role": middleware.GetGlobalRoleFromContext(ctx),
	})
}

func writeIAMError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, iam.ErrForbidden):
		c.JSON(http.StatusForbidden, gin.H{
			"error": "forbidden",
		})

	case errors.Is(err, iam.ErrLastOwner):
		c.JSON(http.StatusConflict, gin.H{
			"error": err.Error(),
		})

	case errors.Is(err, iam.ErrNameRequired),
		errors.Is(err, iam.ErrInvalidWorkspaceRole),
		errors.Is(err, iam.ErrInvalidUserID):
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})

	case errors.Is(err, iam.ErrWorkspaceNotFound),
		errors.Is(err, iam.ErrWorkspaceMemberNotFound),
		errors.Is(err, iam.ErrUserNotFound):
		c.JSON(http.StatusNotFound, gin.H{
			"error": err.Error(),
		})

	default:
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "workspace operation failed",
		})
	}
}

// 列出所有工作空间
func (h *IAMHandler) ListWorkspaces(c *gin.Context) {
	limit, offset, err := pageParams(c.Request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx := c.Request.Context()
	workspaces, err := h.service.ListAccessibleWorkspaces(
		ctx,
		middleware.GetUserIDFromContext(ctx),
		limit,
		offset,
	)
	if err != nil {
		writeIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, workspaces)
}

// 创建工作空间
func (h *IAMHandler) CreateWorkspace(c *gin.Context) {
	var req CreateWorkspaceRequest
	if err := decodeJSON(c.Writer, c.Request, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request body",
		})
		return
	}

	ctx := c.Request.Context()
	workspace, err := h.service.CreateWorkspaceForUser(
		ctx,
		middleware.GetUserIDFromContext(ctx),
		req.Name,
		req.Description,
		req.ParentID,
	)
	if err != nil {
		writeIAMError(c, err)
		return
	}
	c.JSON(http.StatusCreated, workspace)
}

// 删除某个工作空间的成员
func (h *IAMHandler) DeleteWorkspaceMember(c *gin.Context) {
	ctx := c.Request.Context()
	err := h.service.DeleteWorkspaceMember(ctx, middleware.GetUserIDFromContext(ctx), c.Param("workspace_id"), c.Param("user_id"))
	if err != nil {
		writeIAMError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// 列出某个工作空间的成员列表
func (h *IAMHandler) ListWorkspaceMembers(c *gin.Context) {
	ctx := c.Request.Context()
	members, err := h.service.ListWorkspaceMembers(ctx, middleware.GetUserIDFromContext(ctx), c.Param("workspace_id"))
	if err != nil {
		writeIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, members)
}

type UpsertWorkspaceMemberRequest struct {
	Role string `json:"role"`
}

// 给某个工作空间加入成员
func (h *IAMHandler) UpsertWorkspaceMember(c *gin.Context) {
	ctx := c.Request.Context()
	var req UpsertWorkspaceMemberRequest
	if err := decodeJSON(c.Writer, c.Request, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request body",
		})
		return
	}

	member, err := h.service.UpsertWorkspaceMember(ctx, middleware.GetUserIDFromContext(ctx),
		c.Param("workspace_id"), c.Param("user_id"), req.Role)
	if err != nil {
		writeIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, member)
}

func (h *IAMHandler) Context(c *gin.Context) {
	ctx := c.Request.Context()
	c.JSON(http.StatusOK, gin.H{
		"user_id":        middleware.GetUserIDFromContext(ctx),
		"global_role":    middleware.GetGlobalRoleFromContext(ctx),
		"workspace_id":   middleware.GetWorkspaceIDFromContext(ctx),
		"workspace_role": middleware.GetWorkspaceRoleFromContext(ctx),
	})
}
