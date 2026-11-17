package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yz1qaq/asterflow/internal/platform/iam"
)

type contextKey int

const (
	contextKeyUserID contextKey = iota
	contextKeyGlobalRole
	contextKeyWorkspaceID
	contextKeyWorkspaceRole
)

func Auth(service *iam.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		//eg:parts = []string{"Bearer", "eyJhbGciOiJIUzI1Ni..."}
		parts := strings.Fields(c.GetHeader("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "bearer token is required",
			})
			return
		}
		claims, err := service.ValidateToken(parts[1])
		if err != nil {
			if errors.Is(err, iam.ErrInvalidToken) {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"error": "invalid token",
				})
			} else {
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error": "authentication is unavailable",
				})
			}
			return
		}
		user, err := service.GetUser(c.Request.Context(), claims.UserID)
		if errors.Is(err, iam.ErrUserNotFound) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "user account is unavailable",
			})
			return
		}
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"error": "authentication is unavailable",
			})
			return
		}
		if user.Status != "active" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "user account is unavailable",
			})
			return
		}
		ctx := context.WithValue(
			c.Request.Context(),
			contextKeyUserID,
			user.ID,
		)
		ctx = context.WithValue(ctx, contextKeyGlobalRole, user.Role)
		c.Request = c.Request.WithContext(ctx)

		c.Next()

	}

}

func GetUserIDFromContext(ctx context.Context) string {
	userID, _ := ctx.Value(contextKeyUserID).(string)
	return userID
}

func GetGlobalRoleFromContext(ctx context.Context) string {
	role, _ := ctx.Value(contextKeyGlobalRole).(string)
	return role
}

// 检查是否是全局管理员
func RequireGlobalAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		//如果全局角色不是全局管理员
		if GetGlobalRoleFromContext(c.Request.Context()) != iam.GlobalRoleAdmin {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "global admin role required",
			})
			return
		}
		c.Next()
	}
}

//
