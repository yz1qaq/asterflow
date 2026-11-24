package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yz1qaq/asterflow/internal/platform/iam"
)

func GetWorkspaceIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(contextKeyWorkspaceID).(string)
	return id
}

func GetWorkspaceRoleFromContext(ctx context.Context) string {
	role, _ := ctx.Value(contextKeyWorkspaceRole).(string)
	return role
}

func Workspace(service *iam.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := GetUserIDFromContext(c.Request.Context())
		if userID == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "authentication is required",
			})
			return
		}

		workspaceID := strings.TrimSpace(c.GetHeader("X-Workspace-ID"))
		if workspaceID == "" {
			workspaceID = strings.TrimSpace(c.Query("workspace_id"))
		}
		if workspaceID == "" {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error": "workspace id is required",
			})
			return
		}

		role, err := service.WorkspaceRole(
			c.Request.Context(),
			userID,
			workspaceID,
		)
		if err != nil {
			switch {
			case errors.Is(err, iam.ErrForbidden):
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error": "workspace access denied",
				})
			case errors.Is(err, iam.ErrWorkspaceNotFound):
				c.AbortWithStatusJSON(http.StatusNotFound, gin.H{
					"error": "workspace not found",
				})
			default:
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error": "workspace authorization is unavailable",
				})
			}
			return
		}

		if !workspaceMethodAllowed(
			role,
			c.Request.Method,
			c.FullPath(),
		) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "workspace role does not allow this action",
			})
			return
		}

		ctx := context.WithValue(
			c.Request.Context(),
			contextKeyWorkspaceID,
			workspaceID,
		)
		ctx = context.WithValue(ctx, contextKeyWorkspaceRole, role)
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}

func workspaceMethodAllowed(role, method, path string) bool {
	if method != http.MethodGet &&
		(strings.Contains(path, "/approvals/") ||
			strings.HasSuffix(path, "/a2ui/actions")) {
		return role == iam.WorkspaceRoleOwner ||
			role == iam.WorkspaceRoleAdmin
	}

	if role == iam.WorkspaceRoleOwner ||
		role == iam.WorkspaceRoleAdmin ||
		role == iam.WorkspaceRoleEditor {
		return true
	}

	if role != iam.WorkspaceRoleMember &&
		role != iam.WorkspaceRoleViewer {
		return false
	}

	if method == http.MethodGet || method == http.MethodHead {
		return true
	}

	if role == iam.WorkspaceRoleViewer {
		return false
	}

	if method == http.MethodDelete && path == "/api/v1/conversations/:conversation_id" {
		return true
	}

	return strings.HasSuffix(path, "/chat") ||
		strings.HasSuffix(path, "/search") ||
		strings.HasSuffix(path, "/runs") ||
		strings.HasSuffix(path, "/conversations") ||
		strings.Contains(path, "/stream/")
}
